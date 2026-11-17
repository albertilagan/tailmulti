import SwiftUI
import AppKit
import TailmultiKit

struct SettingsView: View {
    var body: some View {
        TabView {
            GeneralSettings().tabItem { Label("General", systemImage: "gearshape") }
            RoutingSettings().tabItem { Label("Routing", systemImage: "arrow.triangle.branch") }
            DiagnosticsSettings().tabItem { Label("Diagnostics", systemImage: "stethoscope") }
        }
        .frame(width: 520, height: 400)
    }
}

private struct GeneralSettings: View {
    @EnvironmentObject var model: EngineModel
    @EnvironmentObject var updater: AppUpdater
    @State private var confirmingRemoval = false
    @AppStorage(DockVisibility.key) private var showInDock = true

    var body: some View {
        Form {
            Section {
                if model.canShowAccounts {
                    NetworkToggle()
                    NetworkErrorText()
                    if model.isNetworkingActive, let health = model.status?.health, health != "ready" {
                        Text(health).foregroundStyle(.orange).textSelection(.enabled)
                    }
                } else {
                    LabeledContent("Status", value: setupState)
                    if case .serviceProblem(let message) = model.stage {
                        Text(message).foregroundStyle(.secondary)
                    }
                }
            } footer: {
                if model.canShowAccounts {
                    Text("Turning Tailmulti off disconnects every account and returns DNS to normal. Sign-ins are kept, and the accounts that were on reconnect when you turn it back on.")
                        .foregroundStyle(.secondary)
                }
            }
            Section {
                Toggle("Show in Dock", isOn: $showInDock)
                    .onChange(of: showInDock) { _, shown in DockVisibility.apply(shown) }
            } footer: {
                Text("When hidden, open Tailmulti from its menu bar icon.").foregroundStyle(.secondary)
            }
            CommandLineSection()
            if updater.isAvailable {
                Section("Updates") {
                    Toggle("Check for updates automatically", isOn: Binding(
                        get: { updater.automaticallyChecks }, set: { updater.automaticallyChecks = $0 }))
                    LabeledContent("Version \(Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "")") {
                        Button("Check for Updates...") { updater.checkForUpdates() }.disabled(!updater.canCheck)
                    }
                }
            }
            Section("Privacy") {
                Text("Sign-in keys stay inside the background service. This app never reads them.")
                Text(updater.isAvailable
                     ? "Remote logging is off. Update checks fetch only Tailmulti's signed release feed."
                     : "Remote logging is off. This build doesn't check for updates.")
                Text("Quitting the app leaves Tailmulti on or off as it is.")
            }
            .foregroundStyle(.secondary)
            if model.canRemoveService {
                Section {
                    DisclosureGroup("Recovery") {
                        LabeledContent {
                            Button("Remove...") { confirmingRemoval = true }.disabled(model.busy || model.networkTransition)
                        } label: {
                            Text("Remove background service")
                            Text("For rebuilding or troubleshooting. You can allow it again later.").font(.caption).foregroundStyle(.secondary)
                        }
                    }
                } header: {
                    Text("Advanced")
                }
            }
        }
        .formStyle(.grouped)
        .confirmationDialog("Remove Tailmulti's background service?", isPresented: $confirmingRemoval) {
            Button("Remove Service", role: .destructive) { model.removeBackgroundService() }
        } message: {
            Text(removalMessage)
        }
    }

    private var removalMessage: String {
        let common = "The service stops and won't start again until you allow it. Account sign-ins stay saved."
        if model.stage == .enableNetworking {
            return "Tailmulti first returns DNS to normal. If that fails, the service is left in place.\n\n" + common
        }
        return "The service isn't running, so Tailmulti can't tidy up DNS settings an earlier run may have left.\n\n" + common
    }

