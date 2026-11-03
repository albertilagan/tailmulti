import Foundation

/// Defers menu mutations out of AppKit callbacks, which enumerate items (mutating there throws); stale open-only work is dropped.
@MainActor
public final class MenuUpdateQueue {
    public typealias Scheduler = (@escaping @MainActor () -> Void) -> Void

    private struct Entry {
        let generation: Int
        let requiresOpen: Bool
        let work: @MainActor () -> Void
    }

    private let schedule: Scheduler
    private var pending: [String: Entry] = [:]
    private var order: [String] = []
    private var flushQueued = false
    public private(set) var isOpen = false
    /// Changes on every open and close, so work from an earlier showing can't touch a later one.
    public private(set) var generation = 0

    public init(schedule: Scheduler? = nil) {
        self.schedule = schedule ?? { work in Task { @MainActor in work() } }
    }

    public var hasPendingWork: Bool { !pending.isEmpty }

    public func opened() {
        isOpen = true
        generation += 1
    }

    public func closed() {
        isOpen = false
        generation += 1
    }

    /// Queues `work` under `key`, replacing any earlier pending work with the same key.
    public func enqueue(_ key: String, requiresOpen: Bool = true, _ work: @escaping @MainActor () -> Void) {
        if pending[key] == nil { order.append(key) }
        pending[key] = Entry(generation: generation, requiresOpen: requiresOpen, work: work)
        guard !flushQueued else { return }
        flushQueued = true
        schedule { [weak self] in self?.flush() }
    }

    /// Runs the work pending when the flush began; work queued while it runs waits for the next turn.
    private func flush() {
        flushQueued = false
        let keys = order, entries = pending
        order = []
        pending = [:]
        for key in keys {
            guard let entry = entries[key] else { continue }
            if entry.requiresOpen && (!isOpen || entry.generation != generation) { continue }
            entry.work()
        }
    }
}
