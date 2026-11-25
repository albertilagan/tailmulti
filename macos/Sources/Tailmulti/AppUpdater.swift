import SwiftUI
import Combine
import Sparkle

/// Sparkle updates for Developer ID releases only. Development and ad-hoc builds never replace themselves
/// with a release, which would also mix signing identities for the background service.
@MainActor
final class AppUpdater: NSObject, ObservableObject, @preconcurrency SPUStandardUserDriverDelegate {
    private var controller: SPUStandardUpdaterController?
    @Published private(set) var canCheck = false
    @Published private(set) var availableVersion: String?

    override init() {
        super.init()
        guard Self.isReleaseBuild else { return }
        let controller = SPUStandardUpdaterController(startingUpdater: true, updaterDelegate: nil, userDriverDelegate: self)
        self.controller = controller
        controller.updater.publisher(for: \.canCheckForUpdates).assign(to: &$canCheck)
    }

    var isAvailable: Bool { controller != nil }

    var automaticallyChecks: Bool {
        get { controller?.updater.automaticallyChecksForUpdates ?? false }
        set {
            controller?.updater.automaticallyChecksForUpdates = newValue
            objectWillChange.send()
        }
    }

    func checkForUpdates() { controller?.checkForUpdates(nil) }

    var commandTitle: String {
        availableVersion.map { "Update to \($0)…" } ?? "Check for Updates…"
    }

    var supportsGentleScheduledUpdateReminders: Bool { true }

    func standardUserDriverShouldHandleShowingScheduledUpdate(_ update: SUAppcastItem,
                                                              andInImmediateFocus immediateFocus: Bool) -> Bool {
        update.isCriticalUpdate
    }

    func standardUserDriverWillHandleShowingUpdate(_ handleShowingUpdate: Bool, forUpdate update: SUAppcastItem,
                                                  state: SPUUserUpdateState) {
        availableVersion = update.displayVersionString
    }

    func standardUserDriverWillFinishUpdateSession() {
        availableVersion = nil
    }

    /// Release bundles record Developer ID signing in their setup manifest and carry a feed URL.
    private static var isReleaseBuild: Bool {
        guard Bundle.main.object(forInfoDictionaryKey: "SUFeedURL") is String else { return false }
        let manifest = Bundle.main.bundleURL.appendingPathComponent("Contents/Resources/SetupManifest.json")
        struct Signing: Decodable { let signingMode: String }
        guard let data = try? Data(contentsOf: manifest),
              let signing = try? JSONDecoder().decode(Signing.self, from: data) else { return false }
        return signing.signingMode == "developer-id"
    }
}

struct CheckForUpdatesCommand: View {
    @ObservedObject var updater: AppUpdater

    var body: some View {
        if updater.isAvailable {
            Button(updater.commandTitle) { updater.checkForUpdates() }
                .disabled(!updater.canCheck)
        }
    }
}
