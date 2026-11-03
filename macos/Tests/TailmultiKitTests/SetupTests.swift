import XCTest
import CryptoKit
import ServiceManagement
@testable import TailmultiKit

final class SetupReportTests: XCTestCase {
    func testDecodesEngineReportAndRequiresEveryCheckToPass() throws {
        let json = #"{"version":1,"ready":true,"checks":[{"id":"routes","state":"pass","title":"t","detail":"d"},{"id":"dns","state":"pass","title":"t","detail":"d"}]}"#
        XCTAssertTrue(try SetupReport.decode(Data(json.utf8)).isReady)
        let inconsistent = SetupReport(ready: true, checks: [SetupCheck(id: "dns", state: .unknown, title: "t", detail: "d")])
        XCTAssertFalse(inconsistent.isReady)
        XCTAssertFalse(SetupReport(ready: true, checks: []).isReady)
        XCTAssertFalse(SetupReport(ready: false, checks: [SetupCheck(id: "a", state: .pass, title: "t", detail: "d")]).isReady)
    }

    func testRejectsUnknownVersionsAndStates() {
        XCTAssertThrowsError(try SetupReport.decode(Data(#"{"version":2,"ready":true,"checks":[]}"#.utf8)))
        XCTAssertThrowsError(try SetupReport.decode(Data(#"{"version":1,"ready":true,"checks":[{"id":"a","state":"maybe","title":"t","detail":"d"}]}"#.utf8)))
        XCTAssertThrowsError(try SetupReport.decode(Data("not json".utf8)))
    }

    func testBlockerFlags() {
        let report = SetupReport(ready: false, checks: [
            SetupCheck(id: "dns", state: .blocked, title: "t", detail: "d"),
            SetupCheck(id: "vpn", state: .blocked, title: "t", detail: "d"),
        ])
        XCTAssertTrue(report.dnsDependsOnVPN)
        XCTAssertTrue(report.vpnConflict)
        XCTAssertFalse(SetupReport(ready: false, checks: [SetupCheck(id: "dns", state: .unknown, title: "t", detail: "d")]).dnsDependsOnVPN)
    }
}

final class SetupStageTests: XCTestCase {
    private func status(_ mode: String) -> EngineStatus {
        try! JSONDecoder().decode(EngineStatus.self, from: Data(#"{"version":1,"mode":"\#(mode)","health":"ready","accounts":[],"devices":[],"conflicts":[]}"#.utf8))
    }

    private let missing = ServiceProbe(result: .failure(ControlError("x", kind: .unavailable)))
    private let signed: Result<VerifiedService, ControlError> = .success(VerifiedService(plistName: "dev.tailmulti.tailmultid.plist", signingMode: .development, team: "ABCDE12345"))
    private func stage(_ probe: ServiceProbe, _ registration: RegistrationStatus = .notRegistered,
                       _ bundle: Result<VerifiedService, ControlError>? = nil) -> SetupStage {
        SetupStage.resolve(probe: probe, registration: registration, bundle: bundle ?? signed)
    }

    func testStagesFollowTheAuthenticatedService() {
        XCTAssertEqual(stage(missing), .allowService)
        XCTAssertEqual(stage(.running(status("inactive")), .enabled), .enableNetworking)
        XCTAssertEqual(stage(.running(status("active")), .enabled), .networkingActive)
        // The answering root service decides, whatever the registration status says.
        XCTAssertEqual(stage(.running(status("inactive")), .notRegistered), .enableNetworking)
    }

    func testRegistrationAloneNeverCountsAsInstalled() {
        XCTAssertEqual(stage(missing, .enabled), .serviceStarting)
        XCTAssertEqual(stage(missing, .requiresApproval), .awaitingApproval)
        XCTAssertEqual(stage(missing, .notFound), .allowService)
        guard case .cannotRegister("bad") = stage(missing, .notFound, .failure(ControlError("bad"))) else { return XCTFail() }
        let adhoc: Result<VerifiedService, ControlError> = .success(VerifiedService(plistName: "x.plist", signingMode: .adhoc))
        guard case .cannotRegister = stage(missing, .notFound, adhoc) else { return XCTFail() }
        guard case .cannotRegister = stage(missing, .unsupported) else { return XCTFail() }
        for registration in [RegistrationStatus.enabled, .requiresApproval, .notFound, .notRegistered, .unsupported] {
            XCTAssertNotEqual(stage(missing, registration), .enableNetworking)
            XCTAssertNotEqual(stage(missing, registration), .networkingActive)
        }
    }

    func testRegistrationOfferedOnlyForVerifiedSignedBundles() {
        let adhoc: Result<VerifiedService, ControlError> = .success(VerifiedService(plistName: "x.plist", signingMode: .adhoc))
        guard case .cannotRegister = stage(missing, .notRegistered, adhoc) else { return XCTFail() }
        guard case .cannotRegister("bad") = stage(missing, .notRegistered, .failure(ControlError("bad"))) else { return XCTFail() }
        let unsignedDevelopment: Result<VerifiedService, ControlError> = .success(VerifiedService(plistName: "x.plist", signingMode: .development))
        guard case .cannotRegister = stage(missing, .notRegistered, unsignedDevelopment) else { return XCTFail() }
        let developerID: Result<VerifiedService, ControlError> = .success(VerifiedService(plistName: "x.plist", signingMode: .developerID, team: "ABCDE12345"))
        XCTAssertEqual(stage(missing, .notRegistered, developerID), .allowService)
    }

    func testUntrustedOrUnexpectedServicesAreProblemsNotSuccess() {
        guard case .serviceProblem = stage(ServiceProbe(result: .failure(ControlError("x", kind: .identityMismatch))), .enabled) else { return XCTFail() }
        guard case .serviceProblem = stage(ServiceProbe(result: .failure(ControlError("timeout"))), .enabled) else { return XCTFail() }
        // A root socket answering in preview mode is never treated as the installed service.
        guard case .serviceProblem = stage(.running(status("preview")), .enabled) else { return XCTFail() }
        guard case .serviceProblem = stage(.running(status("bogus")), .enabled) else { return XCTFail() }
    }

    func testServiceRemovalOnlyWhileNetworkingIsOff() {
        XCTAssertFalse(SetupStage.serviceStarting.allowsServiceRemoval)
        XCTAssertTrue(SetupStage.enableNetworking.allowsServiceRemoval)
        XCTAssertTrue(SetupStage.enableNetworking.restoresDNSBeforeRemoval)
        XCTAssertTrue(SetupStage.awaitingApproval.allowsServiceRemoval)
        XCTAssertFalse(SetupStage.awaitingApproval.restoresDNSBeforeRemoval)
        XCTAssertFalse(SetupStage.networkingActive.allowsServiceRemoval)
        XCTAssertFalse(SetupStage.allowService.allowsServiceRemoval)
        XCTAssertFalse(SetupStage.serviceProblem("x").allowsServiceRemoval)
    }

    func testCheckSources() {
        XCTAssertEqual(SetupStage.allowService.checkSource, .local)
        XCTAssertEqual(SetupStage.awaitingApproval.checkSource, .local)
        XCTAssertEqual(SetupStage.serviceStarting.checkSource, .local)
        XCTAssertEqual(SetupStage.enableNetworking.checkSource, .service)
        XCTAssertNil(SetupStage.networkingActive.checkSource)
    }

    func testEngineModes() {
        XCTAssertEqual(EngineMode("inactive"), .inactive)
        XCTAssertEqual(EngineMode("preview"), .preview)
        XCTAssertNotEqual(EngineMode("inactive"), .preview)
        XCTAssertEqual(EngineMode("x"), .unsupported("x"))
    }

    func testMissingSocketIsReportedAsNotInstalled() async {
        let client = ControlClient(path: "/tmp/tm-missing-\(getpid()).sock", serverUID: getuid())
        do { _ = try await client.status(); XCTFail("connected to a missing socket") }
        catch { XCTAssertEqual((error as? ControlError)?.kind, .unavailable) }
    }

    func testDefaultServiceSocketPath() {
        XCTAssertEqual(ControlClient().path, "/Library/Application Support/Tailmulti/run/control.sock")
        XCTAssertEqual(ControlClient().serverUID, 0)
    }
}

final class PreviewNameTests: XCTestCase {
    private func account(_ name: String, _ n: Int) -> Account {
        try! JSONDecoder().decode(Account.self, from: Data(#"{"id":"\#(String(repeating: String(n), count: 32))","name":"\#(name)","enabled":false,"state":"disconnected","peerCount":0}"#.utf8))
    }

    func testCopiesOnlyMissingNamesOnce() {
        let preview = [account("Home", 1), account("Work", 2), account(" home ", 3), account("Work", 4)]
        XCTAssertEqual(missingAccountNames(preview: preview, service: [account("HOME", 5)]), ["Work"])
        XCTAssertEqual(missingAccountNames(preview: preview, service: []), ["Home", "Work"])
        XCTAssertEqual(missingAccountNames(preview: [], service: [account("Home", 1)]), [])
    }
}

final class ServiceBundleTests: XCTestCase {
    private var bundle: URL!
    private let teamSigned: ServiceBundle.SignatureCheck = { _, _, expected in
        if let expected, expected != "ABCDE12345" { throw ControlError("team") }
        return "ABCDE12345"
    }
    private func verify() throws -> VerifiedService { try ServiceBundle.verify(bundle: bundle, signature: teamSigned) }

    override func setUpWithError() throws {
        bundle = FileManager.default.temporaryDirectory.appendingPathComponent("tm-\(UUID().uuidString).app")
        for dir in ["Contents/Resources", "Contents/Library/HelperTools", "Contents/Library/LaunchDaemons"] {
            try FileManager.default.createDirectory(at: bundle.appendingPathComponent(dir), withIntermediateDirectories: true)
        }
    }
    override func tearDownWithError() throws { try? FileManager.default.removeItem(at: bundle) }

    private func stage(helper: String = "helper-binary", hash: String? = nil, version: Int = 3,
                       signing: String = "development", label: String = "dev.tailmulti.tailmultid", team: String = "ABCDE12345",
                       plist: [String: Any]? = nil) throws {
        try Data(helper.utf8).write(to: bundle.appendingPathComponent(ServiceBundle.bundleProgram))
        let digest = hash ?? SHA256.hash(data: Data(helper.utf8)).map { String(format: "%02x", $0) }.joined()
        let manifest = #"{"version":\#(version),"helperSHA256":"\#(digest)","serviceLabel":"\#(label)","registration":"SMAppService","signingMode":"\#(signing)","signingTeam":"\#(team)"}"#
        try Data(manifest.utf8).write(to: bundle.appendingPathComponent("Contents/Resources/SetupManifest.json"))
        let definition = plist ?? ["Label": "dev.tailmulti.tailmultid", "BundleProgram": ServiceBundle.bundleProgram,
                                   "ProgramArguments": ["tailmultid", "--service"]]
        let data = try PropertyListSerialization.data(fromPropertyList: definition, format: .xml, options: 0)
        try data.write(to: bundle.appendingPathComponent("Contents/Library/LaunchDaemons/dev.tailmulti.tailmultid.plist"))
    }

    func testAcceptsMatchingBundle() throws {
        try stage()
        XCTAssertEqual(try verify(), VerifiedService(plistName: "dev.tailmulti.tailmultid.plist", signingMode: .development, team: "ABCDE12345"))
        try stage(signing: "developer-id")
        XCTAssertEqual(try verify().team, "ABCDE12345")
    }

    func testSigningModeStringIsNotTrusted() throws {
        try stage()
        // A manifest claiming a development signature still fails when the real signature check fails.
        XCTAssertThrowsError(try ServiceBundle.verify(bundle: bundle, signature: { _, _, _ in throw ControlError("unsigned") }))
        // The default check uses the Security framework, which rejects this unsigned test bundle.
        XCTAssertThrowsError(try ServiceBundle.verify(bundle: bundle))
        // Ad-hoc builds are never offered registration and skip the team check.
        try stage(signing: "adhoc")
        let adhoc = try ServiceBundle.verify(bundle: bundle, signature: { _, _, _ in XCTFail("checked ad-hoc"); return "" })
        XCTAssertNil(adhoc.team)
    }

    func testRejectsBuildTimeOwnerHashVersionLabelOrSigningMode() throws {
        try stage(hash: String(repeating: "0", count: 64)); XCTAssertThrowsError(try verify())
        let upper = SHA256.hash(data: Data("helper-binary".utf8)).map { String(format: "%02X", $0) }.joined()
        try stage(hash: upper); XCTAssertThrowsError(try verify())
        // Version 2 manifests came from builds that embedded their owner.
        try stage(version: 2); XCTAssertThrowsError(try verify())
        try stage(label: "other"); XCTAssertThrowsError(try verify())
        try stage(signing: "unknown"); XCTAssertThrowsError(try verify())
        try stage(team: "ZZZZZ99999"); XCTAssertThrowsError(try verify())
    }

    func testRejectsUnexpectedServiceDefinitions() throws {
        let base: [String: Any] = ["Label": "dev.tailmulti.tailmultid", "BundleProgram": ServiceBundle.bundleProgram,
                                   "ProgramArguments": ["tailmultid", "--service"]]
        var absolute = base; absolute["Program"] = "/usr/local/bin/tailmultid"
        var fixedOwner = base; fixedOwner["ProgramArguments"] = ["tailmultid", "--service", "--owner-uid", String(getuid())]
        var extraArgs = base; extraArgs["ProgramArguments"] = ["tailmultid", "--service", "--activate"]
        var otherProgram = base; otherProgram["BundleProgram"] = "Contents/MacOS/Tailmulti"
        var otherLabel = base; otherLabel["Label"] = "com.example"
        for plist in [absolute, fixedOwner, extraArgs, otherProgram, otherLabel] {
            try stage(plist: plist)
            XCTAssertThrowsError(try verify())
        }
    }

    func testRejectsMissingOrLinkedFiles() throws {
        try stage()
        let helper = bundle.appendingPathComponent(ServiceBundle.bundleProgram)
        let real = bundle.appendingPathComponent("Contents/real")
        try FileManager.default.moveItem(at: helper, to: real)
        try FileManager.default.createSymbolicLink(at: helper, withDestinationURL: real)
        XCTAssertThrowsError(try verify())
        try FileManager.default.removeItem(at: bundle.appendingPathComponent("Contents/Library/LaunchDaemons/dev.tailmulti.tailmultid.plist"))
        try FileManager.default.removeItem(at: helper)
        try FileManager.default.moveItem(at: real, to: helper)
        XCTAssertThrowsError(try verify())
    }
}

/// Runs the real bundle and Security-framework checks against a staged app. No registration happens.
final class StagedAppBundleTests: XCTestCase {
    func testStagedBundlePassesRealVerification() throws {
        guard let path = ProcessInfo.processInfo.environment["TAILMULTI_TEST_APP_BUNDLE"] else {
            throw XCTSkip("Set TAILMULTI_TEST_APP_BUNDLE to a signed Tailmulti.app to run.")
        }
        let service = try ServiceBundle.verify(bundle: URL(fileURLWithPath: path))
        XCTAssertTrue(service.signingMode.canRegister)
        XCTAssertNotNil(service.team)
        XCTAssertEqual(service.plistName, "dev.tailmulti.tailmultid.plist")
    }
}

final class CodeSignatureTests: XCTestCase {
    func testTeamIdentifierFormat() {
        XCTAssertTrue(CodeSignature.isTeamIdentifier("ABCDE12345"))
        for bad in ["abcde12345", "ABCDE1234", "ABCDE123456", "ABCDE\"1234", ""] {
            XCTAssertFalse(CodeSignature.isTeamIdentifier(bad))
        }
    }

    func testRejectsCodeWithoutDeveloperTeam() throws {
        // Apple platform binaries pass the anchor check but carry no team, so they're refused.
        XCTAssertThrowsError(try CodeSignature.verifyTeam(bundle: URL(fileURLWithPath: "/bin/ls"), helper: URL(fileURLWithPath: "/bin/ls")))
        let unsigned = FileManager.default.temporaryDirectory.appendingPathComponent("tm-unsigned-\(UUID().uuidString)")
        try Data("#!/bin/sh\n".utf8).write(to: unsigned)
        defer { try? FileManager.default.removeItem(at: unsigned) }
        XCTAssertThrowsError(try CodeSignature.verifyTeam(bundle: unsigned, helper: unsigned))
    }
}

final class RegistrationTests: XCTestCase {
    private func error(_ code: Int, _ domain: String = "SMAppServiceErrorDomain") -> NSError { NSError(domain: domain, code: code) }

    func testStatusAfterFailureDecidesFirst() {
        // DTS shows SMAppServiceErrorDomain code 1 for pending approval; the re-read status, not the number, decides.
        XCTAssertEqual(RegistrationOutcome(error: error(1), statusAfter: .requiresApproval), .needsApproval)
        XCTAssertEqual(RegistrationOutcome(error: error(1), statusAfter: .enabled), .alreadyRegistered)
        guard case .failed = RegistrationOutcome(error: error(1), statusAfter: .notRegistered) else { return XCTFail("code 1 alone isn't approval") }
    }

    func testCodesOnlyCountInServiceManagementDomains() {
        guard case .failed(let generic) = RegistrationOutcome(error: error(Int(kSMErrorInvalidSignature), NSPOSIXErrorDomain), statusAfter: .notRegistered) else { return XCTFail() }
        XCTAssertEqual(generic, "macOS couldn't register the background service.")
        guard case .failed(let signature) = RegistrationOutcome(error: error(Int(kSMErrorInvalidSignature)), statusAfter: .notRegistered) else { return XCTFail() }
        XCTAssertTrue(signature.contains("signature"))
        guard case .failed(let framework) = RegistrationOutcome(error: error(Int(kSMErrorInvalidPlist), "kSMErrorDomainFramework"), statusAfter: .notFound) else { return XCTFail() }
        XCTAssertTrue(framework.contains("isn't valid"))
        // A denial code without a pending-approval status is reported, not treated as approval.
        guard case .failed = RegistrationOutcome(error: error(Int(kSMErrorLaunchDeniedByUser)), statusAfter: .notRegistered) else { return XCTFail() }
        XCTAssertTrue(RegistrationOutcome.isServiceManagementError(error(Int(kSMErrorJobNotFound)), code: Int(kSMErrorJobNotFound)))
        XCTAssertFalse(RegistrationOutcome.isServiceManagementError(error(Int(kSMErrorJobNotFound), NSCocoaErrorDomain), code: Int(kSMErrorJobNotFound)))
    }

    func testStatusMapping() {
        XCTAssertEqual(RegistrationStatus(.enabled), .enabled)
        XCTAssertEqual(RegistrationStatus(.requiresApproval), .requiresApproval)
        XCTAssertEqual(RegistrationStatus(.notFound), .notFound)
        XCTAssertEqual(RegistrationStatus(.notRegistered), .notRegistered)
    }
}

final class HostCheckRunnerTests: XCTestCase {
    private func script(_ body: String) throws -> URL {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent("tailmulti-check-\(UUID().uuidString)")
        try Data("#!/bin/sh\n\(body)\n".utf8).write(to: url)
        try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: url.path)
        addTeardownBlock { try? FileManager.default.removeItem(at: url) }
        return url
    }

    func testParsesReportFromFixedArgument() async throws {
        // The stand-in only answers when given exactly --check-host.
        let url = try script(#"[ "$#" = 1 ] && [ "$1" = --check-host ] || exit 9; echo '{"version":1,"ready":false,"checks":[{"id":"dns","state":"blocked","title":"t","detail":"d"}]}'"#)
        let report = try await HostCheckRunner(executable: url).run()
        XCTAssertFalse(report.isReady)
        XCTAssertTrue(report.dnsDependsOnVPN)
    }

    func testFailuresAreErrors() async throws {
        let failing = try script("exit 1")
        do { _ = try await HostCheckRunner(executable: failing).run(); XCTFail() } catch {}
        let garbage = try script("echo nope")
        do { _ = try await HostCheckRunner(executable: garbage).run(); XCTFail() } catch {}
        do { _ = try await HostCheckRunner(executable: URL(fileURLWithPath: "/nonexistent/tailmultid")).run(); XCTFail() } catch {}
    }

    func testTimeoutTerminatesChecker() async throws {
        var runner = HostCheckRunner(executable: try script("exec sleep 30"))
        runner.timeout = 0.3; runner.killGrace = 0.5
        let start = Date()
        do { _ = try await runner.run(); XCTFail() }
        catch { XCTAssertTrue(error.localizedDescription.contains("timed out")) }
        XCTAssertLessThan(Date().timeIntervalSince(start), 3)
    }

    func testCheckerIgnoringSIGTERMIsKilled() async throws {
        let pidFile = FileManager.default.temporaryDirectory.appendingPathComponent("tm-pid-\(UUID().uuidString)")
        addTeardownBlock { try? FileManager.default.removeItem(at: pidFile) }
        // The ignored disposition survives exec, so only SIGKILL can stop this checker.
        var runner = HostCheckRunner(executable: try script("trap '' TERM; echo $$ > '\(pidFile.path)'; exec sleep 30"))
        // A cold process launch under a new SDK can outlast half a second. Give the
        // fixture time to install its signal disposition and record the child PID.
        runner.timeout = 2; runner.killGrace = 0.5
        let start = Date()
        do { _ = try await runner.run(); XCTFail() }
        catch { XCTAssertTrue(error.localizedDescription.contains("timed out")) }
        XCTAssertGreaterThanOrEqual(Date().timeIntervalSince(start), runner.timeout + runner.killGrace)
        XCTAssertLessThan(Date().timeIntervalSince(start), 6)
        let pid = try XCTUnwrap(pid_t(String(decoding: try Data(contentsOf: pidFile), as: UTF8.self).trimmingCharacters(in: .whitespacesAndNewlines)))
        XCTAssertEqual(kill(pid, 0), -1, "checker still running")
        XCTAssertEqual(errno, ESRCH)
    }

    func testOversizedOutputIsRejected() async throws {
        let url = try script("head -c 1100000 /dev/zero")
        do { _ = try await HostCheckRunner(executable: url).run(); XCTFail() } catch {}
    }
}
