import Foundation
import ServiceManagement

/// Thin wrapper over `SMAppService` for the bundled LaunchDaemon. Reading status has no side effects;
/// `register()` is only called from an explicit user action.
public protocol BackgroundServiceRegistering: Sendable {
    var status: RegistrationStatus { get }
    func register() throws
    func unregister() async throws
    func openApprovalSettings()
}

public struct DaemonRegistration: BackgroundServiceRegistering {
    public let plistName: String
    public init(plistName: String) { self.plistName = plistName }

    private var service: SMAppService { SMAppService.daemon(plistName: plistName) }

    public var status: RegistrationStatus { RegistrationStatus(service.status) }

    /// The status macOS reports after a failed call decides the outcome first; error codes only refine real failures.
    public func register() throws {
        do { try service.register() }
        catch { try RegistrationOutcome(error: error, statusAfter: status).check() }
    }

    /// Stops and removes the registered daemon. Already-unregistered is not an error.
    public func unregister() async throws {
        do { try await service.unregister() }
        catch where RegistrationOutcome.isServiceManagementError(error, code: Int(kSMErrorJobNotFound)) {}
        catch { throw ControlError("macOS couldn't remove the background service.") }
    }

    public func openApprovalSettings() { SMAppService.openSystemSettingsLoginItems() }
}

extension RegistrationStatus {
    public init(_ status: SMAppService.Status) {
        switch status {
        case .enabled: self = .enabled
        case .requiresApproval: self = .requiresApproval
        case .notFound: self = .notFound
        case .notRegistered: self = .notRegistered
        @unknown default: self = .unsupported
        }
    }
}

/// How a failed `register()` call should be presented. Pending approval and an existing registration are not
/// failures. Neither is success either: the app still waits for the authenticated root socket.
public enum RegistrationOutcome: Equatable, Sendable {
    case alreadyRegistered
    case needsApproval
    case failed(String)

    static let domains: Set<String> = ["SMAppServiceErrorDomain", "kSMErrorDomainFramework"]

    static func isServiceManagementError(_ error: Error, code: Int) -> Bool {
        let error = error as NSError
        return domains.contains(error.domain) && error.code == code
    }

    public init(error: Error, statusAfter: RegistrationStatus) {
        switch statusAfter {
        case .requiresApproval: self = .needsApproval; return
        case .enabled: self = .alreadyRegistered; return
        case .notRegistered, .notFound, .unsupported: break
        }
        let generic = "macOS couldn't register the background service."
        let ns = error as NSError
        guard Self.domains.contains(ns.domain) else { self = .failed(generic); return }
        switch ns.code {
        case Int(kSMErrorInvalidSignature):
            self = .failed("macOS rejected this copy of Tailmulti's signature, so it can't run the background service.")
        case Int(kSMErrorAuthorizationFailure):
            self = .failed("macOS didn't authorize the background service.")
        case Int(kSMErrorLaunchDeniedByUser):
            self = .failed("macOS didn't allow the background service. Check Login Items in System Settings.")
        case Int(kSMErrorJobPlistNotFound), Int(kSMErrorInvalidPlist), Int(kSMErrorToolNotValid):
            self = .failed("The background service in this copy of Tailmulti isn't valid.")
        default:
            self = .failed(generic)
        }
    }

    /// Throws only for real failures.
    func check() throws {
        if case .failed(let message) = self { throw ControlError(message) }
    }
}
