import Foundation
import Darwin

public struct Account: Decodable, Identifiable, Sendable {
    public let id: String
    public let name: String
    public let enabled: Bool
    public let state: String
    public let peerCount: Int
    /// Optional for compatibility with an older running helper. These are UI hints, not credentials.
    public let hasIdentity: Bool?
    public let loginReady: Bool?

    public init(id: String, name: String, enabled: Bool, state: String, peerCount: Int,
                hasIdentity: Bool? = nil, loginReady: Bool? = nil) {
        self.id = id; self.name = name; self.enabled = enabled; self.state = state
        self.peerCount = peerCount; self.hasIdentity = hasIdentity; self.loginReady = loginReady
    }
}
public struct Device: Decodable, Identifiable, Sendable {
    public var id: String { profileId + "/" + name + "/" + addresses.joined(separator: ",") }
    public let profileId: String
    public let name: String
    public let addresses: [String]
    public let online: Bool
}
public struct EngineStatus: Decodable, Sendable {
    public let version: Int
    public let mode: String
    public let accounts: [Account]
    public let devices: [Device]
    public let conflicts: [String]
    public let health: String
}
public struct ControlError: Error, LocalizedError, Sendable {
    public enum Kind: Sendable { case unavailable, identityMismatch, other }
    public let message: String
    public let kind: Kind
    public var errorDescription: String? { message }
    public init(_ message: String, kind: Kind = .other) { self.message = message; self.kind = kind }
}
/// A Unix socket transport with kernel peer-UID verification in both directions.
/// No shell, local TCP listener, shared bearer token or account key access.
public struct ControlClient: Sendable {
    public let path: String
    public let serverUID: uid_t
    public static let servicePath = "/Library/Application Support/Tailmulti/run/control.sock"
    public init(path: String = ControlClient.servicePath, serverUID: uid_t = 0) {
        self.path = path; self.serverUID = serverUID
    }
    public func status() async throws -> EngineStatus {
        let data = try await request("GET", "/v1/status")
        let status = try JSONDecoder().decode(EngineStatus.self, from: data)
        guard status.version == 1 else { throw ControlError("The app and engine use different protocol versions.") }
        return status
    }
    @discardableResult public func add(_ name: String) async throws -> Account {
        let body = try JSONSerialization.data(withJSONObject: ["name": name])
        let data = try await request("POST", "/v1/accounts", body: body)
        return try JSONDecoder().decode(Account.self, from: data)
    }
    public func action(_ action: String, id: String) async throws {
        guard ["connect", "disconnect", "remove"].contains(action), Self.validID(id) else { throw ControlError("Invalid account action.") }
        _ = try await request("POST", "/v1/accounts/\(id)/\(action)", body: Data("{}".utf8))
    }
    public func login(id: String) async throws -> URL {
        guard Self.validID(id) else { throw ControlError("Invalid account.") }
        let data = try await request("POST", "/v1/accounts/\(id)/login", body: Data("{}".utf8))
        struct Reply: Decodable { let url: String }
        let reply = try JSONDecoder().decode(Reply.self, from: data)
        return try Self.loginURL(reply.url)
    }
    public static func loginURL(_ string: String) throws -> URL {
        guard let url = URL(string: string), url.scheme == "https", url.host == "login.tailscale.com",
              url.user == nil, url.password == nil, url.port == nil else {
            throw ControlError("The engine returned an unsupported login address.")
        }
        return url
    }
    public func setup() async throws -> SetupReport {
        try SetupReport.decode(try await request("GET", "/v1/setup"))
    }
    public func startNetwork() async throws {
        _ = try await request("POST", "/v1/network/start", body: Data("{}".utf8))
    }
    public func stopNetwork(restoreDNS: Bool) async throws {
        let body = try JSONSerialization.data(withJSONObject: ["restoreDNS": restoreDNS])
        _ = try await request("POST", "/v1/network/stop", body: body)
    }
    /// The running service's build number (the app's CFBundleVersion it was bundled with).
    public func serviceBuild() async throws -> String {
        struct Reply: Decodable { let build: String }
        return try JSONDecoder().decode(Reply.self, from: try await request("GET", "/v1/service")).build
    }
    /// Asks the service to exit so launchd starts the bundled executable again, for example after an update.
    public func restartService() async throws {
        _ = try await request("POST", "/v1/service/restart", body: Data("{}".utf8))
    }
    public func diagnostics() async throws -> String {
        let data = try await request("GET", "/v1/diagnostics")
        let object = try JSONSerialization.jsonObject(with: data)
        return String(decoding: try JSONSerialization.data(withJSONObject: object, options: [.prettyPrinted, .sortedKeys]), as: UTF8.self)
    }
    static func validID(_ id: String) -> Bool {
        id.count == 32 && id.allSatisfy { "0123456789abcdef".contains($0) }
    }
    func request(_ method: String, _ endpoint: String, body: Data = Data()) async throws -> Data {
        try await Task.detached { try self.exchange(method, endpoint, body: body) }.value
    }
    func exchange(_ method: String, _ endpoint: String, body: Data) throws -> Data {
        var address = sockaddr_un()
        address.sun_family = sa_family_t(AF_UNIX)
        let pathBytes = Array(path.utf8) + [UInt8(0)]
        guard pathBytes.count <= MemoryLayout.size(ofValue: address.sun_path) else { throw ControlError("Control socket path is too long.") }
        withUnsafeMutableBytes(of: &address.sun_path) { bytes in bytes.copyBytes(from: pathBytes) }
        address.sun_len = UInt8(MemoryLayout<sockaddr_un>.size)
        let fd = Darwin.socket(AF_UNIX, SOCK_STREAM, 0)
        guard fd >= 0 else { throw ControlError("Cannot create a control connection.") }
        defer { Darwin.close(fd) }
        var noSignal: Int32 = 1
        _ = setsockopt(fd, SOL_SOCKET, SO_NOSIGPIPE, &noSignal, socklen_t(MemoryLayout.size(ofValue: noSignal)))
        let networkChange = endpoint == "/v1/network/start" || endpoint == "/v1/network/stop"
        var timeout = timeval(tv_sec: networkChange ? 110 : 15, tv_usec: 0)
        _ = setsockopt(fd, SOL_SOCKET, SO_RCVTIMEO, &timeout, socklen_t(MemoryLayout.size(ofValue: timeout)))
        _ = setsockopt(fd, SOL_SOCKET, SO_SNDTIMEO, &timeout, socklen_t(MemoryLayout.size(ofValue: timeout)))
        let connected = withUnsafePointer(to: &address) { pointer in
            pointer.withMemoryRebound(to: sockaddr.self, capacity: 1) { Darwin.connect(fd, $0, socklen_t(MemoryLayout<sockaddr_un>.size)) }
        }
        guard connected == 0 else {
            let missing = errno == ENOENT || errno == ECONNREFUSED
            throw ControlError("Tailmulti engine is unavailable.", kind: missing ? .unavailable : .other)
        }
        var uid: uid_t = 0; var gid: gid_t = 0
        guard getpeereid(fd, &uid, &gid) == 0, uid == serverUID else { throw ControlError("Control server identity does not match.", kind: .identityMismatch) }
        var request = Data("\(method) \(endpoint) HTTP/1.1\r\nHost: tailmulti\r\nContent-Type: application/json\r\nContent-Length: \(body.count)\r\nConnection: close\r\n\r\n".utf8)
        request.append(body)
        try request.withUnsafeBytes { bytes in
            var sent = 0
            while sent < bytes.count {
                let n = Darwin.write(fd, bytes.baseAddress!.advanced(by: sent), bytes.count - sent)
                if n < 0 && errno == EINTR { continue }
                guard n > 0 else { throw ControlError("Control request could not be sent.") }; sent += n
            }
        }
        var response = Data(); var buffer = [UInt8](repeating: 0, count: 8192)
        while true {
            let n = Darwin.read(fd, &buffer, buffer.count)
            if n < 0 && errno == EINTR { continue }
            guard n >= 0 else { throw ControlError("Control request timed out or failed.") }
            if n == 0 { break }
            response.append(contentsOf: buffer.prefix(n))
            guard response.count <= 4 * 1024 * 1024 else { throw ControlError("Engine response exceeded the size limit.") }
        }
        return try Self.decodeHTTP(response)
    }
    public static func decodeHTTP(_ response: Data) throws -> Data {
        guard let separator = response.range(of: Data("\r\n\r\n".utf8)),
              let header = String(data: response[..<separator.lowerBound], encoding: .utf8) else { throw ControlError("Malformed engine response.") }
        let lines = header.components(separatedBy: "\r\n")
        guard let first = lines.first, let code = Int(first.split(separator: " ").dropFirst().first ?? "") else { throw ControlError("Malformed engine status.") }
        let body = Data(response[separator.upperBound...])
        guard let lengthLine = lines.first(where: { $0.lowercased().hasPrefix("content-length:") }),
              let length = Int(lengthLine.dropFirst("content-length:".count).trimmingCharacters(in: .whitespaces)), length == body.count else { throw ControlError("Incomplete engine response.") }
        guard code == 200 else {
            struct Failure: Decodable { let error: String }
            throw ControlError((try? JSONDecoder().decode(Failure.self, from: body).error) ?? "Engine request failed.")
        }
        return body
    }
}

