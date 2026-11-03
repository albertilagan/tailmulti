import Foundation
import Combine

public protocol NetworkControl: Sendable {
    func status() async throws -> EngineStatus
    func setup() async throws -> SetupReport
    func startNetwork() async throws
    func stopNetwork(restoreDNS: Bool) async throws
}

extension ControlClient: NetworkControl {}

@MainActor
public final class NetworkSwitch: ObservableObject {
    @Published public private(set) var pending: Bool?
    @Published public private(set) var error: String?
    private let client: any NetworkControl

    public init(client: any NetworkControl) { self.client = client }

    public func setEnabled(_ enabled: Bool, current: EngineStatus?) async -> EngineStatus? {
        guard pending == nil, let current, ["active", "inactive"].contains(current.mode) else { return nil }
        pending = enabled
        error = nil
        defer { pending = nil }
        do {
            let before = try await client.status()
            guard ["active", "inactive"].contains(before.mode) else {
                throw ControlError("The background service reported an unexpected mode.")
            }
            if enabled && before.mode != "active" {
                let report = try await client.setup()
                guard report.isReady else {
                    let blocker = report.checks.first { $0.state != .pass }
                    throw ControlError(blocker.map { $0.title + ". " + $0.detail } ?? "This Mac isn't ready to connect. Check the setup details.")
                }
                try await client.startNetwork()
            } else if !enabled {
                try await client.stopNetwork(restoreDNS: true)
            }
            let observed = try await client.status()
            if observed.mode != (enabled ? "active" : "inactive") {
                error = "The background service hasn't confirmed the change. Try again."
            } else if observed.health != "ready" {
                error = observed.health
            }
            return observed
        } catch {
            self.error = error.localizedDescription
            return try? await client.status()
        }
    }
}
