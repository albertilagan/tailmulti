import AppKit

/// One account's device rows. Items are built only when the device set changes; search only hides rows,
/// so typing never inserts, removes or rebuilds menu items.
@MainActor
public final class DeviceMenuSection {
    private struct Snapshot: Equatable {
        let id: String, name: String, addresses: [String], online: Bool
    }

    public let accountID: String
    /// Device rows (online first), the Offline header before the first offline row, then the no-match note.
    public private(set) var items: [NSMenuItem] = []
    public private(set) var query = ""
    private var devices: [Device] = []
    private var snapshot: [Snapshot] = []
    private var rows: [(device: Device, item: NSMenuItem)] = []
    private let offlineHeader = NSMenuItem.sectionHeader(title: "Offline")
    private let noMatches = NSMenuItem(title: "No Matching Devices", action: nil, keyEquivalent: "")
    private weak var target: AnyObject?
    private let copyAction: Selector
    private weak var submenuDelegate: NSMenuDelegate?

    public init(accountID: String, target: AnyObject, copyAction: Selector, submenuDelegate: NSMenuDelegate?) {
        self.accountID = accountID
        self.target = target
        self.copyAction = copyAction
        self.submenuDelegate = submenuDelegate
        noMatches.isEnabled = false
    }

    /// True when `all` differs from the shown rows in membership, names, addresses or online state.
    public func needsUpdate(_ all: [Device]) -> Bool {
        Self.makeSnapshot(DeviceMenuCatalog.devices(all, accountID: accountID, query: "")) != snapshot
    }

    /// Rebuilds rows from the engine's device list, keeping only this account's devices.
    public func update(_ all: [Device]) {
        devices = DeviceMenuCatalog.devices(all, accountID: accountID, query: "")
        snapshot = Self.makeSnapshot(devices)
        // Short names repeat only rarely within one account; fall back to the full name then.
        let counts = Dictionary(devices.map { ($0.shortName, 1) }, uniquingKeysWith: +)
        rows = devices.map { device in
            (device, makeItem(device, title: counts[device.shortName, default: 0] > 1 ? device.name : device.shortName))
        }
        var built: [NSMenuItem] = []
        for row in rows {
            if !row.device.online && !built.contains(offlineHeader) { built.append(offlineHeader) }
            built.append(row.item)
        }
        if !built.contains(offlineHeader) { offlineHeader.isHidden = true }
        items = built + [noMatches]
        apply(query: query, force: true)
    }

    /// Shows rows matching `query` by toggling visibility. Returns the number of visible devices.
    @discardableResult
    public func apply(query newQuery: String, force: Bool = false) -> Int {
        guard force || newQuery != query else { return visibleCount }
        query = newQuery
        // Rows are already this account's devices in catalog order, so filtering needs no sort.
        let trimmed = DeviceMenuCatalog.trimmed(newQuery)
        var anyOffline = false, count = 0
        for row in rows {
            let visible = DeviceMenuCatalog.matches(row.device, trimmedQuery: trimmed)
            setHidden(row.item, !visible)
            if visible {
                count += 1
                if !row.device.online { anyOffline = true }
            }
        }
        setHidden(offlineHeader, !anyOffline)
        setHidden(noMatches, count > 0)
        visibleCount = count
        return count
    }

    public private(set) var visibleCount = 0

    /// The address is in the title so several IPv4/IPv6 values stay distinguishable.
    public static func copyTitle(_ item: DeviceCopyItem, device: Device) -> String {
        if item.value == device.name { return item.title }
        return item.title.replacingOccurrences(of: " Address", with: "") + ": " + item.value
    }

    private func setHidden(_ item: NSMenuItem, _ hidden: Bool) {
        if item.isHidden != hidden { item.isHidden = hidden }
    }

    private func makeItem(_ device: Device, title: String) -> NSMenuItem {
        let item = NSMenuItem(title: title, action: nil, keyEquivalent: "")
        item.toolTip = device.name
        item.setAccessibilityLabel("\(device.name), \(device.online ? "online" : "offline")")
        let submenu = NSMenu()
        submenu.autoenablesItems = false
        submenu.delegate = submenuDelegate
        submenu.addItem(.sectionHeader(title: device.name))
        let copies = device.copyItems
        if copies.isEmpty {
            let none = NSMenuItem(title: "No Addresses Available", action: nil, keyEquivalent: "")
            none.isEnabled = false
            submenu.addItem(none)
        }
        for copy in copies {
            let entry = NSMenuItem(title: Self.copyTitle(copy, device: device), action: copyAction, keyEquivalent: "")
            entry.target = target
            entry.representedObject = copy.value
            submenu.addItem(entry)
        }
        item.submenu = submenu
        return item
    }

    private static func makeSnapshot(_ devices: [Device]) -> [Snapshot] {
        devices.map { Snapshot(id: $0.id, name: $0.name, addresses: $0.addresses, online: $0.online) }
    }
}
