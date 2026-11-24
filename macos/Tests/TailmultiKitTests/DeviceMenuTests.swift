import XCTest
@testable import TailmultiKit

final class DeviceMenuTests: XCTestCase {
    func testSearchCannotCrossAccountsWithIdenticalNamesOrOriginalIPs() {
        let personal = Device(profileId: "personal", name: "server.personal.ts.net", addresses: ["198.18.0.2"], tailscaleAddresses: ["100.64.0.2"], online: true)
        let work = Device(profileId: "work", name: "server.personal.ts.net", addresses: ["198.18.0.3"], tailscaleAddresses: ["100.64.0.2", "100.64.0.9"], online: true)
        let all = [work, personal]
        XCTAssertEqual(DeviceMenuCatalog.devices(all, accountID: "personal", query: " SERVER ").map(\.id), [personal.id])
        XCTAssertEqual(DeviceMenuCatalog.devices(all, accountID: "personal", query: "100.64.0.2").map(\.id), [personal.id])
        XCTAssertTrue(DeviceMenuCatalog.devices(all, accountID: "personal", query: "198.18.0.3").isEmpty)
        XCTAssertTrue(DeviceMenuCatalog.devices(all, accountID: "personal", query: "100.64.0.9").isEmpty)
        XCTAssertTrue(DeviceMenuCatalog.devices(all, accountID: "removed", query: "").isEmpty)
        XCTAssertEqual(DeviceMenuCatalog.devices(all, accountID: "work", query: "198.18.0.3").map(\.id), [work.id])
        XCTAssertEqual(DeviceMenuCatalog.devices(all, accountID: "work", query: "100.64.0.2").map(\.id), [work.id])
    }

    func testMenuSortsOnlineFirstAndSearchesBothAddressFamilies() {
        let offline = Device(profileId: "a", name: "alpha.tail.ts.net", addresses: ["198.18.0.2"], tailscaleAddresses: ["100.64.0.2"], online: false)
        let node10 = Device(profileId: "a", name: "node10.tail.ts.net", addresses: ["198.18.0.10"], tailscaleAddresses: ["100.64.0.10"], online: true)
        let node2 = Device(profileId: "a", name: "node2.tail.ts.net", addresses: ["198.18.0.3", "fd74:6169:6c6d::3"], tailscaleAddresses: ["100.64.0.3", "fd7a:115c:a1e0::3"], online: true)
        let all = [offline, node10, node2]
        XCTAssertEqual(DeviceMenuCatalog.devices(all, accountID: "a", query: "").map(\.id), [node2.id, node10.id, offline.id])
        XCTAssertEqual(DeviceMenuCatalog.devices(all, accountID: "a", query: "FD74:6169:6C6D::3").map(\.id), [node2.id])
        XCTAssertEqual(DeviceMenuCatalog.devices(all, accountID: "a", query: "FD7A:115C:A1E0::3").map(\.id), [node2.id])
    }

    func testCopyKeepsFullHostnameAndExactEngineAddresses() {
        let device = Device(
            profileId: "a",
            name: "server.personal.ts.net.",
            addresses: ["fd74:6169:6c6d::2", "198.18.0.2"],
            tailscaleAddresses: ["fd7a:115c:a1e0::2", "100.64.0.2"],
            online: true
        )
        XCTAssertEqual(device.shortName, "server")
        XCTAssertEqual(device.primaryAddress, "198.18.0.2")
        XCTAssertEqual(device.primaryTailscaleAddress, "100.64.0.2")
        XCTAssertEqual(device.copyItems.map(\.title), [
            "Copy Hostname",
            "Copy Local IPv4 Address",
            "Copy Local IPv6 Address",
            "Copy Tailscale IPv4 Address",
            "Copy Tailscale IPv6 Address",
        ])
        XCTAssertEqual(device.copyItems.map(\.value), [
            "server.personal.ts.net.",
            "198.18.0.2",
            "fd74:6169:6c6d::2",
            "100.64.0.2",
            "fd7a:115c:a1e0::2",
        ])
        XCTAssertEqual(device.copyItems.map(\.group), [.identity, .identity, .identity, .tailscale, .tailscale])
        XCTAssertEqual(Set(device.copyItems.map(\.id)).count, 5)
    }

    func testCopyDoesNotInventMissingAddressesOrOfferEmptyValues() {
        let device = Device(
            profileId: "a",
            name: "ipv6.tail.ts.net",
            addresses: ["", "invalid", "198.18.0.999", "fd74:6169:6c6d::2", "fd74:6169:6c6d::2"],
            tailscaleAddresses: ["", "not-an-ip", "fd7a:115c:a1e0::2", "fd7a:115c:a1e0::2"],
            online: false
        )
        XCTAssertEqual(device.copyItems.map(\.value), ["ipv6.tail.ts.net", "fd74:6169:6c6d::2", "fd7a:115c:a1e0::2"])
        XCTAssertEqual(device.primaryAddress, "fd74:6169:6c6d::2")
        XCTAssertEqual(device.primaryTailscaleAddress, "fd7a:115c:a1e0::2")
        XCTAssertEqual(Device(profileId: "a", name: "", addresses: [], tailscaleAddresses: ["", "invalid"], online: false).primaryTailscaleAddress, "")
        XCTAssertTrue(Device(profileId: "a", name: "", addresses: [], online: false).copyItems.isEmpty)
    }
}
