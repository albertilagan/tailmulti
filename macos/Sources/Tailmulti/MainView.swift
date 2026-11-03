import SwiftUI
import TailmultiKit

struct MainView: View {
    @EnvironmentObject var model: EngineModel

    var body: some View {
        Group {
            if !model.loaded {
                ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
            } else if !model.canShowAccounts {
                SetupView()
            } else if model.accounts.isEmpty {
                WelcomeView()
            } else {
                AccountsSplitView()
            }
        }
        .toolbar {
            if model.canShowAccounts {
                ToolbarItem(placement: .primaryAction) { ToolbarNetworkToggle() }
            }
        }
        .sheet(isPresented: $model.showingAdd) { AddAccountSheet() }
        .alert("Something went wrong", isPresented: Binding(get: { model.actionError != nil }, set: { if !$0 { model.actionError = nil } })) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(model.actionError ?? "")
        }
    }
}

struct AccountsSplitView: View {
    @EnvironmentObject var model: EngineModel

    var body: some View {
        NavigationSplitView {
            List(selection: $model.selection) {
                ForEach(model.accounts) { account in
                    AccountRow(account: account, label: model.statusLabel(account), color: model.statusColor(account))
                        .tag(account.id)
                }
            }
            .navigationSplitViewColumnWidth(min: 180, ideal: 210, max: 280)
            .safeAreaInset(edge: .bottom) {
                if !model.labelOfferDismissed, !model.missingPreviewNames.isEmpty {
                    SidebarNamesOffer(count: model.missingPreviewNames.count)
                }
            }
            .toolbar {
                ToolbarItem {
                    Button { model.showingAdd = true } label: { Label("Add Account", systemImage: "plus") }
                        .help(model.isNetworkingActive ? "Add account" : "Turn on Tailmulti to add an account")
                        .disabled(model.busy || !model.isNetworkingActive)
                }
            }
        } detail: {
            Group {
                if let account = model.accounts.first(where: { $0.id == model.selection }) {
                    AccountDetailView(account: account)
                } else {
                    ContentUnavailableView("No Account Selected", systemImage: "sidebar.left")
                }
            }
            // The detail toolbar backdrop is inset from the column divider, so hide it to keep one straight edge.
            .toolbarBackground(.hidden, for: .windowToolbar)
        }
    }
}

private struct SidebarNamesOffer: View {
    @EnvironmentObject var model: EngineModel
    let count: Int

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(count == 1 ? "1 name saved in preview" : "\(count) names saved in preview")
                .font(.callout.weight(.medium))
            Text("Copying adds names only. Each still needs its own sign-in.")
                .font(.caption)
                .foregroundStyle(.secondary)
            HStack {
                Button("Copy") { model.copyPreviewNames() }.disabled(model.busy)
                Button("Not Now") { model.labelOfferDismissed = true }.buttonStyle(.link)
            }
            .controlSize(.small)
        }
        .padding(10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.background.secondary, in: RoundedRectangle(cornerRadius: 8))
        .padding(10)
    }
}

struct AccountRow: View {
    let account: Account
    let label: String
    let color: Color

    var body: some View {
        HStack(spacing: 10) {
            AccountMonogram(name: account.name, size: 26)
            VStack(alignment: .leading, spacing: 1) {
                Text(account.name).lineLimit(1)
                HStack(spacing: 4) {
                    StatusDot(color: color)
                    Text(label)
                }
                .font(.caption)
                .foregroundStyle(.secondary)
            }
        }
        .padding(.vertical, 3)
        .accessibilityElement(children: .combine)
    }
}

struct WelcomeView: View {
    @EnvironmentObject var model: EngineModel

