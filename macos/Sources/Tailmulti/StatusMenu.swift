import AppKit
import Combine
import SwiftUI
import TailmultiKit

/// Native menu bar menu: global switch, one cascading submenu per account, then app commands.
@MainActor
final class StatusMenuController: NSObject, NSMenuDelegate {
    let model: EngineModel
    let updater: AppUpdater
    private let statusItem: NSStatusItem
    private let menu = NSMenu()
    private let header = MenuHeaderView()
    private lazy var headerItem = NSMenuItem.hosting(header)
    private let addItem = NSMenuItem(title: "Add Account…", action: #selector(addAccount), keyEquivalent: "")
    private let retryItem = NSMenuItem(title: "Retry", action: #selector(retryNetworking), keyEquivalent: "")
    private var accountMenus: [String: AccountMenu] = [:]
    private var layoutKey = ""
    private var isOpen = false
    private var refreshQueued = false
    private var changes: AnyCancellable?
    private let updates = MenuUpdateQueue()

    init(model: EngineModel, updater: AppUpdater) {
        self.model = model
        self.updater = updater
        statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.squareLength)
        super.init()
        statusItem.autosaveName = "dev.tailmulti.status"
        menu.delegate = self
        menu.autoenablesItems = false
        statusItem.menu = menu
        addItem.target = self
        retryItem.target = self
        header.onToggle = { [weak self] on in
            guard let self else { return }
            model.setNetworkingEnabled(on)
            self.updates.enqueue("header") { [weak self] in self?.updateRoot() }
        }
        // objectWillChange fires before the mutation; the task reads the new values. Tasks on the
        // main actor also run while a menu is tracking, so an open menu stays current.
        changes = model.objectWillChange.sink { [weak self] _ in
            Task { @MainActor in self?.requestRefresh() }
        }
        updateButton()
    }

    // MARK: NSMenuDelegate

    func menuNeedsUpdate(_ menu: NSMenu) { rebuildRoot() }
    func menuWillOpen(_ menu: NSMenu) {
        isOpen = true
        updates.opened()
    }
    // AppKit is still enumerating menus here; all cleanup waits for a later turn.
    func menuDidClose(_ menu: NSMenu) {
        isOpen = false
        updates.closed()
        updates.enqueue("reset", requiresOpen: false) { [weak self] in
            self?.accountMenus.values.forEach { $0.resetSearch() }
        }
    }

    // MARK: Refresh

    func requestRefresh() {
        guard !refreshQueued else { return }
        refreshQueued = true
        Task { @MainActor in
            refreshQueued = false
            updateButton()
            guard isOpen else { return }
            // Rebuilding would remove an open submenu's item; wait until it closes and asks again.
            if currentLayoutKey() == layoutKey || accountMenus.values.contains(where: \.isTracking) {
                updateRoot()
            } else {
                rebuildRoot()
            }
            accountMenus.values.forEach { $0.refreshIfOpen() }
        }
    }

    private func updateButton() {
        guard let button = statusItem.button else { return }
        let description = "Tailmulti, \(model.connected) connected"
        let image = model.needsAttention
            ? NSImage(systemSymbolName: "exclamationmark.triangle", accessibilityDescription: description)
            : Self.dotGrid(connected: model.connected > 0, description: description)
        image?.isTemplate = true
        button.image = image
        button.toolTip = "Tailmulti: \(model.networkSummary)"
    }

    /// The app icon's 4x4 dot grid as a monochrome template: both corner groups solid while connected,
    /// every dot faint while off.
    static func dotGrid(connected: Bool, description: String) -> NSImage {
        let image = NSImage(size: NSSize(width: 18, height: 18), flipped: true) { _ in
            let pitch: CGFloat = 4.4, radius: CGFloat = 1.5, start = 9 - 1.5 * pitch
            for row in 0..<4 {
                for column in 0..<4 {
                    let lit = connected && ((row < 2 && column < 2) || (row >= 2 && column >= 2))
                    NSColor.black.withAlphaComponent(lit ? 1 : 0.35).setFill()
                    let center = NSPoint(x: start + CGFloat(column) * pitch, y: start + CGFloat(row) * pitch)
                    NSBezierPath(ovalIn: NSRect(x: center.x - radius, y: center.y - radius, width: 2 * radius, height: 2 * radius)).fill()
                }
            }
            return true
        }
        image.accessibilityDescription = description
        return image
    }

    /// Items are rebuilt only when this changes, so an open submenu isn't torn down by status polls.
    private func currentLayoutKey() -> String {
        let ids = model.canShowAccounts ? model.accounts.map(\.id).joined(separator: ",") : "-"
        return "\(model.loaded)|\(model.canShowAccounts)|\(ids)|\(model.networkError ?? "")"
    }

    private func rebuildRoot() {
        menu.removeAllItems()
        layoutKey = currentLayoutKey()
        updateHeader()
        menu.addItem(headerItem)
        if let error = model.networkError {
            menu.addItem(.hosting(MenuMessageView(error)))
            retryItem.isEnabled = retryEnabled
            menu.addItem(retryItem)
        }
        menu.addItem(.separator())

        if model.canShowAccounts {
            let ids = Set(model.accounts.map(\.id))
            accountMenus = accountMenus.filter { ids.contains($0.key) }
            for account in model.accounts {
                let entry = accountMenus[account.id] ?? AccountMenu(id: account.id, owner: self)
                accountMenus[account.id] = entry
                entry.updateRootItem(account)
                menu.addItem(entry.rootItem)
            }
            addItem.isEnabled = model.canAddAccount
            menu.addItem(addItem)
            menu.addItem(.separator())
        } else {
            accountMenus = [:]
            if model.loaded {
                menu.addItem(item("Set Up Tailmulti…", #selector(openMain)))
                menu.addItem(.separator())
            }
        }

        menu.addItem(item("Open Tailmulti", #selector(openMain)))
        if updater.isAvailable {
            menu.addItem(item("Check for Updates…", #selector(checkForUpdates)))
        }
        menu.addItem(item("Settings…", #selector(openSettings), key: ","))
        menu.addItem(.separator())
        menu.addItem(item("Quit Tailmulti", #selector(quit), key: "q"))
    }

    private var retryEnabled: Bool { !model.networkTransition && model.networkSwitchEnabled }

    private func updateRoot() {
        updateHeader()
        retryItem.isEnabled = retryEnabled
        addItem.isEnabled = model.canAddAccount
        for account in model.accounts { accountMenus[account.id]?.updateRootItem(account) }
    }

    private func updateHeader() {
        header.update(summary: model.networkSummary, isOn: model.networkSwitchIsOn,
                      enabled: model.networkSwitchEnabled, working: model.networkTransition)
    }

    private func item(_ title: String, _ action: Selector, key: String = "") -> NSMenuItem {
        let item = NSMenuItem(title: title, action: action, keyEquivalent: key)
        item.target = self
        return item
    }

    // MARK: Actions

    @objc private func retryNetworking() {
        guard retryEnabled else { return }
        model.retryNetworking()
    }
    @objc private func addAccount() {
        model.showingAdd = true
        SceneActions.shared.showMain()
    }
    @objc func openMain() { SceneActions.shared.showMain() }
    @objc private func openSettings() { SceneActions.shared.showSettings() }
    @objc private func quit() { NSApp.terminate(nil) }
    @objc private func checkForUpdates() { updater.checkForUpdates() }

    func show(accountID: String) {
        model.selection = accountID
        SceneActions.shared.showMain()
    }
}

/// One account's submenu: its connection action, search, and only that account's devices.
@MainActor
final class AccountMenu: NSObject, NSMenuDelegate {
    let id: String
    let rootItem = NSMenuItem()
    private let menu = NSMenu()
    private unowned let owner: StatusMenuController
    private var model: EngineModel { owner.model }
    private let search = MenuSearchView(placeholder: "Search devices")
    private lazy var searchItem = NSMenuItem.hosting(search)
    private lazy var lifecycle = DeviceSearchCoordinator(section: DeviceMenuSection(
        accountID: id, target: self, copyAction: #selector(copyValue(_:)), submenuDelegate: self))
    private var section: DeviceMenuSection { lifecycle.section }
    private var actionItem: NSMenuItem?
    private var headKey = ""

    var isTracking: Bool { lifecycle.isTracking }

    init(id: String, owner: StatusMenuController) {
        self.id = id
        self.owner = owner
        super.init()
        menu.delegate = self
        menu.autoenablesItems = false
        rootItem.submenu = menu
        search.field.target = self
        search.field.action = #selector(searchChanged)
        lifecycle.currentQuery = { [search] in search.field.stringValue }
        lifecycle.refresh = { [weak self] in self?.refreshIfOpen() }
    }

    private var account: Account? { model.accounts.first { $0.id == id } }

    /// Devices are shown only while this account can actually carry traffic.
    private func reachable(_ account: Account) -> Bool {
        model.isNetworkingActive && !model.networkingBlocked && !model.networkTransition && account.phase == .connected
    }

    private func engineDevices(_ account: Account) -> [Device] {
        reachable(account) ? model.status?.devices ?? [] : []
    }

    func updateRootItem(_ account: Account) {
        rootItem.title = account.name
        var subtitle = model.statusLabel(account)
        if reachable(account) {
            let devices = model.devices(for: account)
            if !devices.isEmpty { subtitle += " · \(devices.filter(\.online).count) of \(devices.count) online" }
        }
        if model.menuAccountErrors[id] != nil { subtitle += " · Action failed" }
        rootItem.subtitle = subtitle
        rootItem.image = MenuImages.monogram(name: account.name, badge: NSColor(model.statusColor(account)))
        rootItem.setAccessibilityLabel("\(account.name), \(subtitle)")
    }

    /// Called only after the root menu has closed.
    func resetSearch() {
        guard !isTracking else { return }
        search.field.stringValue = ""
        lifecycle.reset()
    }

    // MARK: NSMenuDelegate (this account's submenu and its device submenus)

    func menuNeedsUpdate(_ menu: NSMenu) {
        if menu === self.menu { rebuild() }
    }

    func menuWillOpen(_ menu: NSMenu) {
        guard menu === self.menu else { lifecycle.childWillOpen(); return }
        lifecycle.parentWillOpen { [weak self] in
            guard let self, self.menu.index(of: self.searchItem) >= 0 else { return }
            self.search.window?.makeFirstResponder(self.search.field)
        }
    }

    // Closing resigns the search field, which may send its action; nothing here mutates items.
    func menuDidClose(_ menu: NSMenu) {
        guard menu === self.menu else { lifecycle.childDidClose(); return }
        lifecycle.parentDidClose()
        owner.requestRefresh()
    }

    // MARK: Building

    /// Runs only from deferred work (status refresh task or the lifecycle queue).
    func refreshIfOpen() {
        guard lifecycle.isOpen, let account else { return }
        actionItem?.isEnabled = actionEnabled
        let headChanged = makeHeadKey(account) != headKey
        let all = engineDevices(account)
        let devicesChanged = section.needsUpdate(all)
        let queryPending = search.field.stringValue != section.query
        guard headChanged || devicesChanged || queryPending else { lifecycle.clearStale(); return }
        // Removing or hiding a device item would tear down its open copy submenu mid-tracking.
        guard lifecycle.canMutate else { lifecycle.markStale(); return }
        lifecycle.clearStale()
        if headChanged {
            rebuild()
        } else if devicesChanged {
            replaceDeviceItems(all)
        } else {
            section.apply(query: search.field.stringValue)
        }
    }

    private var actionEnabled: Bool {
        !(model.busy || model.networkTransition || !model.isNetworkingActive)
    }

    private func makeHeadKey(_ account: Account) -> String {
        let browsable = reachable(account) && !model.devices(for: account).isEmpty
        return "\(actionTitle(account))|\(model.menuAccountErrors[id] ?? "")|\(browsable)|\(unavailableText(account))"
    }

    /// Called from menuNeedsUpdate (before opening) or a deferred refresh, never from a close callback.
    private func rebuild() {
        menu.removeAllItems()
        actionItem = nil
        guard let account else { return }
        headKey = makeHeadKey(account)

        let action = NSMenuItem(title: actionTitle(account), action: #selector(performAction), keyEquivalent: "")
        action.target = self
        action.isEnabled = actionEnabled
        actionItem = action
        menu.addItem(action)
        if let error = model.menuAccountErrors[id] {
            menu.addItem(.hosting(MenuMessageView(error)))
            let dismiss = NSMenuItem(title: "Dismiss Message", action: #selector(dismissError), keyEquivalent: "")
            dismiss.target = self
            menu.addItem(dismiss)
        }
        menu.addItem(.separator())

        if reachable(account) && !model.devices(for: account).isEmpty {
            menu.addItem(searchItem)
            let all = engineDevices(account)
            if section.needsUpdate(all) { section.update(all) }
            section.items.forEach { menu.addItem($0) }
            section.apply(query: search.field.stringValue)
        } else {
            menu.addItem(.note(unavailableText(account)))
        }

        menu.addItem(.separator())
        let show = NSMenuItem(title: "Show in Tailmulti", action: #selector(showInWindow), keyEquivalent: "")
        show.target = self
        menu.addItem(show)
    }

    /// Structural change only when the engine's device set changes, not while typing.
    private func replaceDeviceItems(_ all: [Device]) {
        let start = menu.index(of: searchItem)
        guard start >= 0 else { rebuild(); return }
        section.items.forEach { if $0.menu === menu { menu.removeItem($0) } }
        section.update(all)
        section.apply(query: search.field.stringValue)
        for (offset, item) in section.items.enumerated() { menu.insertItem(item, at: start + 1 + offset) }
    }

    private func actionTitle(_ account: Account) -> String {
        if account.phase == .signInRequired { return "Sign In…" }
        return account.enabled ? "Disconnect" : "Connect"
    }

    private func unavailableText(_ account: Account) -> String {
        if model.networkTransition { return model.networkSummary }
        if !model.isNetworkingActive { return "Turn on Tailmulti to see devices" }
        if model.networkingBlocked { return "Devices unavailable while routing is paused" }
        switch account.phase {
        case .connected: return "No other devices"
        case .connecting: return "Devices appear once connected"
        case .signInRequired: return "Sign in to see devices"
        case .awaitingApproval: return "Waiting for admin approval"
        default: return "Connect to see devices"
        }
    }

    // MARK: Actions

    /// Applied on the next main-actor turn; the field also sends this while resigning during dismissal.
    @objc private func searchChanged() { lifecycle.textChanged() }

    @objc private func performAction() {
        guard actionEnabled, let account else { return }
        let model = model, id = id
        model.menuAccountErrors[id] = nil
        let onError: (String) -> Void = { message in model.menuAccountErrors[id] = message }
        if account.phase == .signInRequired {
            model.signIn(account, onError: onError)
        } else {
            model.act(account.enabled ? "disconnect" : "connect", account, onError: onError)
        }
    }

    @objc private func dismissError() { model.menuAccountErrors[id] = nil }

    @objc private func copyValue(_ sender: NSMenuItem) {
        guard let value = sender.representedObject as? String else { return }
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(value, forType: .string)
    }

    @objc private func showInWindow() { owner.show(accountID: id) }
}
