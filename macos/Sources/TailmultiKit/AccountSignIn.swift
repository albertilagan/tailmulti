import Foundation

public protocol AccountSignInControl: Sendable {
    func status() async throws -> EngineStatus
    func action(_ action: String, id: String) async throws
    func login(id: String) async throws -> URL
}

extension ControlClient: AccountSignInControl {}

public enum AccountSignInResult: Equatable, Sendable {
    case browser(URL), connected, awaitingApproval
}

/// Prepares one account's browser sign-in. Starting its engine is part of this action,
/// not a separate user-facing Connect step. It never changes another account or global networking.
public struct AccountSignIn: Sendable {
    private let client: any AccountSignInControl
    private let timeout: Duration
    private let pollInterval: Duration
    private let wait: @Sendable (Duration) async throws -> Void

    public init(client: any AccountSignInControl, timeout: Duration = .seconds(30),
                pollInterval: Duration = .milliseconds(250),
                wait: @escaping @Sendable (Duration) async throws -> Void = { try await Task.sleep(for: $0) }) {
        self.client = client; self.timeout = timeout; self.pollInterval = pollInterval; self.wait = wait
    }

    public func begin(id: String) async throws -> AccountSignInResult {
        try Task.checkCancellation()
        let initial = try account(in: await client.status(), id: id)
        if initial.phase == .connected { return .connected }
        if initial.phase == .awaitingApproval { return .awaitingApproval }
        if !initial.enabled {
            try Task.checkCancellation()
            try await client.action("connect", id: id)
        }
        let deadline = ContinuousClock.now.advanced(by: timeout)
        repeat {
            try Task.checkCancellation()
            let current = try account(in: await client.status(), id: id)
            guard current.enabled else { throw ControlError("This account was disconnected before sign-in opened. Try Sign In again.") }
            if current.phase == .connected { return .connected }
            if current.phase == .awaitingApproval { return .awaitingApproval }
            if current.loginReady == true || current.loginReady == nil && current.phase == .signInRequired {
                let url = try await client.login(id: id)
                try Task.checkCancellation()
                return .browser(try ControlClient.loginURL(url.absoluteString))
            }
            if current.phase == .unavailable {
                throw ControlError("This account couldn't start sign-in. Disconnect it and try again.")
            }
            guard ContinuousClock.now < deadline else { break }
            try await wait(pollInterval)
        } while ContinuousClock.now < deadline
        throw ControlError("Tailscale hasn't supplied a sign-in page yet. Check your internet connection and try Sign In again.")
    }

    private func account(in status: EngineStatus, id: String) throws -> Account {
        guard status.mode == "active", status.health == "ready" else {
            throw ControlError("Turn Tailmulti on and resolve any connection problem before signing in.")
        }
        guard let account = status.accounts.first(where: { $0.id == id }) else {
            throw ControlError("This account is no longer available.")
        }
        return account
    }
}