    var body: some View {
        VStack(spacing: 18) {
            if model.isNetworkingActive, let health = model.status?.health, health != "ready" {
                HealthBanner(health: health).frame(maxWidth: 420)
            }
            NetworkErrorText().frame(maxWidth: 420)
            Image(systemName: "point.3.connected.trianglepath.dotted")
                .font(.system(size: 44, weight: .light))
                .foregroundStyle(.tint)
                .accessibilityHidden(true)
            VStack(spacing: 6) {
                Text("Add your first account")
                    .font(.title2.weight(.semibold))
                Text(model.isNetworkingActive
                     ? "Each Tailscale account signs in on its own and keeps its own devices. Add one for home and one for work."
                     : "Turn on Tailmulti, then add an account for home and one for work. Each signs in on its own.")
                    .multilineTextAlignment(.center)
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: 340)
            }
            Button { model.showingAdd = true } label: {
                Text("Add Account...").padding(.horizontal, 8)
            }
            .buttonStyle(.borderedProminent)
            .controlSize(.large)
            .keyboardShortcut(.defaultAction)
            .disabled(model.busy || !model.isNetworkingActive)
            if !model.labelOfferDismissed, !model.missingPreviewNames.isEmpty {
                PreviewNamesNote(names: model.missingPreviewNames).frame(maxWidth: 460).padding(.top, 8)
            }
        }
        .padding(40)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

struct AddAccountSheet: View {
    @EnvironmentObject var model: EngineModel
    @Environment(\.dismiss) private var dismiss
    @State private var name = ""
    private var trimmed: String { name.trimmingCharacters(in: .whitespacesAndNewlines) }

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            Text("Add Account").font(.headline)
            TextField("Name", text: $name, prompt: Text("Home, Work..."))
                .textFieldStyle(.roundedBorder)
                .onSubmit(submit)
            Text("The name is just a label for this Mac. Tailscale sign-in opens in your browser next.")
                .font(.callout)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
            HStack {
                Spacer()
                Button("Cancel", role: .cancel) { dismiss() }
                    .keyboardShortcut(.cancelAction)
                Button("Sign In...", action: submit)
                    .keyboardShortcut(.defaultAction)
                    .disabled(trimmed.isEmpty || model.busy)
            }
        }
        .padding(20)
        .frame(width: 360)
    }

    private func submit() {
        guard !trimmed.isEmpty, !model.busy else { return }
        model.addAndSignIn(trimmed)
        dismiss()
    }
}

struct AccountMonogram: View {
    let name: String
    var size: CGFloat = 26

    var body: some View {
        Text(name.first.map { String($0).uppercased() } ?? "?")
            .font(.system(size: size * 0.46, weight: .semibold, design: .rounded))
            .foregroundStyle(.white)
            .frame(width: size, height: size)
            .background(Circle().fill(color.gradient))
            .accessibilityHidden(true)
    }

    // Stable per-name hue so accounts stay visually distinct across launches.
    private var color: Color {
        let palette: [Color] = [.blue, .indigo, .teal, .orange, .pink, .purple, .green, .brown]
        let sum = name.unicodeScalars.reduce(0) { $0 &+ Int($1.value) }
        return palette[sum % palette.count]
    }
}

struct StatusDot: View {
    let color: Color

    var body: some View {
        Circle().fill(color).frame(width: 7, height: 7).accessibilityHidden(true)
    }
}

extension ConnectionPhase {
    var color: Color {
        switch self {
        case .connected: .green
        case .connecting: .blue
        case .signInRequired, .awaitingApproval: .orange
        case .unavailable: .red
        case .off, .setupRequired, .other: .secondary
        }
    }
}

struct HealthBanner: View {
    let health: String

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Image(systemName: "exclamationmark.octagon.fill").foregroundStyle(.red)
            VStack(alignment: .leading, spacing: 2) {
                Text("Networking paused").fontWeight(.semibold)
                Text("No traffic reaches any account's devices. Engine: \(health)")
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
                    .textSelection(.enabled)
            }
            Spacer(minLength: 8)
            SettingsLink { Text("Details") }.buttonStyle(.link)
        }
        .font(.callout)
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
        .background(.red.opacity(0.1), in: RoundedRectangle(cornerRadius: 8))
        .accessibilityElement(children: .combine)
    }
}
