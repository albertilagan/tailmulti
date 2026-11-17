import Foundation

/// Links the bundled `tailmulti` command into a directory on the default PATH.
/// Only a symlink is created; the command itself stays inside the signed app bundle.
public struct CommandLineTool: Sendable {
    public enum State: Equatable, Sendable {
        /// This copy of the app has no bundled command.
        case unavailable
        case notInstalled
        case installed
        /// A link exists but points at another copy of the app, or at one that is gone.
        case linkedElsewhere
        /// Something that is not a symlink occupies the path; it is never replaced.
        case blocked
    }
    public enum Operation: Sendable { case install, remove }
    /// Runs the operation with administrator rights. Returns false when the user cancels.
    public typealias Elevation = @Sendable (_ operation: Operation, _ tool: URL, _ link: URL) throws -> Bool

    public static let bundlePath = "Contents/Helpers/tailmulti"
    public static let defaultLink = URL(fileURLWithPath: "/usr/local/bin/tailmulti")

    public let tool: URL
    public let link: URL
    private let elevate: Elevation

    public init(bundle: URL, link: URL = CommandLineTool.defaultLink, elevate: @escaping Elevation = CommandLineTool.administratorPrompt) {
        self.tool = bundle.appendingPathComponent(Self.bundlePath); self.link = link; self.elevate = elevate
    }

    public var state: State {
        guard ServiceBundle.isRegularFile(tool) else { return .unavailable }
        guard let attributes = try? FileManager.default.attributesOfItem(atPath: link.path) else { return .notInstalled }
        guard attributes[.type] as? FileAttributeType == .typeSymbolicLink else { return .blocked }
        return (try? FileManager.default.destinationOfSymbolicLink(atPath: link.path)) == tool.path ? .installed : .linkedElsewhere
    }

    /// Returns false when the user cancels the administrator prompt.
    @discardableResult public func install() throws -> Bool {
        switch state {
        case .unavailable: throw ControlError("This copy of Tailmulti doesn't include the command line tool.")
        case .blocked: throw ControlError("\(link.path) already exists and wasn't created by Tailmulti. Move it aside first.")
        case .installed: return true
        case .notInstalled, .linkedElsewhere: break
        }
        let files = FileManager.default
        // Directories the user owns, such as a Homebrew prefix, need no administrator prompt.
        if files.isWritableFile(atPath: link.deletingLastPathComponent().path) {
            // Rename over the old link so a failure never leaves the command missing.
            let staged = link.path + ".\(ProcessInfo.processInfo.processIdentifier).tmp"
            if (try? files.createSymbolicLink(atPath: staged, withDestinationPath: tool.path)) != nil {
                if rename(staged, link.path) == 0 { return true }
                try? files.removeItem(atPath: staged)
            }
        }
        guard try elevate(.install, tool, link) else { return false }
        guard state == .installed else { throw ControlError("The command line tool couldn't be installed.") }
        return true
    }

    /// Removes only a symlink; a link to another copy of the app counts as Tailmulti's.
    @discardableResult public func remove() throws -> Bool {
        switch state {
        case .blocked: throw ControlError("\(link.path) wasn't created by Tailmulti, so it was left alone.")
        case .notInstalled: return true
        case .unavailable where (try? FileManager.default.destinationOfSymbolicLink(atPath: link.path)) == nil: return true
        default: break
        }
        if (try? FileManager.default.removeItem(at: link)) != nil { return true }
        guard try elevate(.remove, tool, link) else { return false }
        guard (try? FileManager.default.attributesOfItem(atPath: link.path)) == nil else {
            throw ControlError("The command line tool couldn't be removed.")
        }
        return true
    }

    /// Paths reach the shell only through AppleScript's `quoted form`, never by string interpolation.
    static let script = """
    on run argv
        set tool to quoted form of (item 2 of argv)
        set target to quoted form of (item 3 of argv)
        set guard to "if [ -e " & target & " ] && [ ! -L " & target & " ]; then exit 1; fi; "
        if (item 1 of argv) is "install" then
            set command to guard & "/bin/mkdir -p " & quoted form of (item 4 of argv) & " && /bin/ln -sfh " & tool & " " & target
        else
            set command to guard & "/bin/rm -f " & target
        end if
        do shell script command with prompt "Tailmulti wants to change its command line tool." with administrator privileges
    end run
    """

    public static let administratorPrompt: Elevation = { operation, tool, link in
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/usr/bin/osascript")
        process.arguments = ["-e", script, "--", operation == .install ? "install" : "remove", tool.path, link.path,
                             link.deletingLastPathComponent().path]
        let errors = Pipe()
        process.standardError = errors
        process.standardOutput = FileHandle.nullDevice
        try process.run()
        let message = String(decoding: errors.fileHandleForReading.readDataToEndOfFile(), as: UTF8.self)
        process.waitUntilExit()
        if process.terminationStatus == 0 { return true }
        // -128 is AppleScript's "User canceled".
        if message.contains("-128") { return false }
        throw ControlError("macOS didn't allow the change to \(link.path).")
    }
}
