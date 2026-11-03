import Foundation
import Security

/// Static code-signature checks for the app bundle and its bundled service. Errors never include
/// certificate names; only the team identifier is returned.
public enum CodeSignature {
    /// Verifies that `bundle` and `helper` are validly signed by Apple-issued certificates from the same
    /// developer team, and returns that team. Ad-hoc and unsigned code fail.
    public static func verifyTeam(bundle: URL, helper: URL, expectedTeam: String? = nil) throws -> String {
        let app = try staticCode(bundle)
        try check(app, requirement: "anchor apple generic", flags: [.strictValidate, .checkNestedCode])
        guard let team = teamIdentifier(app), isTeamIdentifier(team) else {
            throw ControlError("This copy of Tailmulti isn't signed by a developer team.")
        }
        if let expectedTeam, expectedTeam != team {
            throw ControlError("This copy of Tailmulti was signed by a different developer team than its service manifest expects.")
        }
        let requirement = teamRequirement(team)
        try check(app, requirement: requirement, flags: [.strictValidate, .checkNestedCode])
        try check(try staticCode(helper), requirement: requirement, flags: [.strictValidate])
        return team
    }

    /// Team IDs are ten uppercase letters or digits; anything else is rejected before it reaches a requirement string.
    public static func isTeamIdentifier(_ value: String) -> Bool {
        value.count == 10 && value.allSatisfy { ("A"..."Z").contains($0) || ("0"..."9").contains($0) }
    }

    static func teamRequirement(_ team: String) -> String {
        "anchor apple generic and certificate leaf[subject.OU] = \"\(team)\""
    }

    private static func staticCode(_ url: URL) throws -> SecStaticCode {
        var code: SecStaticCode?
        guard SecStaticCodeCreateWithPath(url as CFURL, [], &code) == errSecSuccess, let code else {
            throw ControlError("This copy of Tailmulti's signature can't be read.")
        }
        return code
    }

    private static func check(_ code: SecStaticCode, requirement text: String, flags: SecCSFlags) throws {
        var requirement: SecRequirement?
        guard SecRequirementCreateWithString(text as CFString, [], &requirement) == errSecSuccess, let requirement else {
            throw ControlError("This copy of Tailmulti's signature can't be checked.")
        }
        guard SecStaticCodeCheckValidity(code, flags, requirement) == errSecSuccess else {
            throw ControlError("This copy of Tailmulti isn't validly signed by a developer team, so macOS won't run its background service.")
        }
    }

    private static func teamIdentifier(_ code: SecStaticCode) -> String? {
        var info: CFDictionary?
        guard SecCodeCopySigningInformation(code, SecCSFlags(rawValue: kSecCSSigningInformation), &info) == errSecSuccess,
              let dict = info as? [String: Any] else { return nil }
        return dict[kSecCodeInfoTeamIdentifier as String] as? String
    }
}

private extension SecCSFlags {
    static let strictValidate = SecCSFlags(rawValue: kSecCSStrictValidate)
    static let checkNestedCode = SecCSFlags(rawValue: kSecCSCheckNestedCode)
}
