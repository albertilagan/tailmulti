import SwiftUI
import AppKit
import Combine
import TailmultiKit

enum EngineSource { case none, preview, service }

@MainActor
final class EngineModel: ObservableObject {
    @Published private(set) var status: EngineStatus?
    @Published private(set) var source: EngineSource = .none
    @Published private(set) var stage: SetupStage = .serviceStarting
    @Published private(set) var previewStatus: EngineStatus?
    @Published private(set) var loaded = false
    @Published private(set) var connectionError: String?
    @Published var actionError: String?
    // Menu operations can finish after the popover closes. Retain their errors per account
    // so reopening the menu shows the failure instead of discarding it with a view's state.
    @Published var menuAccountErrors: [String: String] = [:]
    @Published private(set) var busyAccount: String?
    @Published private(set) var signingInAccount: String?
    @Published private(set) var busy = false
    @Published var diagnostics = ""
    @Published var showingAdd = false
    @Published var selection: String?
    @Published var pendingRemoval: Account?

    @Published private(set) var setupReport: SetupReport?
    @Published private(set) var setupError: String?
    @Published private(set) var checking = false
    @Published private(set) var registration: RegistrationStatus = .notRegistered
    /// Nil while this bundle's service files are still being verified.
    @Published private(set) var bundleCheck: Result<VerifiedService, ControlError>?
    @Published var labelOfferDismissed = false

    private var refreshTask: Task<Void, Never>?
    private var refreshGeneration = 0
    private var lastNetworkIntent: Bool?
    @Published private(set) var networkTransition = false
    /// True while an outdated background service restarts into this app's bundled version.
    @Published private(set) var updatingService = false
    private var serviceBuildChecked = false
    private let networkControl = NetworkSwitch(client: ControlClient())
    private var networkChanges: AnyCancellable?
    let serviceClient = ControlClient()
    let previewClient: ControlClient?
    private let registrar = DaemonRegistration(plistName: ServiceBundle.label + ".plist")

    init() {
        if let path = ProcessInfo.processInfo.environment["TAILMULTI_PREVIEW_SOCKET"] {
            previewClient = ControlClient(path: path, serverUID: getuid())
        } else { previewClient = nil }
        networkChanges = networkControl.objectWillChange.sink { [weak self] in self?.objectWillChange.send() }
        verifyBundle()
        // Owned by the app scene, so polling continues after the window closes.
        refreshTask = Task { [weak self] in
            while !Task.isCancelled {
                await self?.refresh()
                try? await Task.sleep(for: .seconds(3))
            }
        }
    }

    /// Account and network actions go only to the trusted root service when it answers.
    var client: ControlClient { source == .preview ? previewClient ?? serviceClient : serviceClient }

    func refresh() async {
        guard !networkTransition else { return }
        let generation = refreshGeneration
        let probe: ServiceProbe
        do { probe = .running(try await serviceClient.status()) }
        catch { probe = ServiceProbe(result: .failure(error)) }
        if let previewClient { previewStatus = try? await previewClient.status() }

        let registrar = registrar
        let registration = await Task.detached { registrar.status }.value
        guard !networkTransition, generation == refreshGeneration else { return }
        self.registration = registration

        let previousStage = stage
        stage = SetupStage.resolve(probe: probe, registration: registration,
                                   bundle: bundleCheck ?? .failure(ControlError("Checking this copy of Tailmulti...")))
        switch probe {
        case .running(let service):
            status = service; source = .service; connectionError = nil
        default:
            if let previewStatus { status = previewStatus; source = .preview; connectionError = nil }
            else {
                status = nil; source = .none
                if case .serviceProblem(let message) = stage { connectionError = message } else { connectionError = nil }
            }
        }
        if stage.checkSource != previousStage.checkSource {
            // A different checker applies now; earlier results no longer do.
            setupReport = nil; setupError = nil
        }
        if stage != previousStage, !canShowAccounts { selection = nil }
        loaded = true
        menuAccountErrors = menuAccountErrors.filter { id, _ in accounts.contains { $0.id == id } }
        if let selection, !accounts.contains(where: { $0.id == selection }) { self.selection = nil }
        if selection == nil, canShowAccounts { selection = accounts.first?.id }
        if setupReport == nil, setupError == nil, !checking, stage.checkSource != nil { runChecks() }
        if !serviceBuildChecked, canShowAccounts, !busy, !networkTransition { Task { await updateServiceIfOutdated() } }
    }

