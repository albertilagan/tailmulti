import XCTest
@testable import TailmultiKit

final class CommandLineToolTests: XCTestCase {
    private var root: URL!
    private var bundle: URL!
    private var bin: URL!
    private var link: URL { bin.appendingPathComponent("tailmulti") }
    private let files = FileManager.default

    override func setUpWithError() throws {
        root = files.temporaryDirectory.appendingPathComponent("tm-cli-\(UUID().uuidString)")
        bundle = root.appendingPathComponent("Tailmulti.app")
        bin = root.appendingPathComponent("bin")
        try files.createDirectory(at: bundle.appendingPathComponent("Contents/Helpers"), withIntermediateDirectories: true)
        try files.createDirectory(at: bin, withIntermediateDirectories: true)
        try Data("tool".utf8).write(to: bundle.appendingPathComponent(CommandLineTool.bundlePath))
    }

    override func tearDownWithError() throws {
        try? files.setAttributes([.posixPermissions: 0o755], ofItemAtPath: bin.path)
        try? files.removeItem(at: root)
    }

    private func tool(elevate: @escaping CommandLineTool.Elevation = { _, _, _ in XCTFail("Unexpected administrator prompt"); return false }) -> CommandLineTool {
        CommandLineTool(bundle: bundle, link: link, elevate: elevate)
    }

    func testInstallsAndRemovesWithoutPromptInWritableDirectory() throws {
        XCTAssertEqual(tool().state, .notInstalled)
        XCTAssertTrue(try tool().install())
        XCTAssertEqual(tool().state, .installed)
        XCTAssertEqual(try files.destinationOfSymbolicLink(atPath: link.path), bundle.appendingPathComponent(CommandLineTool.bundlePath).path)
        XCTAssertTrue(try tool().remove())
        XCTAssertEqual(tool().state, .notInstalled)
    }

    func testRepointsALinkToAnotherCopy() throws {
        try files.createSymbolicLink(atPath: link.path, withDestinationPath: "/Applications/Old/Tailmulti.app/Contents/Helpers/tailmulti")
        XCTAssertEqual(tool().state, .linkedElsewhere)
        XCTAssertTrue(try tool().install())
        XCTAssertEqual(tool().state, .installed)
    }

    func testNeverReplacesOrRemovesAForeignFile() throws {
        try Data("someone else's".utf8).write(to: link)
        XCTAssertEqual(tool().state, .blocked)
        XCTAssertThrowsError(try tool().install())
        XCTAssertThrowsError(try tool().remove())
        XCTAssertEqual(try Data(contentsOf: link), Data("someone else's".utf8))
    }

    func testMissingBundledCommandIsUnavailable() throws {
        try files.removeItem(at: bundle.appendingPathComponent(CommandLineTool.bundlePath))
        XCTAssertEqual(tool().state, .unavailable)
        XCTAssertThrowsError(try tool().install())
    }

    func testProtectedDirectoryAsksForAdministratorAndHonorsCancel() throws {
        try files.setAttributes([.posixPermissions: 0o555], ofItemAtPath: bin.path)
        XCTAssertFalse(try tool(elevate: { _, _, _ in false }).install())
        XCTAssertEqual(tool().state, .notInstalled)
        let bin = bin!
        let granted: CommandLineTool.Elevation = { operation, tool, link in
            XCTAssertEqual(operation, .install)
            try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: bin.path)
            try FileManager.default.createSymbolicLink(atPath: link.path, withDestinationPath: tool.path)
            try FileManager.default.setAttributes([.posixPermissions: 0o555], ofItemAtPath: bin.path)
            return true
        }
        XCTAssertTrue(try tool(elevate: granted).install())
        XCTAssertEqual(tool().state, .installed)
        // An elevation that reports success without creating the link is an error, not an install.
        try files.setAttributes([.posixPermissions: 0o755], ofItemAtPath: bin.path)
        try files.removeItem(at: link)
        try files.setAttributes([.posixPermissions: 0o555], ofItemAtPath: bin.path)
        XCTAssertThrowsError(try tool(elevate: { _, _, _ in true }).install())
    }
}
