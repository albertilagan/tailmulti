import Foundation
import CryptoKit
import Darwin

/// One read-only host check reported by the engine.
public struct SetupCheck: Decodable, Identifiable, Equatable, Sendable {
    public enum State: String, Decodable, Sendable { case pass, blocked, unknown }
    public let id: String
    public let state: State
    public let title: String
    public let detail: String
    public init(id: String, state: State, title: String, detail: String) {
        self.id = id; self.state = state; self.title = title; self.detail = detail
    }
}

public struct SetupReport: Decodable, Equatable, Sendable {
    public let version: Int
    public let ready: Bool
    public let checks: [SetupCheck]
    public init(version: Int = 1, ready: Bool, checks: [SetupCheck]) {
        self.version = version; self.ready = ready; self.checks = checks
    }

    /// Ready only when the engine says so and every reported check passed.
    public var isReady: Bool { version == 1 && ready && !checks.isEmpty && checks.allSatisfy { $0.state == .pass } }
    public var dnsDependsOnVPN: Bool { checks.contains { $0.id == "dns" && $0.state == .blocked } }
    public var vpnConflict: Bool { checks.contains { $0.id == "vpn" && $0.state == .blocked } }

    public static func decode(_ data: Data) throws -> SetupReport {
        let report: SetupReport
        do { report = try JSONDecoder().decode(SetupReport.self, from: data) }
        catch { throw ControlError("The setup check returned an unreadable report.") }
        guard report.version == 1 else { throw ControlError("The setup check uses an unsupported report version.") }
        return report
    }
}

/// How the app reaches an engine. The root service is preferred whenever it answers.
public enum EngineMode: Equatable, Sendable {
    case preview, inactive, active
    case unsupported(String)
    public init(_ raw: String) {
        switch raw {
        case "preview": self = .preview
        case "inactive": self = .inactive
        case "active": self = .active
        default: self = .unsupported(raw)
        }
    }
}

public enum ServiceProbe: Sendable {
    case notInstalled
    case untrusted
    case failed(String)
    case running(EngineStatus)

    public init(result: Result<EngineStatus, Error>) {
        switch result {
        case .success(let status): self = .running(status)
        case .failure(let error as ControlError) where error.kind == .unavailable: self = .notInstalled
        case .failure(let error as ControlError) where error.kind == .identityMismatch: self = .untrusted
        case .failure(let error): self = .failed(error.localizedDescription)
        }
    }
}

/// macOS registration state of the bundled LaunchDaemon, mirrored from `SMAppService.Status`.
public enum RegistrationStatus: Equatable, Sendable {
    case notRegistered, enabled, requiresApproval, notFound
    /// A status this app doesn't recognize; treated as unable to register.
    case unsupported
}

/// Where setup stands. Only an authenticated root service answering on its socket counts as installed;
/// a registration status of `.enabled` alone never does.
public enum SetupStage: Equatable, Sendable {
    case allowService
    case cannotRegister(String)
    case awaitingApproval
    case serviceStarting
    case serviceProblem(String)
    case enableNetworking
    case networkingActive

    public static func resolve(probe: ServiceProbe, registration: RegistrationStatus, bundle: Result<VerifiedService, ControlError>) -> SetupStage {
        switch probe {
        case .running(let status):
            switch EngineMode(status.mode) {
            case .inactive: return .enableNetworking
            case .active: return .networkingActive
            case .preview, .unsupported: return .serviceProblem("The background service reported an unexpected mode.")
            }
        case .untrusted:
            return .serviceProblem("Something other than Tailmulti's background service is answering on its control socket. Tailmulti won't talk to it.")
        case .failed(let message):
            return .serviceProblem(message)
        case .notInstalled:
            switch registration {
            case .enabled: return .serviceStarting
            case .requiresApproval: return .awaitingApproval
            case .unsupported: return .cannotRegister("macOS reported a background service state Tailmulti doesn't recognize.")
            // macOS can report "not found" before a first registration, so the bundle's own verification decides.
            case .notRegistered, .notFound:
                switch bundle {
                case .failure(let error): return .cannotRegister(error.message)
                case .success(let service) where !service.signingMode.canRegister || service.team == nil:
                    return .cannotRegister("This build is ad-hoc signed. macOS only runs background services from apps signed with a development or Developer ID certificate.")
                case .success: return .allowService
                }
            }
        }
    }