    /// After the app updates, the running root service is still the previous version. Restart it once so
    /// launchd starts the updated executable, then turn networking back on if it was on.
    private func updateServiceIfOutdated() async {
        guard !serviceBuildChecked else { return }
        serviceBuildChecked = true
        guard let bundled = Bundle.main.object(forInfoDictionaryKey: "CFBundleVersion") as? String,
              let bundledNumber = Int(bundled),
              let running = try? await serviceClient.serviceBuild(),
              let runningNumber = Int(running), runningNumber < bundledNumber else { return }
        let wasOn = isNetworkingActive
        updatingService = true
        busy = true
        do {
            try await serviceClient.restartService()
            var updated = false
            for _ in 0..<45 where !updated {
                try? await Task.sleep(for: .seconds(1))
                updated = (try? await serviceClient.serviceBuild()) == bundled
            }
            if !updated { actionError = "The background service didn't restart into the updated version. Restart your Mac to finish updating." }
        } catch {
            actionError = "The background service couldn't restart to finish updating: \(error.localizedDescription)"
        }
        updatingService = false
        busy = false
        await refresh()
        if wasOn { setNetworkingEnabled(true) }
    }

    func perform(account: String? = nil, onError: ((String) -> Void)? = nil,
                 _ operation: @escaping () async throws -> Void) {
        guard !busy else { return }
        busy = true; busyAccount = account
        Task {
            do { try await operation(); await refresh() }
            catch {
                if let onError { onError(error.localizedDescription) }
                else { actionError = error.localizedDescription }
                await refresh()
            }
            busy = false; busyAccount = nil
        }
    }

    /// The client is captured when the user acts, so a later source change can't redirect the request.
    func act(_ action: String, _ account: Account, onError: ((String) -> Void)? = nil) {
        guard let client = accountClient else { return }
        perform(account: account.id, onError: onError) { try await client.action(action, id: account.id) }
    }
    func toggle(_ account: Account) { act(account.enabled ? "disconnect" : "connect", account) }
    func signIn(_ account: Account, onError: ((String) -> Void)? = nil) {
        guard let client = accountClient else { return }
        perform(account: account.id, onError: onError) { try await self.prepareSignIn(account.id, client: client) }
    }
    /// Adding an account needs a running engine, so Add Account turns Tailmulti on first when it is off.
    /// Returns whether networking is on; a failure's reason is in `networkError`.
    func turnOnForSignIn() async -> Bool {
        if isNetworkingActive { return true }
        guard networkSwitchEnabled else { return false }
        await switchNetworking(true)
        return isNetworkingActive
    }
    func addAndSignIn(_ name: String) {
        guard let client = accountClient else { return }
        perform {
            let created = try await client.add(name)
            self.selection = created.id
            self.busyAccount = created.id
            await self.refresh()
            try await self.prepareSignIn(created.id, client: client)
        }
    }
    private func prepareSignIn(_ id: String, client: ControlClient) async throws {
        signingInAccount = id
        defer { signingInAccount = nil }
        if case .browser(let url) = try await AccountSignIn(client: client).begin(id: id) {
            guard NSWorkspace.shared.open(url) else { throw ControlError("Couldn't open your browser. Try Sign In again.") }
        }
    }
    func loadDiagnostics() {
        let client = client
        perform { self.diagnostics = try await client.diagnostics() }
    }
    /// Account changes go only to the installed service while its networking is on.
    private var accountClient: ControlClient? { isNetworkingActive ? serviceClient : nil }

    // MARK: Setup

    static var bundledHelper: URL { Bundle.main.bundleURL.appendingPathComponent("Contents/Library/HelperTools/tailmultid") }

    func runChecks() {
        guard !checking, let source = stage.checkSource else { return }
        let serviceClient = serviceClient
        checking = true; setupError = nil
        Task {
            var report: SetupReport?, failure: String?
            do {
                switch source {
                case .service: report = try await serviceClient.setup()
                case .local: report = try await HostCheckRunner(executable: Self.bundledHelper).run()
                }
            } catch { failure = error.localizedDescription }
            checking = false
            // Results from a different checker are discarded, then checks run again for the current one.
            guard stage.checkSource == source else {
                setupReport = nil; setupError = nil
                runChecks()
                return
            }
            setupReport = report; setupError = failure
        }
    }

