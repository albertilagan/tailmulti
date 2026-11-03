import XCTest
@testable import TailmultiKit
final class ControlClientTests: XCTestCase {
    func testLoginURLAllowlist() throws {
        XCTAssertEqual(try ControlClient.loginURL("https://login.tailscale.com/a/example").host, "login.tailscale.com")
        for bad in ["http://login.tailscale.com/a/x", "https://login.tailscale.com.evil.test/a/x", "file:///etc/passwd", "https://user@login.tailscale.com/a/x", "https://login.tailscale.com:443/a/x"] {
            XCTAssertThrowsError(try ControlClient.loginURL(bad))
        }
    }
    func testHTTPFramingAndErrors() throws {
        let good = Data("HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\n{}".utf8)
        XCTAssertEqual(try ControlClient.decodeHTTP(good), Data("{}".utf8))
        XCTAssertThrowsError(try ControlClient.decodeHTTP(Data("HTTP/1.1 200 OK\r\nContent-Length: 3\r\n\r\n{}".utf8)))
        let error = "{\"error\":\"access denied\"}"
        XCTAssertThrowsError(try ControlClient.decodeHTTP(Data("HTTP/1.1 403 Forbidden\r\nContent-Length: \(error.utf8.count)\r\n\r\n\(error)".utf8)))
    }
    func testProtocolStatusFixture() throws {
        let data = Data(#"{"version":1,"mode":"preview","health":"ready","accounts":[{"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","name":"Home","enabled":false,"state":"disconnected","peerCount":0}],"devices":[],"conflicts":[]}"#.utf8)
        let status = try JSONDecoder().decode(EngineStatus.self, from: data)
        XCTAssertEqual(status.accounts.first?.name, "Home")
        XCTAssertFalse(status.accounts[0].enabled)
    }
}

final class LivePreviewProtocolTests: XCTestCase {
    func testNativeClientAgainstRealPreviewDaemon() async throws {
        guard let path = ProcessInfo.processInfo.environment["TAILMULTI_TEST_SOCKET"] else { throw XCTSkip("Requires scripts/test-preview.py; no daemon launched by unit tests.") }
        let client = ControlClient(path: path, serverUID: getuid())
        let initial = try await client.status()
        XCTAssertEqual(initial.mode, "preview")
        let created = try await client.add("Native protocol test")
        let status = try await client.status()
        let account = try XCTUnwrap(status.accounts.first(where: { $0.name == "Native protocol test" }))
        XCTAssertEqual(created.id, account.id)
        XCTAssertEqual(created.hasIdentity, false)
        XCTAssertEqual(account.hasIdentity, false)
        XCTAssertEqual(account.loginReady, false)
        XCTAssertEqual(account.phase, .signInRequired)
        XCTAssertFalse(account.enabled)
        do { _ = try await AccountSignIn(client: client).begin(id: account.id); XCTFail("Preview started sign-in") }
        catch { XCTAssertTrue(error.localizedDescription.contains("Turn Tailmulti on")) }
        do { try await client.action("connect", id: account.id); XCTFail("Preview enrolled an account") }
        catch { XCTAssertTrue(error.localizedDescription.contains("preview")) }
        let diagnostics = try await client.diagnostics()
        XCTAssertFalse(diagnostics.contains(account.name))
        XCTAssertFalse(diagnostics.contains(account.id))
        try await client.action("remove", id: account.id)
        let final = try await client.status()
        XCTAssertFalse(final.accounts.contains(where: { $0.id == account.id }))
        let wrongServer = ControlClient(path: path, serverUID: getuid() + 1)
        do { _ = try await wrongServer.status(); XCTFail("Untrusted server accepted") }
        catch { XCTAssertTrue(error.localizedDescription.contains("identity")) }
    }
}

final class ConnectionPhaseTests: XCTestCase {
    func testEngineStatesMapToPhases() {
        XCTAssertEqual(ConnectionPhase(state: "Running", enabled: true), .connected)
        XCTAssertEqual(ConnectionPhase(state: "NeedsLogin", enabled: true), .signInRequired)
        XCTAssertEqual(ConnectionPhase(state: "NeedsMachineAuth", enabled: true), .awaitingApproval)
        XCTAssertEqual(ConnectionPhase(state: "starting", enabled: true), .connecting)
        XCTAssertEqual(ConnectionPhase(state: "disconnected", enabled: false), .off)
        XCTAssertEqual(ConnectionPhase(state: "activation required", enabled: true), .setupRequired)
        XCTAssertEqual(ConnectionPhase(state: "unavailable", enabled: true), .unavailable)
        XCTAssertEqual(ConnectionPhase(state: "mystery", enabled: true).label, "Mystery")
        XCTAssertEqual(ConnectionPhase.off.label, "Not connected")
        XCTAssertTrue(ConnectionPhase.signInRequired.needsAttention)
        XCTAssertFalse(ConnectionPhase.connected.needsAttention)
    }
}