    private var setupState: String {
        guard model.loaded else { return "Checking..." }
        switch model.stage {
        case .allowService, .cannotRegister: return "Background service not set up"
        case .awaitingApproval: return "Waiting for approval in System Settings"
        case .serviceStarting: return "Background service starting"
        case .serviceProblem: return "Background service not responding correctly"
        case .enableNetworking: return "Off"
        case .networkingActive: return "On"
        }
    }
}

private struct CommandLineSection: View {
    private let tool = CommandLineTool(bundle: Bundle.main.bundleURL)
    @State private var state = CommandLineTool.State.unavailable
    @State private var error: String?
    @State private var working = false

    var body: some View {
        if tool.state != .unavailable {
            Section {
                LabeledContent {
                    switch state {
                    case .installed: Button("Remove") { run { try tool.remove() } }
                    case .linkedElsewhere: Button("Reinstall...") { run { try tool.install() } }
                    default: Button("Install...") { run { try tool.install() } }
                    }
                } label: {
                    Text("tailmulti command")
                    Text(caption).font(.caption).foregroundStyle(.secondary)
                }
                .disabled(working)
                if let error { Text(error).foregroundStyle(.orange).textSelection(.enabled) }
            } header: {
                Text("Command Line")
            }
            .onAppear { state = tool.state }
        }
    }

    private var caption: String {
        switch state {
        case .installed: "Installed. Try \"tailmulti status\" in a new Terminal window."
        case .linkedElsewhere: "\(tool.link.path) points to another copy of Tailmulti."
        case .blocked: "\(tool.link.path) is in use by something else."
        default: "Control Tailmulti from Terminal. Adds a link at \(tool.link.path); macOS may ask for an administrator password."
        }
    }

    @MainActor private func finish(_ failure: String?) { error = failure; state = tool.state; working = false }

    private func run(_ change: @escaping @Sendable () throws -> Bool) {
        working = true; error = nil
        // The administrator prompt blocks until answered, so it stays off the main thread.
        Task.detached {
            var failure: String?
            do { _ = try change() } catch { failure = error.localizedDescription }
            await finish(failure)
        }
    }
}

private struct RoutingSettings: View {
    @EnvironmentObject var model: EngineModel

    var body: some View {
        Form {
            Section("How names resolve") {
                Text("While Tailmulti is on, names that lead to a device in one of your tailnets go through that account. Other internet traffic keeps its usual route.")
                Text("Traffic never passes from one account's tailnet into another.")
            }
            Section("Not supported yet") {
                Text("Subnet routes, exit nodes, short hostnames and custom company DNS forwarding.")
                    .foregroundStyle(.secondary)
            }
            if let conflicts = model.status?.conflicts, !conflicts.isEmpty {
                Section("Notices") {
                    ForEach(Array(conflicts.enumerated()), id: \.offset) { _, conflict in
                        Label(conflict, systemImage: "exclamationmark.triangle").foregroundStyle(.orange)
                    }
                }
            }
        }
        .formStyle(.grouped)
    }
}

private struct DiagnosticsSettings: View {
    @EnvironmentObject var model: EngineModel

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("A redacted engine report for troubleshooting. It leaves out account names, IDs and sign-in links.")
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
            HStack {
                Button(model.diagnostics.isEmpty ? "Load Report" : "Reload") { model.loadDiagnostics() }
                    .disabled(model.status == nil || model.busy)
                Button("Copy") {
                    NSPasteboard.general.clearContents()
                    NSPasteboard.general.setString(model.diagnostics, forType: .string)
                }
                .disabled(model.diagnostics.isEmpty)
            }
            ScrollView {
                Text(model.diagnostics.isEmpty ? " " : model.diagnostics)
                    .font(.system(.caption, design: .monospaced))
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(8)
            }
            .background(.background.secondary, in: RoundedRectangle(cornerRadius: 6))
            .overlay(RoundedRectangle(cornerRadius: 6).strokeBorder(.separator))
        }
        .padding(20)
    }
}