    nonisolated private static func verifyBundleSync() -> Result<VerifiedService, ControlError> {
        do { return .success(try ServiceBundle.verify(bundle: Bundle.main.bundleURL)) }
        catch let error as ControlError { return .failure(error) }
        catch { return .failure(ControlError("This copy of Tailmulti couldn't be checked.")) }
    }

    func verifyBundle() {
        Task {
            bundleCheck = await Task.detached { Self.verifyBundleSync() }.value
            await refresh()
        }
    }

    /// Registers the bundled LaunchDaemon. Only called from the user's explicit confirmation.
    func allowBackgroundService() {
        guard stage == .allowService else { return }
        let registrar = registrar
        perform {
            try await self.verifyForRegistration()
            try await Task.detached { try registrar.register() }.value
            if await Task.detached(operation: { registrar.status }).value == .requiresApproval {
                registrar.openApprovalSettings()
            }
        }
    }

    func openApprovalSettings() { registrar.openApprovalSettings() }

    /// Re-checks this bundle before registering; failures surface as an error instead of a silent no-op.
    private func verifyForRegistration() async throws {
        let verified = await Task.detached { Self.verifyBundleSync() }.value
        bundleCheck = verified
        switch verified {
        case .failure(let error): throw error
        case .success(let service) where !service.signingMode.canRegister || service.team == nil:
            throw ControlError("This copy of Tailmulti isn't signed in a way macOS accepts for background services.")
        case .success: break
        }
    }

    /// Replaces a registration macOS reports as enabled whose service never answers, such as one left by a
    /// deleted or moved copy of the app. Only called from the user's explicit action.
    func reinstallBackgroundService() {
        guard stage == .serviceStarting else { return }
        let registrar = registrar
        perform {
            try await self.verifyForRegistration()
            try await registrar.unregister()
            try await Task.detached { try registrar.register() }.value
            if await Task.detached(operation: { registrar.status }).value == .requiresApproval {
                registrar.openApprovalSettings()
            }
        }
    }

    /// Removal is offered only while networking is off. A running service first restores its owned
    /// DNS entries; if that fails, the service stays registered.
    var canRemoveService: Bool { stage.allowsServiceRemoval }

    func removeBackgroundService() {
        guard canRemoveService else { return }
        let restoreFirst = stage.restoresDNSBeforeRemoval
        let registrar = registrar, serviceClient = serviceClient
        perform {
            if restoreFirst { try await serviceClient.stopNetwork(restoreDNS: true) }
            try await registrar.unregister()
        }
    }

    func setNetworkingEnabled(_ enabled: Bool) {
        guard networkSwitchEnabled, enabled != isNetworkingActive || networkError != nil else { return }
        Task { await switchNetworking(enabled) }
    }

    private func switchNetworking(_ enabled: Bool) async {
        guard networkSwitchEnabled else { return }
        lastNetworkIntent = enabled
        busy = true
        networkTransition = true
        refreshGeneration += 1
        let current = status
        if let observed = await networkControl.setEnabled(enabled, current: current) {
            status = observed
            source = .service
            stage = SetupStage.resolve(probe: .running(observed), registration: registration,
                                       bundle: bundleCheck ?? .failure(ControlError("Checking this copy of Tailmulti...")))
        }
        setupReport = nil
        networkTransition = false
        busy = false
        refreshGeneration += 1
        await refresh()
    }

    func retryNetworking() {
        if let lastNetworkIntent { setNetworkingEnabled(lastNetworkIntent) }
    }

    /// Preview names the installed service lacks. Copying adds names only; nothing signs in or connects.
    var missingPreviewNames: [String] {
        guard source == .service, let previewStatus, let status else { return [] }
        return missingAccountNames(preview: previewStatus.accounts, service: status.accounts)
    }
    func copyPreviewNames() {
        let names = missingPreviewNames, serviceClient = serviceClient
        guard !names.isEmpty else { return }
        perform { for name in names { try await serviceClient.add(name) } }
    }

    static var tailscaleApp: URL? {
        ["io.tailscale.ipn.macos", "io.tailscale.ipn.macsys"].lazy.compactMap { NSWorkspace.shared.urlForApplication(withBundleIdentifier: $0) }.first
    }
    func openTailscale() {
        guard let url = Self.tailscaleApp else { return }
        NSWorkspace.shared.openApplication(at: url, configuration: NSWorkspace.OpenConfiguration())
    }

    // MARK: Derived state

