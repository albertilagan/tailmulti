import XCTest
import AppKit
@testable import TailmultiKit

@MainActor
final class MenuUpdateQueueTests: XCTestCase {
    /// A scheduler the test drains by hand, standing in for the next main-actor turn.
    @MainActor private final class ManualScheduler {
        var queued: [@MainActor () -> Void] = []
        func run() {
            let batch = queued
            queued = []
            batch.forEach { $0() }
        }
    }

    private func makeQueue() -> (MenuUpdateQueue, ManualScheduler) {
        let scheduler = ManualScheduler()
        return (MenuUpdateQueue(schedule: { scheduler.queued.append($0) }), scheduler)
    }

    func testWorkNeverRunsInsideTheCallbackThatQueuedIt() {
        let (queue, scheduler) = makeQueue()
        queue.opened()
        var ran = false
        queue.enqueue("search") { ran = true }
        XCTAssertFalse(ran)
        scheduler.run()
        XCTAssertTrue(ran)
    }

    /// Mirrors AppKit enumerating a menu's items while it sends close notifications and resigns
    /// the search field. A synchronous mutation here would raise an Objective-C exception.
    func testCloseCallbackDuringEnumerationDoesNotMutateMenu() {
        let (queue, scheduler) = makeQueue()
        let menu = NSMenu()
        ["search", "alpha", "beta"].forEach { menu.addItem(withTitle: $0, action: nil, keyEquivalent: "") }
        queue.opened()
        let items = NSMutableArray(array: menu.items)
        for case let item as NSMenuItem in items {
            queue.enqueue("devices") {
                menu.removeItem(at: menu.numberOfItems - 1)
                items.removeLastObject()
            }
            XCTAssertEqual(items.count, 3, "mutated during enumeration of \(item.title)")
        }
        scheduler.run()
        XCTAssertEqual(menu.numberOfItems, 2, "coalesced work runs once after enumeration")
    }

    func testRepeatedRequestsCoalesceToTheLatest() {
        let (queue, scheduler) = makeQueue()
        queue.opened()
        var applied: [String] = []
        for query in ["s", "se", "ser"] { queue.enqueue("search") { applied.append(query) } }
        queue.enqueue("focus") { applied.append("focus") }
        XCTAssertEqual(scheduler.queued.count, 1)
        scheduler.run()
        XCTAssertEqual(applied, ["ser", "focus"])
    }

    func testOpenOnlyWorkIsDroppedAfterClose() {
        let (queue, scheduler) = makeQueue()
        queue.opened()
        var ran = false
        queue.enqueue("focus") { ran = true }
        queue.closed()
        scheduler.run()
        XCTAssertFalse(ran)
        XCTAssertFalse(queue.hasPendingWork)
    }

    func testWorkFromAnEarlierShowingIsDroppedAfterReopen() {
        let (queue, scheduler) = makeQueue()
        queue.opened()
        var ran = false
        queue.enqueue("devices") { ran = true }
        queue.closed()
        queue.opened()
        scheduler.run()
        XCTAssertFalse(ran)
    }

    func testCleanupAfterCloseStillRuns() {
        let (queue, scheduler) = makeQueue()
        queue.opened()
        queue.closed()
        var reset = false
        queue.enqueue("reset", requiresOpen: false) { reset = true }
        scheduler.run()
        XCTAssertTrue(reset)
    }

    func testWorkQueuedDuringFlushWaitsForNextTurn() {
        let (queue, scheduler) = makeQueue()
        queue.opened()
        var order: [String] = []
        queue.enqueue("first") {
            order.append("first")
            queue.enqueue("second") { order.append("second") }
        }
        scheduler.run()
        XCTAssertEqual(order, ["first"])
        scheduler.run()
        XCTAssertEqual(order, ["first", "second"])
    }

    func testDefaultSchedulerDefersToALaterMainActorTurn() async {
        let queue = MenuUpdateQueue()
        queue.opened()
        var ran = false
        queue.enqueue("search") { ran = true }
        XCTAssertFalse(ran)
        for _ in 0..<20 where !ran { await Task.yield() }
        XCTAssertTrue(ran)
    }
}
