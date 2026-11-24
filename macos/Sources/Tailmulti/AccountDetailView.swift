import SwiftUI
import AppKit
import TailmultiKit

struct AccountDetailView: View {
    @EnvironmentObject var model: EngineModel
    let account: Account

    var body: some View {
        Group {
            if model.isNetworkingActive {
                ActiveAccountView(account: account)
            } else {
                OffAccountView(account: account)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
        .navigationTitle(account.name)
        .confirmationDialog("Remove \(model.pendingRemoval?.name ?? "account")?",
                            isPresented: Binding(get: { model.pendingRemoval != nil }, set: { if !$0 { model.pendingRemoval = nil } }),
                            presenting: model.pendingRemoval) { account in
            Button("Remove", role: .destructive) { model.act("remove", account) }
        } message: { _ in
            Text("Tailmulti forgets this account. Its sign-in files stay on this Mac for recovery.")
        }
    }
}

/// Shown while the global switch is off: nothing connects, existing sign-ins are kept.
private struct OffAccountView: View {
    @EnvironmentObject var model: EngineModel
    let account: Account

    private var offMessage: String {
        if account.hasIdentity == false { return "\(account.name) isn't signed in yet. Once Tailmulti is on, sign in here." }
        return account.enabled
            ? "\(account.name) keeps its sign-in and reconnects when Tailmulti is on."
            : "\(account.name) keeps its sign-in. Once Tailmulti is on, connect it here."
    }

    var body: some View {
        VStack(spacing: 12) {
            Image(systemName: "power.circle")
                .font(.system(size: 40, weight: .light))
                .foregroundStyle(.secondary)
                .accessibilityHidden(true)
            Text(model.networkTransition ? "Turning on..." : "Tailmulti is off").font(.title3.weight(.semibold))
            Text(offMessage)
                .multilineTextAlignment(.center)
                .foregroundStyle(.secondary)
                .frame(maxWidth: 360)
            NetworkErrorText().frame(maxWidth: 360)
        }
        .padding(32)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

private struct DeviceSearchFocusKey: FocusedValueKey {
    typealias Value = () -> Void
}

extension FocusedValues {
    var focusDeviceSearch: (() -> Void)? {
        get { self[DeviceSearchFocusKey.self] }
        set { self[DeviceSearchFocusKey.self] = newValue }
    }
}

private struct ActiveAccountView: View {
    @EnvironmentObject var model: EngineModel
    let account: Account
    @State private var searchText = ""
    @FocusState private var searchFocused: Bool

    private var allDevices: [Device] {
        DeviceMenuCatalog.devices(model.status?.devices ?? [], accountID: account.id, query: "")
    }

    private var trimmedQuery: String { DeviceMenuCatalog.trimmed(searchText) }

    private var filteredDevices: [Device] {
        trimmedQuery.isEmpty ? allDevices : allDevices.filter { DeviceMenuCatalog.matches($0, trimmedQuery: trimmedQuery) }
    }

    private var canSearch: Bool { !allDevices.isEmpty && !model.networkingBlocked }

    var body: some View {
        VStack(spacing: 0) {
            statusBar
                .padding(.horizontal, 20)
                .padding(.vertical, 10)
            if model.networkError != nil {
                NetworkErrorText().frame(maxWidth: .infinity, alignment: .leading).padding(.horizontal, 20).padding(.bottom, 10)
            }
            if let health = model.status?.health, health != "ready" {
                HealthBanner(health: health).padding(.horizontal, 20).padding(.bottom, 10)
            }
            if let conflicts = model.status?.conflicts, !conflicts.isEmpty {
                ConflictBanner(conflicts: conflicts).padding(.horizontal, 20).padding(.bottom, 10)
            }
            if canSearch {
                searchBar
                    .padding(.horizontal, 20)
                    .padding(.bottom, 10)
            }
            Divider()
            if allDevices.isEmpty || model.networkingBlocked {
                emptyState.frame(maxWidth: .infinity, maxHeight: .infinity)
            } else if filteredDevices.isEmpty {
                ContentUnavailableView.search(text: trimmedQuery)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                DeviceTable(devices: filteredDevices)
            }
        }
        .focusedSceneValue(\.focusDeviceSearch, canSearch ? { searchFocused = true } : nil)
        .onChange(of: account.id) { _, _ in
            searchText = ""
            searchFocused = false
        }
    }

    private var searchBar: some View {
        HStack(spacing: 8) {
            HStack(spacing: 6) {
                Image(systemName: "magnifyingglass")
                    .foregroundStyle(.secondary)
                    .accessibilityHidden(true)
                TextField("Search name or IP", text: $searchText)
                    .textFieldStyle(.plain)
                    .focused($searchFocused)
                    .onExitCommand {
                        if !searchText.isEmpty { searchText = "" }
                        else { searchFocused = false }
                    }
                    .accessibilityLabel("Search devices by name or IP")
                if !searchText.isEmpty {
                    Button {
                        searchText = ""
                    } label: {
                        Image(systemName: "xmark.circle.fill")
                            .foregroundStyle(.secondary)
                    }
                    .buttonStyle(.plain)
                    .help("Clear search")
                    .accessibilityLabel("Clear search")
                }
            }
            .padding(.horizontal, 8)
            .padding(.vertical, 5)
            .background(.background.secondary, in: RoundedRectangle(cornerRadius: 7))
            .overlay(RoundedRectangle(cornerRadius: 7).strokeBorder(.separator))
            if !trimmedQuery.isEmpty {
                Text("\(filteredDevices.count) of \(allDevices.count)")
                    .font(.callout)
                    .foregroundStyle(.secondary)
                    .monospacedDigit()
                    .lineLimit(1)
            }
        }
    }

    private var statusBar: some View {
        HStack(spacing: 8) {
            StatusDot(color: model.statusColor(account))
            Text(model.statusLabel(account)).fontWeight(.medium)
            if let summary {
                Text(summary).foregroundStyle(.secondary).lineLimit(1)
            }
            Spacer(minLength: 12)
            if model.busyAccount == account.id { ProgressView().controlSize(.small) }
            if account.phase == .signInRequired {
                Button("Sign In...") { model.signIn(account) }
                    .buttonStyle(.borderedProminent)
                    .disabled(locked)
            } else if account.enabled {
                Button("Disconnect") { model.toggle(account) }.disabled(locked)
            } else {
                Button("Connect") { model.toggle(account) }
                    .buttonStyle(.borderedProminent)
                    .disabled(locked)
            }
            Menu {
                Button("Remove Account...", role: .destructive) { model.pendingRemoval = account }
            } label: {
                Label("More", systemImage: "ellipsis.circle")
            }
            .menuStyle(.borderlessButton)
            .menuIndicator(.hidden)
            .labelStyle(.iconOnly)
            .fixedSize()
            .disabled(locked)
            .help("More actions")
        }
        .controlSize(.regular)
        .accessibilityElement(children: .contain)
    }

    private var locked: Bool { model.busy || model.networkTransition || !model.isNetworkingActive }

    private var summary: String? {
        guard account.phase == .connected, !model.networkingBlocked, !allDevices.isEmpty else { return nil }
        return "\(allDevices.filter(\.online).count) of \(allDevices.count) devices online"
    }

    @ViewBuilder private var emptyState: some View {
        switch account.phase {
        case .connecting:
            VStack(spacing: 10) { ProgressView(); Text("Connecting...").foregroundStyle(.secondary) }
        case .connected where model.networkingBlocked:
            message("Networking paused", "network.slash", "Devices can't be reached until the engine's routing problem is fixed.")
        case .connected:
            message("No other devices", "laptopcomputer.and.iphone", "This account is connected but has no other devices yet.")
        case .signInRequired:
            message("Sign in to Tailscale", "person.badge.key", "Sign In opens Tailscale's login page in your browser.")
        case .awaitingApproval:
            message("Waiting for approval", "hourglass", "A tailnet admin needs to approve this Mac.")
        case .unavailable:
            message("Connection problem", "exclamationmark.triangle", "This account stopped responding. Disconnect, then connect again.")
        case .setupRequired:
            message("Network setup needed", "lock.shield", "This Mac hasn't been set up for Tailmulti networking yet.")
        case .off:
            message("Not connected", "bolt.horizontal.circle", "Connect to reach this account's devices again. They appear here once it's connected.")
        case .other(let state):
            message(state, "questionmark.circle", "The engine reported a state this app doesn't recognize.")
        }
    }

    private func message(_ title: String, _ symbol: String, _ detail: String) -> some View {
        ContentUnavailableView(title, systemImage: symbol, description: Text(detail))
    }
}

struct DeviceTable: View {
    let devices: [Device]
    @State private var selection = Set<Device.ID>()

    var body: some View {
        Table(devices, selection: $selection) {
            TableColumn("Device") { device in
                HStack(spacing: 8) {
                    Image(systemName: "desktopcomputer")
                        .foregroundStyle(device.online ? Color.accentColor : .secondary)
                        .accessibilityHidden(true)
                    Text(device.name).lineLimit(1).truncationMode(.middle)
                }
            }
            .width(min: 150, ideal: 230)
            TableColumn("Local IP") { device in
                Text(device.primaryAddress)
                    .font(.system(.body, design: .monospaced))
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }
            .width(min: 105, ideal: 130)
            TableColumn("Tailscale IP") { device in
                Text(device.primaryTailscaleAddress)
                    .font(.system(.body, design: .monospaced))
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }
            .width(min: 105, ideal: 130)
            TableColumn("Status") { device in
                Text(device.online ? "Online" : "Offline")
                    .foregroundStyle(device.online ? .primary : .secondary)
            }
            .width(min: 55, ideal: 65, max: 85)
        }
        .tableStyle(.inset)
        .contextMenu(forSelectionType: Device.ID.self) { ids in
            if ids.count == 1, let device = devices.first(where: { ids.contains($0.id) }) {
                Button("Copy Name") { copy(device.name) }
                if !device.primaryAddress.isEmpty {
                    Button("Copy Local IP") { copy(device.primaryAddress) }
                }
                if !device.primaryTailscaleAddress.isEmpty {
                    Button("Copy Tailscale IP") { copy(device.primaryTailscaleAddress) }
                }
            }
        }
    }

    private func copy(_ value: String) {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(value, forType: .string)
    }
}

struct ConflictBanner: View {
    let conflicts: [String]
    @State private var showing = false

    var body: some View {
        HStack(spacing: 8) {
            Image(systemName: "exclamationmark.triangle.fill").foregroundStyle(.orange)
            Text(conflicts.count == 1 ? "1 routing notice" : "\(conflicts.count) routing notices")
            Spacer()
            Button("Show") { showing.toggle() }
                .buttonStyle(.link)
                .popover(isPresented: $showing, arrowEdge: .bottom) {
                    VStack(alignment: .leading, spacing: 8) {
                        ForEach(Array(conflicts.enumerated()), id: \.offset) { _, conflict in
                            Text(conflict).fixedSize(horizontal: false, vertical: true)
                        }
                    }
                    .padding(14)
                    .frame(width: 300, alignment: .leading)
                }
        }
        .font(.callout)
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
        .background(.orange.opacity(0.1), in: RoundedRectangle(cornerRadius: 8))
        .accessibilityElement(children: .contain)
    }
}