    var isNetworkingActive: Bool { stage == .networkingActive && source == .service }
    var canShowAccounts: Bool { source == .service && (stage == .networkingActive || stage == .enableNetworking) }
    var networkSwitchIsOn: Bool { networkTransition ? lastNetworkIntent ?? isNetworkingActive : isNetworkingActive }
    var networkSwitchEnabled: Bool { loaded && canShowAccounts && !busy && !networkTransition }
    /// Add Account stays available while off; signing in turns Tailmulti on.
    var canAddAccount: Bool { networkSwitchEnabled }
    var networkError: String? { networkControl.error }
    var networkSummary: String {
        if updatingService { return "Finishing update..." }
        if networkTransition { return lastNetworkIntent == false ? "Disconnecting..." : "Connecting..." }
        switch stage {
        case .allowService, .cannotRegister: return "Setup needed"
        case .awaitingApproval: return "Waiting for approval"
        case .serviceStarting: return "Starting service..."
        case .serviceProblem: return "Service unavailable"
        case .enableNetworking: return "Not connected"
        case .networkingActive:
            if networkingBlocked { return "Connection problem" }
            if accounts.contains(where: { $0.enabled && $0.phase == .connecting }) { return "Connecting..." }
            if accounts.isEmpty { return "On" }
            return "\(connected) of \(accounts.count) connected"
        }
    }
    var accounts: [Account] { status?.accounts ?? [] }
    var isPreview: Bool { source == .preview }
    var connected: Int { isNetworkingActive && !networkingBlocked ? accounts.filter { $0.phase == .connected }.count : 0 }
    var needsAttention: Bool {
        if case .serviceProblem = stage { return true }
        return isNetworkingActive && (accounts.contains { $0.phase.needsAttention } || networkingBlocked)
    }
    var networkingBlocked: Bool { status.map { $0.health != "ready" } ?? false }
    // A signed-in account can't carry traffic while the engine has routing blocked.
    func statusLabel(_ account: Account) -> String {
        if networkTransition { return networkSummary }
        if signingInAccount == account.id { return "Opening sign-in..." }
        if !isNetworkingActive { return source == .service ? "Not connected" : "Not signed in" }
        return networkingBlocked && account.phase == .connected ? "Signed in, routing paused" : account.phase.label
    }
    func statusColor(_ account: Account) -> Color {
        if networkTransition { return .secondary }
        if !isNetworkingActive { return .secondary }
        return networkingBlocked && account.phase == .connected ? .orange : account.phase.color
    }
    func devices(for account: Account) -> [Device] { status?.devices.filter { $0.profileId == account.id } ?? [] }
}

/// Owns the model before any scene appears, so the status menu exists even without a window.
@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate {
    let model = EngineModel()
    let updater = AppUpdater()
    private var statusMenu: StatusMenuController?

    func applicationDidFinishLaunching(_ notification: Notification) {
        DockVisibility.apply(DockVisibility.isShown)
        statusMenu = StatusMenuController(model: model, updater: updater)
    }

    /// The status menu is the everyday interface, so closing the last window keeps the app running.
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { false }
}

/// Hiding the Dock icon makes Tailmulti a menu bar app; its windows still open from the status menu.
enum DockVisibility {
    static let key = "showInDock"

    static var isShown: Bool { UserDefaults.standard.object(forKey: key) as? Bool ?? true }

    @MainActor static func apply(_ shown: Bool) {
        NSApp.setActivationPolicy(shown ? .regular : .accessory)
        // Changing the policy deactivates the app, which would hide the Settings window it was changed from.
        NSApp.activate()
    }
}

@main
struct TailmultiApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) private var delegate

    var body: some Scene {
        Window("Tailmulti", id: "main") {
            MainView()
                .environmentObject(delegate.model)
                .registersSceneActions()
                .frame(minWidth: 620, minHeight: 400)
        }
        .defaultSize(width: 820, height: 540)
        .commands {
            CommandGroup(replacing: .newItem) { AddAccountCommand(model: delegate.model) }
            CommandGroup(after: .appInfo) { CheckForUpdatesCommand(updater: delegate.updater) }
        }

        Settings {
            SettingsView()
                .environmentObject(delegate.model)
                .environmentObject(delegate.updater)
                .registersSceneActions()
        }
    }
}

private struct AddAccountCommand: View {
    @ObservedObject var model: EngineModel

    var body: some View {
        Button("Add Account...") { model.showingAdd = true }
            .keyboardShortcut("n")
            .disabled(!model.canAddAccount)
    }
}
