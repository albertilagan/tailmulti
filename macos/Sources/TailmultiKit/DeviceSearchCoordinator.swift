import Foundation

/// Menu lifecycle for one account's device submenu: open state, open copy submenus and search.
/// Every visibility or structural change runs on a later main-actor turn, never inside an AppKit callback.
@MainActor
public final class DeviceSearchCoordinator {
    public let section: DeviceMenuSection
    private let updates: MenuUpdateQueue
    public private(set) var isOpen = false
    public private(set) var openChildren = 0
    /// A change arrived while a copy submenu was open and waits for it to close.
    public private(set) var stale = false
    /// Reads the search field's current text.
    public var currentQuery: () -> String = { "" }
    /// Re-checks devices and the pending query; runs only on a later turn while the parent is open.
    public var refresh: () -> Void = {}

    public init(section: DeviceMenuSection, schedule: MenuUpdateQueue.Scheduler? = nil) {
        self.section = section
        updates = MenuUpdateQueue(schedule: schedule)
    }

    /// Items may change only while the parent is open with no copy submenu over it.
    public var canMutate: Bool { isOpen && openChildren == 0 }
    public var isTracking: Bool { isOpen || openChildren > 0 }

    public func parentWillOpen(focus: @escaping @MainActor () -> Void) {
        isOpen = true
        stale = false
        updates.opened()
        updates.enqueue("focus", focus)
    }

    public func parentDidClose() {
        isOpen = false
        openChildren = 0
        updates.closed()
    }

    public func childWillOpen() { openChildren += 1 }

    public func childDidClose() {
        openChildren = max(0, openChildren - 1)
        if stale { updates.enqueue("refresh") { [weak self] in self?.refresh() } }
    }

    /// The search field's action. It is also sent when the field resigns during dismissal, so it only queues.
    public func textChanged() {
        updates.enqueue("search") { [weak self] in self?.applyQuery() }
    }

    /// Applies the field's text if it differs; held as stale while a copy submenu is open.
    @discardableResult
    public func applyQuery() -> Bool {
        let query = currentQuery()
        guard isOpen, query != section.query else { return false }
        guard openChildren == 0 else { stale = true; return false }
        stale = false
        section.apply(query: query)
        return true
    }

    public func markStale() { stale = true }
    public func clearStale() { stale = false }

    /// Clears the search after the menu has fully closed.
    public func reset() {
        guard !isTracking else { return }
        stale = false
        section.apply(query: "")
    }
}
