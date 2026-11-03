import XCTest
@testable import TailmultiKit

private let homeID = String(repeating: "a", count: 32)
private let workID = String(repeating: "b", count: 32)

private func home(_ state: String, enabled: Bool, identity: Bool = false, ready: Bool = false) -> Account {
    Account(id: homeID, name: "Home", enabled: enabled, state: state, peerCount: 0, hasIdentity: identity, loginReady: ready)
}
private func snapshot(_ account: Account?, mode: String = "active") -> EngineStatus {
    let work = Account(id: workID, name: "Work", enabled: true, state: "Running", peerCount: 4, hasIdentity: true)
    return EngineStatus(version: 1, mode: mode, accounts: (account.map { [$0] } ?? []) + [work], devices: [], conflicts: [], health: "ready")
}
private actor SignInClient: AccountSignInControl {
    var snapshots: [EngineStatus]
    var events: [String] = []
    let url: URL
    let failStart: Bool
    init(_ snapshots: [EngineStatus], url: URL = URL(string: "https://login.tailscale.com/a/test-only")!, failStart: Bool = false) {
        self.snapshots = snapshots; self.url = url; self.failStart = failStart
    }
    func status() -> EngineStatus {
        let result = snapshots[0]
        if snapshots.count > 1 { snapshots.removeFirst() }
        return result
    }
    func action(_ action: String, id: String) throws {
        events.append(action + ":" + id)
        if failStart { throw ControlError("Account engine could not start.") }
    }
    func login(id: String) -> URL { events.append("login:" + id); return url }
    func trace() -> [String] { events }
}

@MainActor
final class AccountSignInTests: XCTestCase {
    func testNewAccountStartsOnlyItsOwnEngineAndWaitsForLoginURL() async throws {
        let client = SignInClient([
            snapshot(home("disconnected", enabled: false)),
            snapshot(home("starting", enabled: true)),
            snapshot(home("NeedsLogin", enabled: true)),
            snapshot(home("NeedsLogin", enabled: true, ready: true))
        ])
        let result = try await AccountSignIn(client: client, wait: { _ in }).begin(id: homeID)
        XCTAssertEqual(result, .browser(URL(string: "https://login.tailscale.com/a/test-only")!))
        let trace = await client.trace()
        XCTAssertEqual(trace, ["connect:" + homeID, "login:" + homeID])
    }

    func testSavedIdentityReconnectDoesNotOpenAnUnnecessaryBrowser() async throws {
        let client = SignInClient([
            snapshot(home("disconnected", enabled: false, identity: true)),
            snapshot(home("Running", enabled: true, identity: true))
        ])
        let result = try await AccountSignIn(client: client).begin(id: homeID)
        XCTAssertEqual(result, .connected)
        let trace = await client.trace()
        XCTAssertEqual(trace, ["connect:" + homeID])
    }

    func testExistingLoginAndAdminApprovalDoNotRestartTheAccount() async throws {
        let pending = snapshot(home("NeedsLogin", enabled: true, ready: true))
        let client = SignInClient([pending])
        _ = try await AccountSignIn(client: client).begin(id: homeID)
        let trace = await client.trace()
        XCTAssertEqual(trace, ["login:" + homeID])
        let approval = SignInClient([snapshot(home("NeedsMachineAuth", enabled: true))])
        let result = try await AccountSignIn(client: approval).begin(id: homeID)
        XCTAssertEqual(result, .awaitingApproval)
        let approvalTrace = await approval.trace()
        XCTAssertTrue(approvalTrace.isEmpty)
    }

    func testGlobalOffOrRemovedAccountStopsThePendingSignIn() async {
        for stopped in [snapshot(home("NeedsLogin", enabled: true, ready: true), mode: "inactive"), snapshot(nil), snapshot(home("disconnected", enabled: false))] {
            let client = SignInClient([snapshot(home("disconnected", enabled: false)), stopped])
            do { _ = try await AccountSignIn(client: client).begin(id: homeID); XCTFail("opened sign-in after cancellation") }
            catch {}
            let trace = await client.trace()
            XCTAssertEqual(trace, ["connect:" + homeID])
        }
    }

    func testStartFailureTimeoutAndCancellationDoNotOpenLogin() async {
        let startFailure = SignInClient([snapshot(home("disconnected", enabled: false))], failStart: true)
        do { _ = try await AccountSignIn(client: startFailure).begin(id: homeID); XCTFail("ignored start failure") }
        catch {}
        let starting = snapshot(home("starting", enabled: true))
        let timeout = SignInClient([starting])
        do { _ = try await AccountSignIn(client: timeout, timeout: .zero).begin(id: homeID); XCTFail("ignored deadline") }
        catch {}
        let cancelled = SignInClient([starting])
        do {
            _ = try await AccountSignIn(client: cancelled, wait: { _ in throw CancellationError() }).begin(id: homeID)
            XCTFail("ignored cancellation")
        } catch is CancellationError {} catch { XCTFail("wrong cancellation error") }
        for client in [startFailure, timeout, cancelled] {
            let trace = await client.trace()
            XCTAssertFalse(trace.contains { $0.hasPrefix("login:") })
        }
    }

    func testUntrustedLoginURLIsRejected() async {
        let client = SignInClient([snapshot(home("NeedsLogin", enabled: true, ready: true))], url: URL(string: "https://example.com/login")!)
        do { _ = try await AccountSignIn(client: client).begin(id: homeID); XCTFail("accepted unsupported login destination") }
        catch {}
    }

    func testAccountPresentationDistinguishesNewSavedAndLegacyIdentities() throws {
        XCTAssertEqual(home("disconnected", enabled: false).phase, .signInRequired)
        XCTAssertEqual(home("disconnected", enabled: false, identity: true).phase, .off)
        XCTAssertEqual(home("NeedsLogin", enabled: true, identity: true).phase, .signInRequired)
        let legacy = try JSONDecoder().decode(Account.self, from: Data("""
        {"id":"a","name":"Old account","enabled":false,"state":"disconnected","peerCount":0}
        """.utf8))
        XCTAssertNil(legacy.hasIdentity)
        XCTAssertNil(legacy.loginReady)
        XCTAssertEqual(legacy.phase, .off)
    }
}