/// User-facing connection phase derived from the engine's account state string.
public enum ConnectionPhase: Equatable, Sendable {
    case off, connecting, connected, signInRequired, awaitingApproval, setupRequired, unavailable
    case other(String)

    public init(state: String, enabled: Bool) {
        switch state.lowercased() {
        case "running": self = .connected
        case "needslogin": self = .signInRequired
        case "needsmachineauth": self = .awaitingApproval
        case "starting", "nostate": self = enabled ? .connecting : .off
        case "stopped", "disconnected": self = enabled ? .connecting : .off
        case "activation required": self = .setupRequired
        case "unavailable", "inuse": self = .unavailable
        default: self = .other(state)
        }
    }

    public var label: String {
        switch self {
        case .off: "Not connected"
        case .connecting: "Connecting"
        case .connected: "Connected"
        case .signInRequired: "Sign-in required"
        case .awaitingApproval: "Waiting for admin approval"
        case .setupRequired: "Needs network setup"
        case .unavailable: "Unavailable"
        case .other(let state): state.prefix(1).uppercased() + state.dropFirst()
        }
    }

    public var needsAttention: Bool {
        switch self {
        case .signInRequired, .awaitingApproval, .unavailable: true
        default: false
        }
    }
}

extension Account {
    public var phase: ConnectionPhase {
        let phase = ConnectionPhase(state: state, enabled: enabled)
        return phase == .off && hasIdentity == false ? .signInRequired : phase
    }
}