    /// Which checker applies: the bundled helper as the user before the service runs, the service once it does.
    public var checkSource: CheckSource? {
        switch self {
        case .allowService, .cannotRegister, .awaitingApproval, .serviceStarting: .local
        case .enableNetworking: .service
        case .networkingActive, .serviceProblem: nil
        }
    }
    public enum CheckSource: Sendable { case local, service }

    /// Service removal is offered only while networking is off.
    public var allowsServiceRemoval: Bool {
        switch self {
        case .enableNetworking, .awaitingApproval: true
        default: false
        }
    }
    /// An answering inactive service restores its owned DNS entries before it is removed.
    public var restoresDNSBeforeRemoval: Bool { self == .enableNetworking }
}

/// Names saved in preview that the installed service doesn't have yet. Matching is by trimmed, case-insensitive name.
public func missingAccountNames(preview: [Account], service: [Account]) -> [String] {
    func key(_ s: String) -> String { s.trimmingCharacters(in: .whitespacesAndNewlines).lowercased() }
    var seen = Set(service.map { key($0.name) })
    var result: [String] = []
    for account in preview {
        let name = account.name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !name.isEmpty, seen.insert(key(name)).inserted else { continue }
        result.append(name)
    }
    return result
}

public enum SigningMode: String, Decodable, Sendable {
    case development, developerID = "developer-id", adhoc
    /// SMAppService rejects ad-hoc signed apps, so registration isn't offered for them.
    public var canRegister: Bool { self != .adhoc }
}

public struct SetupManifest: Decodable, Equatable, Sendable {
    public let version: Int
    public let ownerUID: Int
    public let helperSHA256: String
    public let serviceLabel: String
    public let registration: String
    public let signingMode: SigningMode
    public let signingTeam: String?
}

public struct VerifiedService: Equatable, Sendable {
    public let plistName: String
    public let signingMode: SigningMode
    /// Team that signed both the app and its service. Nil for ad-hoc builds, which can't register.
    public let team: String?
    public init(plistName: String, signingMode: SigningMode, team: String? = nil) {
        self.plistName = plistName; self.signingMode = signingMode; self.team = team
    }
}

/// Checks that this app bundle carries the expected LaunchDaemon and helper for this user before offering registration.
public enum ServiceBundle {
    public static let label = "dev.tailmulti.tailmultid"
    public static let bundleProgram = "Contents/Library/HelperTools/tailmultid"

    public typealias SignatureCheck = @Sendable (_ bundle: URL, _ helper: URL, _ expectedTeam: String?) throws -> String

    /// `signature` is the real Security-framework check by default; tests substitute it.
    public static func verify(bundle: URL, uid: uid_t = getuid(),
                              signature: SignatureCheck = { try CodeSignature.verifyTeam(bundle: $0, helper: $1, expectedTeam: $2) }) throws -> VerifiedService {
        let manifestURL = bundle.appendingPathComponent("Contents/Resources/SetupManifest.json")
        guard isRegularFile(manifestURL), let data = try? Data(contentsOf: manifestURL), data.count < 64 * 1024 else {
            throw ControlError("This copy of Tailmulti doesn't include its background service.")
        }
        guard let manifest = try? JSONDecoder().decode(SetupManifest.self, from: data), manifest.version == 2,
              manifest.registration == "SMAppService", manifest.serviceLabel == label else {
            throw ControlError("This copy of Tailmulti has an unreadable service manifest.")
        }
        guard manifest.ownerUID == Int(uid) else {
            throw ControlError("This build of Tailmulti was made for a different macOS user account.")
        }
        let hex = manifest.helperSHA256
        guard hex.count == 64, hex.allSatisfy({ "0123456789abcdef".contains($0) }) else {
            throw ControlError("This copy of Tailmulti has an unreadable service manifest.")
        }
        let helper = bundle.appendingPathComponent(bundleProgram)
        guard isRegularFile(helper), (try? sha256(of: helper)) == hex else {
            throw ControlError("The background service in this copy of Tailmulti doesn't match its manifest. Rebuild Tailmulti.")
        }
        let plistName = label + ".plist"
        let plistURL = bundle.appendingPathComponent("Contents/Library/LaunchDaemons/" + plistName)
        guard isRegularFile(plistURL), let plistData = try? Data(contentsOf: plistURL),
              let plist = try? PropertyListSerialization.propertyList(from: plistData, format: nil) as? [String: Any],
              plistIsExpected(plist, uid: uid) else {
            throw ControlError("The background service definition in this copy of Tailmulti isn't valid.")
        }
        // The manifest's signing mode is informational; registration needs a real team signature.
        guard manifest.signingMode.canRegister else { return VerifiedService(plistName: plistName, signingMode: .adhoc) }
        let team = try signature(bundle, helper, manifest.signingTeam)
        return VerifiedService(plistName: plistName, signingMode: manifest.signingMode, team: team)
    }

