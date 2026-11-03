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

    /// Name or any engine address contains the query, ignoring case. Does not check the account.
    public static func matches(_ device: Device, trimmedQuery query: String) -> Bool {
        query.isEmpty || device.name.localizedCaseInsensitiveContains(query) ||
            device.addresses.contains { $0.localizedCaseInsensitiveContains(query) }
    }
}

public struct DeviceCopyItem: Identifiable, Equatable, Sendable {
    public var id: String { title + "/" + value }
    public let title: String
    public let value: String
}

extension Device {
    public var shortName: String { name.split(separator: ".").first.map(String.init) ?? name }

    /// Copy the full DNS name and the local addresses supplied by the authenticated engine.
    /// Never derive a canonical Tailscale address or an address from another account.
    public var copyItems: [DeviceCopyItem] {
        var items: [DeviceCopyItem] = []
        if !name.isEmpty { items.append(DeviceCopyItem(title: "Copy Hostname", value: name)) }
        var seen = Set<String>()
        var ipv4: [DeviceCopyItem] = [], ipv6: [DeviceCopyItem] = []
        for address in addresses where seen.insert(address).inserted {
            var v4 = in_addr(), v6 = in6_addr()
            if inet_pton(AF_INET, address, &v4) == 1 {
                ipv4.append(DeviceCopyItem(title: "Copy IPv4 Address", value: address))
            } else if inet_pton(AF_INET6, address, &v6) == 1 {
                ipv6.append(DeviceCopyItem(title: "Copy IPv6 Address", value: address))
            }
        }
        return items + ipv4 + ipv6
    }
}
