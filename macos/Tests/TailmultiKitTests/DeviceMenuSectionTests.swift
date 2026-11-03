import XCTest
import AppKit
@testable import TailmultiKit

@MainActor
final class DeviceMenuSectionTests: XCTestCase {
    private final class Target: NSObject { @objc func copy(_ sender: Any?) {} }
    private let target = Target()

    private func section(_ account: String) -> DeviceMenuSection {
        DeviceMenuSection(accountID: account, target: target, copyAction: #selector(Target.copy(_:)), submenuDelegate: nil)
    }

    private func device(_ account: String, _ name: String, _ host: Int, online: Bool = true) -> Device {
        Device(profileId: account, name: "\(name).\(account).ts.net", addresses: ["198.18.\(host / 256).\(host % 256)", "fd74:6169:6c6d::\(String(host, radix: 16))"], online: online)
    }

    /// Two accounts with `count` devices each, a third offline, and identical names across accounts.
    private func fleet(_ count: Int) -> [Device] {
        (0..<count).flatMap { i in
            [device("personal", "sandbox-lab-\(i)", i, online: i % 3 != 0), device("work", "sandbox-lab-\(i)", 10_000 + i)]
        }
    }

    private func visibleTitles(_ section: DeviceMenuSection) -> [String] {
        section.items.filter { !$0.isHidden }.map(\.title)
    }

    func testRowsAndSearchStayWithinOneAccount() {
        let all = [device("personal", "nas", 2), device("work", "nas", 3), device("work", "build", 4)]
        let personal = section("personal")
        personal.update(all)
        XCTAssertEqual(visibleTitles(personal), ["nas"])
        XCTAssertEqual(personal.items.compactMap(\.toolTip), ["nas.personal.ts.net"])
        personal.apply(query: "198.18.0.3")
        XCTAssertEqual(visibleTitles(personal), ["No Matching Devices"])
        personal.apply(query: "build")
        XCTAssertEqual(visibleTitles(personal), ["No Matching Devices"])
    }

    func testTypingOnlyChangesVisibilityNeverItems() {
        let personal = section("personal")
        personal.update(fleet(30))
        let items = personal.items.map(ObjectIdentifier.init)
        let submenus = personal.items.compactMap(\.submenu).map(ObjectIdentifier.init)
        for query in ["s", "sa", "sandbox-lab-1", "sandbox-lab-12", "fd74:6169:6c6d::c", ""] {
            personal.apply(query: query)
            XCTAssertEqual(personal.items.map(ObjectIdentifier.init), items, query)
            XCTAssertEqual(personal.items.compactMap(\.submenu).map(ObjectIdentifier.init), submenus, query)
        }
        personal.apply(query: "sandbox-lab-1")
        XCTAssertEqual(personal.visibleCount, 11)
        personal.apply(query: "fd74:6169:6c6d::c")
        XCTAssertEqual(visibleTitles(personal), ["Offline", "sandbox-lab-12"], "offline header only when an offline row is visible")
        personal.apply(query: "sandbox-lab-1")
        XCTAssertFalse(visibleTitles(personal).contains("No Matching Devices"))
    }

    func testRepeatedQueryIsANoOp() {
        let personal = section("personal")
        personal.update(fleet(5))
        personal.apply(query: "lab-2")
        personal.items.forEach { $0.isHidden.toggle() }
        let tampered = personal.items.map(\.isHidden)
        personal.apply(query: "lab-2")
        XCTAssertEqual(personal.items.map(\.isHidden), tampered, "an unchanged query, as sent on resign, touches nothing")
    }

    func testCopyEntriesUseExactEngineValues() {
        let personal = section("personal")
        personal.update([device("personal", "nas", 2)])
        let copies = personal.items.first?.submenu?.items.dropFirst().map { ($0.title, $0.representedObject as? String) }
        XCTAssertEqual(copies?.map(\.0), ["Copy Hostname", "Copy IPv4: 198.18.0.2", "Copy IPv6: fd74:6169:6c6d::2"])
        XCTAssertEqual(copies?.map(\.1), ["nas.personal.ts.net", "198.18.0.2", "fd74:6169:6c6d::2"])
        XCTAssertEqual(personal.items.first?.submenu?.items.first?.title, "nas.personal.ts.net")
    }

    func testNeedsUpdateTracksEverythingARowShows() {
        let base = [device("personal", "nas", 2), device("work", "nas", 3)]
        let personal = section("personal")
        personal.update(base)
        XCTAssertFalse(personal.needsUpdate(base))
        XCTAssertFalse(personal.needsUpdate([base[0], device("work", "other", 9)]), "other accounts don't matter")
        XCTAssertTrue(personal.needsUpdate([device("personal", "nas", 2, online: false)]))
        XCTAssertTrue(personal.needsUpdate([Device(profileId: "personal", name: "nas.personal.ts.net", addresses: ["198.18.0.2"], online: true)]))
        XCTAssertTrue(personal.needsUpdate([device("personal", "nas-renamed", 2)]))
    }

    func testDuplicateShortNamesFallBackToFullNames() {
        let personal = section("personal")
        personal.update([
            Device(profileId: "personal", name: "nas.a.ts.net", addresses: ["198.18.0.2"], online: true),
            Device(profileId: "personal", name: "nas.b.ts.net", addresses: ["198.18.0.3"], online: true),
        ])
        XCTAssertEqual(visibleTitles(personal), ["nas.a.ts.net", "nas.b.ts.net"])
    }

    /// Measured code timing only: excludes AppKit menu layout, drawing and event delivery.
    func testKeystrokeFilteringStaysWellUnderAFrame() {
        let all = fleet(500)
        let personal = section("personal")
        let clock = ContinuousClock()
        let build = clock.measure { personal.update(all) }
        let typed = Array("sandbox-lab-42")
        var worst = Duration.zero, total = Duration.zero
        for round in 0..<5 {
            for end in 1...typed.count {
                let query = String(typed[..<end])
                let elapsed = clock.measure { personal.apply(query: query) }
                worst = max(worst, elapsed); total += elapsed
            }
            personal.apply(query: round.isMultiple(of: 2) ? "" : "x")
        }
        let average = total / (5 * typed.count)
        print("DeviceMenuSection 500/account x2: build \(build), keystroke avg \(average), worst \(worst)")
        XCTAssertEqual(personal.apply(query: "sandbox-lab-42"), 11, "42 and 420-429, personal only")
        XCTAssertLessThan(worst, .milliseconds(16), "generous bound; typical values are reported above")
    }
}