    static func plistIsExpected(_ plist: [String: Any], uid: uid_t) -> Bool {
        guard plist["Label"] as? String == label, plist["BundleProgram"] as? String == bundleProgram,
              plist["Program"] == nil, let args = plist["ProgramArguments"] as? [String] else { return false }
        return args == ["tailmultid", "--service", "--owner-uid", String(uid)]
    }

    static func isRegularFile(_ url: URL) -> Bool {
        let values = try? url.resourceValues(forKeys: [.isRegularFileKey, .isSymbolicLinkKey])
        return values?.isRegularFile == true && values?.isSymbolicLink != true
    }

    static func sha256(of url: URL) throws -> String {
        guard let handle = try? FileHandle(forReadingFrom: url) else { throw ControlError("The background service can't be read.") }
        defer { try? handle.close() }
        var hasher = SHA256()
        while let chunk = try handle.read(upToCount: 1 << 20), !chunk.isEmpty { hasher.update(data: chunk) }
        return hasher.finalize().map { String(format: "%02x", $0) }.joined()
    }
}

/// Runs the bundled engine's read-only `--check-host`. Fixed executable and arguments, no shell.
/// Only the checker process this runner started is ever signalled.
public struct HostCheckRunner: Sendable {
    public let executable: URL
    public var timeout: TimeInterval = 20
    /// How long a timed-out checker gets after SIGTERM before SIGKILL.
    public var killGrace: TimeInterval = 2
    public init(executable: URL) { self.executable = executable }

    public func run() async throws -> SetupReport {
        let runner = self
        return try await Task.detached { try runner.runSync() }.value
    }

    static let outputLimit = 1 << 20

    func runSync() throws -> SetupReport {
        guard FileManager.default.isExecutableFile(atPath: executable.path) else {
            throw ControlError("The setup checker isn't included in this copy of Tailmulti.")
        }
        let process = Process()
        process.executableURL = executable
        process.arguments = ["--check-host"]
        process.environment = ["PATH": "/usr/bin:/bin:/usr/sbin:/sbin"]
        let output = Pipe()
        process.standardOutput = output
        process.standardError = FileHandle.nullDevice
        process.standardInput = FileHandle.nullDevice
        let exited = DispatchSemaphore(value: 0)
        process.terminationHandler = { _ in exited.signal() }
        do { try process.run() } catch { throw ControlError("The setup checker couldn't start.") }

        // Read on a separate thread so a stuck pipe can never block past the deadline.
        let buffer = OutputBuffer()
        let reader = output.fileHandleForReading
        Thread.detachNewThread {
            while let chunk = try? reader.read(upToCount: 64 * 1024), !chunk.isEmpty {
                if !buffer.append(chunk, limit: Self.outputLimit) {
                    try? reader.close()  // the checker gets EPIPE instead of blocking on a full pipe
                    break
                }
            }
            buffer.finish()
        }

        if exited.wait(timeout: .now() + timeout) == .timedOut {
            process.terminate()
            if exited.wait(timeout: .now() + killGrace) == .timedOut {
                kill(process.processIdentifier, SIGKILL)
                _ = exited.wait(timeout: .now() + killGrace)
            }
            throw ControlError("The setup checker timed out.")
        }
        guard buffer.waitFinished(timeout: killGrace) else { throw ControlError("The setup checker's output didn't finish.") }
        let (data, overflow) = buffer.snapshot
        guard !overflow else { throw ControlError("The setup checker returned too much output.") }
        guard process.terminationStatus == 0 else { throw ControlError("The setup checker failed.") }
        return try SetupReport.decode(data)
    }
}

private final class OutputBuffer: @unchecked Sendable {
    private let lock = NSLock()
    private let done = DispatchSemaphore(value: 0)
    private var data = Data(), overflow = false

    func append(_ chunk: Data, limit: Int) -> Bool {
        lock.lock(); defer { lock.unlock() }
        guard data.count + chunk.count <= limit else { overflow = true; return false }
        data.append(chunk); return true
    }
    func finish() { done.signal() }
    func waitFinished(timeout: TimeInterval) -> Bool { done.wait(timeout: .now() + timeout) == .success }
    var snapshot: (Data, Bool) { lock.lock(); defer { lock.unlock() }; return (data, overflow) }
}
