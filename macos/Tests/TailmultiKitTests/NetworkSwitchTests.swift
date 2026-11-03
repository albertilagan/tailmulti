import XCTest
@testable import TailmultiKit

private func networkStatus(_ mode: String, health: String = "ready") throws -> EngineStatus {
    let object: [String: Any] = ["version": 1, "mode": mode, "health": health, "accounts": [], "devices": [], "conflicts": []]
    return try JSONDecoder().decode(EngineStatus.self, from: JSONSerialization.data(withJSONObject: object))
}

private actor SwitchService: NetworkControl {
    var mode: String
    var health = "ready"
    var calls: [String] = []
    let ready: Bool
    let failStop: Bool
    let holdStop: Bool
    private var enteredStop = false
    private var enteredWaiter: CheckedContinuation<Void, Never>?
    private var stopWaiter: CheckedContinuation<Void, Never>?

    init(mode: String = "active", ready: Bool = true, failStop: Bool = false, holdStop: Bool = false) {
        self.mode = mode; self.ready = ready; self.failStop = failStop; self.holdStop = holdStop
    }
    func status() throws -> EngineStatus { calls.append("status"); return try networkStatus(mode, health: health) }
    func setup() -> SetupReport {
        calls.append("setup")
        return SetupReport(ready: ready, checks: [SetupCheck(id: "vpn", state: ready ? .pass : .blocked,
            title: "VPN check", detail: "Disconnect the conflicting VPN.")])
    }
    func startNetwork() { calls.append("start"); mode = "active" }
    func stopNetwork(restoreDNS: Bool) async throws {
        calls.append(restoreDNS ? "stop-and-restore-dns" : "stop-without-restoring")
        enteredStop = true
        enteredWaiter?.resume(); enteredWaiter = nil
        if holdStop { await withCheckedContinuation { stopWaiter = $0 } }
        mode = "inactive"
        if failStop {
            health = "DNS cleanup failed"
            throw ControlError(health)
        }
    }
    func waitForStop() async {
        if !enteredStop { await withCheckedContinuation { enteredWaiter = $0 } }
    }
    func releaseStop() { stopWaiter?.resume(); stopWaiter = nil }
}

@MainActor
final class NetworkSwitchTests: XCTestCase {
    func testOffRestoresDNSAndWaitsForConfirmedState() async throws {
        let service = SwitchService(holdStop: true)
        let control = NetworkSwitch(client: service)
        let before = try networkStatus("active")
        let operation = Task { await control.setEnabled(false, current: before) }
        await service.waitForStop()
        XCTAssertEqual(control.pending, false)
        let duplicate = await control.setEnabled(true, current: before)
        XCTAssertNil(duplicate)
        await service.releaseStop()
        let after = await operation.value
        XCTAssertEqual(after?.mode, "inactive")
        XCTAssertNil(control.pending)
        XCTAssertNil(control.error)
        let calls = await service.calls
        XCTAssertEqual(calls, ["status", "stop-and-restore-dns", "status"])
    }

    func testOnRunsFreshPreflightAndResumesEngine() async throws {
        let service = SwitchService(mode: "inactive")
        let control = NetworkSwitch(client: service)
        let after = await control.setEnabled(true, current: try networkStatus("inactive"))
        XCTAssertEqual(after?.mode, "active")
        XCTAssertNil(control.error)
        let calls = await service.calls
        XCTAssertEqual(calls, ["status", "setup", "start", "status"])
    }

    func testBlockedOnDoesNotActivateAndReportsReason() async throws {
        let service = SwitchService(mode: "inactive", ready: false)
        let control = NetworkSwitch(client: service)
        let after = await control.setEnabled(true, current: try networkStatus("inactive"))
        XCTAssertEqual(after?.mode, "inactive")
        XCTAssertTrue(control.error?.contains("conflicting VPN") == true)
        XCTAssertNil(control.pending)
        let calls = await service.calls
        XCTAssertFalse(calls.contains("start"))
    }

    func testCleanupFailureShowsActualOffStateAndCanRetry() async throws {
        let service = SwitchService(failStop: true)
        let control = NetworkSwitch(client: service)
        let after = await control.setEnabled(false, current: try networkStatus("active"))
        XCTAssertEqual(after?.mode, "inactive")
        XCTAssertEqual(control.error, "DNS cleanup failed")
        _ = await control.setEnabled(false, current: after)
        let calls = await service.calls
        XCTAssertEqual(calls.filter { $0 == "stop-and-restore-dns" }.count, 2)
    }

    func testStaleAppStateDoesNotRestartAnActiveService() async throws {
        let service = SwitchService(mode: "active")
        let control = NetworkSwitch(client: service)
        let after = await control.setEnabled(true, current: try networkStatus("inactive"))
        XCTAssertEqual(after?.mode, "active")
        let calls = await service.calls
        XCTAssertEqual(calls, ["status", "status"])
    }

    func testPreviewAndUnexpectedServiceModesCannotChangeNetwork() async throws {
        let service = SwitchService(mode: "preview")
        let control = NetworkSwitch(client: service)
        let preview = await control.setEnabled(true, current: try networkStatus("preview"))
        XCTAssertNil(preview)
        let callsBefore = await service.calls
        XCTAssertTrue(callsBefore.isEmpty)
        _ = await control.setEnabled(true, current: try networkStatus("inactive"))
        XCTAssertTrue(control.error?.contains("unexpected mode") == true)
        let calls = await service.calls
        XCTAssertFalse(calls.contains("start"))
    }
}
