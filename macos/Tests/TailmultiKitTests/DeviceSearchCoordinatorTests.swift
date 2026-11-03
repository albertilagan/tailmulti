import XCTest
import AppKit
@testable import TailmultiKit

@MainActor
final class DeviceSearchCoordinatorTests: XCTestCase {
    private final class Target: NSObject { @objc func copy(_ sender: Any?) {} }
    private let target = Target()
    private var queued: [@MainActor () -> Void] = []
    private var query = ""

    /// Drains one main-actor turn's worth of queued work.
    private func nextTurn() {
        let batch = queued
        queued = []
        batch.forEach { $0() }
    }

    private func makeCoordinator() -> DeviceSearchCoordinator {
        let section = DeviceMenuSection(accountID: "personal", target: target, copyAction: #selector(Target.copy(_:)), submenuDelegate: nil)
        section.update((0..<30).flatMap { i in [
            Device(profileId: "personal", name: "lab-\(i).personal.ts.net", addresses: ["198.18.0.\(i)"], online: i % 4 != 0),
            Device(profileId: "work", name: "lab-\(i).work.ts.net", addresses: ["198.18.1.\(i)"], online: true),
        ] })
        let coordinator = DeviceSearchCoordinator(section: section, schedule: { [unowned self] in queued.append($0) })
        coordinator.currentQuery = { [unowned self] in query }
        coordinator.refresh = { [unowned coordinator] in coordinator.applyQuery() }
        return coordinator
    }

    private func hidden(_ c: DeviceSearchCoordinator) -> [Bool] { c.section.items.map(\.isHidden) }

    func testKeystrokeAppliesOnTheNextTurnNotInsideTheAction() {
        let c = makeCoordinator()
        c.parentWillOpen {}
        nextTurn()
        let before = hidden(c)
        query = "lab-1"
        c.textChanged()
        XCTAssertEqual(hidden(c), before, "the field action must not change items synchronously")
        nextTurn()
        XCTAssertEqual(c.section.query, "lab-1")
        XCTAssertEqual(c.section.visibleCount, 11)
    }

    /// Held query, copy submenu closes, the field resigns and sends its action, then the parent closes:
    /// nothing may change items synchronously, and nothing applies once the parent has closed.
    func testHeldQueryDuringChildThenParentDismissalNeverMutatesSynchronously() {
        let c = makeCoordinator()
        c.parentWillOpen {}
        nextTurn()
        c.childWillOpen()
        query = "lab-2"
        c.textChanged()
        nextTurn()
        XCTAssertTrue(c.stale)
        XCTAssertEqual(c.section.query, "", "held while a copy submenu is open")
        let before = hidden(c)

        c.childDidClose()
        XCTAssertEqual(c.openChildren, 0)
        c.textChanged()
        XCTAssertEqual(hidden(c), before)
        c.parentDidClose()
        XCTAssertEqual(hidden(c), before)
        nextTurn()
        XCTAssertEqual(hidden(c), before, "work queued while open is dropped after the parent closes")
        XCTAssertEqual(c.section.query, "")

        c.reset()
        XCTAssertFalse(c.stale)
    }

    func testHeldQueryAppliesOnTheTurnAfterTheChildCloses() {
        let c = makeCoordinator()
        c.parentWillOpen {}
        c.childWillOpen()
        query = "198.18.0.2"
        c.textChanged()
        nextTurn()
        let before = hidden(c)
        c.childDidClose()
        c.textChanged()
        XCTAssertEqual(hidden(c), before)
        nextTurn()
        XCTAssertEqual(c.section.query, "198.18.0.2")
        XCTAssertEqual(c.section.visibleCount, 11, "personal 2 and 20-29 only; work addresses never match")
        XCTAssertFalse(c.stale)
    }

    func testReopenDropsWorkFromThePreviousShowing() {
        let c = makeCoordinator()
        c.parentWillOpen {}
        query = "lab-3"
        c.textChanged()
        c.parentDidClose()
        c.parentWillOpen {}
        nextTurn()
        XCTAssertEqual(c.section.query, "")
    }

    func testResetWaitsUntilNothingIsTracking() {
        let c = makeCoordinator()
        c.parentWillOpen {}
        query = "lab-5"
        c.textChanged()
        nextTurn()
        c.reset()
        XCTAssertEqual(c.section.query, "lab-5", "no reset while open")
        c.parentDidClose()
        c.reset()
        XCTAssertEqual(c.section.query, "")
    }
}
