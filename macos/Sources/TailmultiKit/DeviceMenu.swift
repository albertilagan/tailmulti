import Foundation
import Darwin

/// A menu always queries one account. Names and addresses never select an account implicitly.
public enum DeviceMenuCatalog {
    public static func devices(_ all: [Device], accountID: String, query: String) -> [Device] {
        let query = trimmed(query)
        return all.filter { device in
            device.profileId == accountID && matches(device, trimmedQuery: query)
        }.sorted { lhs, rhs in
            if lhs.online != rhs.online { return lhs.online }
            let order = lhs.name.localizedStandardCompare(rhs.name)
            return order == .orderedSame ? lhs.id < rhs.id : order == .orderedAscending
        }
    }
}

extension DeviceMenuCatalog {
    public static func trimmed(_ query: String) -> String { query.trimmingCharacters(in: .whitespacesAndNewlines) }

    /// Name or any engine address (local or Tailscale) contains the query, ignoring case. Does not check the account.
    public static func matches(_ device: Device, trimmedQuery query: String) -> Bool {
        query.isEmpty || device.name.localizedCaseInsensitiveContains(query) ||
            device.addresses.contains { $0.localizedCaseInsensitiveContains(query) } ||
            device.tailscaleAddresses.contains { $0.localizedCaseInsensitiveContains(query) }
    }
}

public struct DeviceCopyItem: Identifiable, Equatable, Sendable {
    public enum Group: Equatable, Sendable {
        case identity
        case tailscale
    }
    public var id: String { title + "/" + value }
    public let title: String
    public let value: String
    public let group: Group

    public init(title: String, value: String, group: Group = .identity) {
        self.title = title
        self.value = value
        self.group = group
    }
}

extension Device {
    public var shortName: String { name.split(separator: ".").first.map(String.init) ?? name }

    public var primaryAddress: String { Self.primaryIP(in: addresses) }

    public var primaryTailscaleAddress: String { Self.primaryIP(in: tailscaleAddresses) }

    /// Copy the full DNS name, local addresses, and original Tailscale addresses supplied by the
    /// authenticated engine. Never derive an address locally or from another account.
    public var copyItems: [DeviceCopyItem] {
        var items: [DeviceCopyItem] = []
        if !name.isEmpty { items.append(DeviceCopyItem(title: "Copy Hostname", value: name, group: .identity)) }
        items += Self.addressCopyItems(addresses, prefix: "Copy Local", group: .identity)
        items += Self.addressCopyItems(tailscaleAddresses, prefix: "Copy Tailscale", group: .tailscale)
        return items
    }

    private static func primaryIP(in list: [String]) -> String {
        var firstV6: String?
        for address in list {
            var v4 = in_addr(), v6 = in6_addr()
            if inet_pton(AF_INET, address, &v4) == 1 { return address }
            if firstV6 == nil, inet_pton(AF_INET6, address, &v6) == 1 { firstV6 = address }
        }
        return firstV6 ?? ""
    }

    private static func addressCopyItems(_ list: [String], prefix: String, group: DeviceCopyItem.Group) -> [DeviceCopyItem] {
        var seen = Set<String>()
        var ipv4: [DeviceCopyItem] = [], ipv6: [DeviceCopyItem] = []
        for address in list where seen.insert(address).inserted {
            var v4 = in_addr(), v6 = in6_addr()
            if inet_pton(AF_INET, address, &v4) == 1 {
                ipv4.append(DeviceCopyItem(title: "\(prefix) IPv4 Address", value: address, group: group))
            } else if inet_pton(AF_INET6, address, &v6) == 1 {
                ipv6.append(DeviceCopyItem(title: "\(prefix) IPv6 Address", value: address, group: group))
            }
        }
        return ipv4 + ipv6
    }
}
