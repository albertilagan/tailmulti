import SwiftUI
import TailmultiKit

struct SetupView: View {
    @EnvironmentObject var model: EngineModel
    @State private var confirmingAllow = false

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                VStack(alignment: .leading, spacing: 6) {
                    Text("Set up Tailmulti").font(.title2.weight(.semibold))
                    Text("Tailmulti uses a background service to keep each account's traffic separate. Your network stays as it is until you turn Tailmulti on.")
                        .foregroundStyle(.secondary)
                }
                if case .serviceProblem(let message) = model.stage {
                    Notice(symbol: "xmark.octagon.fill", tint: .red, title: "The background service isn't responding correctly", detail: message)
                }
                VStack(spacing: 0) {
                    SetupStep(number: 1, title: "Allow the background service", state: serviceState) { serviceBody }
                    Divider().padding(.leading, 44)
                    SetupStep(number: 2, title: "Check this Mac", state: checkState) { checksBody }
                    Divider().padding(.leading, 44)
                    SetupStep(number: 3, title: "Turn on Tailmulti", state: enableState) { enableBody }
                }
                .background(.background.secondary, in: RoundedRectangle(cornerRadius: 10))
                .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(.separator))
                if let names = savedPreviewNames, !names.isEmpty {
                    PreviewNamesNote(names: names)
                }
            }
            .padding(28)
            .frame(maxWidth: 600, alignment: .leading)
            .frame(maxWidth: .infinity)
        }
        .navigationTitle("Tailmulti")
        .confirmationDialog("Allow Tailmulti's background service?", isPresented: $confirmingAllow) {
            Button("Allow") { model.allowBackgroundService() }
        } message: {
            Text(allowMessage)
        }
    }

    private var savedPreviewNames: [String]? {
        if model.source == .service { return model.missingPreviewNames }
        return model.previewStatus?.accounts.map(\.name)
    }

    // MARK: Step 1

    private var allowMessage: String {
        var text = "macOS will ask an administrator to approve it in System Settings. The service runs as root for the whole Mac, but only your user account and administrators can control it. It starts with Tailmulti off; turning it on is a separate step."
        if case .success(let service) = model.bundleCheck, service.signingMode == .development {
            text += "\n\nThis is a development build signed on this Mac, not a notarized release."
        }
        if !Bundle.main.bundleURL.path.hasPrefix("/Applications/") {
            text += "\n\nTo start the service when the Mac starts up, keep Tailmulti in the Applications folder."
        }
        return text
    }

    private var serviceState: StepState {
        switch model.stage {
        case .allowService: .current
        case .awaitingApproval, .serviceStarting: .working
        case .cannotRegister: model.bundleCheck == nil ? .working : .problem
        case .serviceProblem: .problem
        case .enableNetworking, .networkingActive: .done
        }
    }

    @ViewBuilder private var serviceBody: some View {
        switch model.stage {
        case .allowService:
            Text("macOS will ask for administrator approval. The service starts with networking off.")
                .foregroundStyle(.secondary)
            Button("Allow Background Service...") { confirmingAllow = true }
                .buttonStyle(.borderedProminent)
                .disabled(model.busy)
        case .awaitingApproval:
            Text("Allow Tailmulti under Login Items in System Settings. This window moves on once the service is running.")
                .foregroundStyle(.secondary)
            Button("Open Login Items Settings") { model.openApprovalSettings() }
        case .serviceStarting:
            Text("macOS lists the service as allowed, but it isn't answering yet. It may still be starting. If this doesn't change within a few seconds, reinstall it. An earlier copy of Tailmulti may have left an outdated registration.")
                .foregroundStyle(.secondary)
            Button("Reinstall Background Service") { model.reinstallBackgroundService() }
                .disabled(model.busy)
        case .cannotRegister(let reason):
            if model.bundleCheck == nil {
                Text("Checking this copy of Tailmulti...").foregroundStyle(.secondary)
            } else {
                Label(reason, systemImage: "exclamationmark.triangle.fill").foregroundStyle(.orange)
            }
        case .serviceProblem:
            Text("Tailmulti won't send commands to a service it can't verify.")
                .foregroundStyle(.secondary)
        case .enableNetworking, .networkingActive:
            Text("Running.").foregroundStyle(.secondary)
        }
    }

    // MARK: Step 2

    private var checkState: StepState {
        if model.checking { return .working }
        if model.setupReport?.isReady == true { return .done }
        if model.setupReport != nil || model.setupError != nil { return .problem }
        return .pending
    }

    @ViewBuilder private var checksBody: some View {
        HStack(alignment: .firstTextBaseline) {
            checkSummary
            Spacer(minLength: 8)
            Button("Check Again") { model.runChecks() }
                .disabled(model.checking || model.stage.checkSource == nil)
        }
        if let report = model.setupReport {
            if report.vpnConflict, !report.dnsDependsOnVPN, EngineModel.tailscaleApp != nil {
                Button("Open Tailscale") { model.openTailscale() }
            }
            DisclosureGroup("Check details") {
                VStack(alignment: .leading, spacing: 10) {
                    ForEach(report.checks) { CheckRow(check: $0) }
                    Text(model.stage.checkSource == .local
                         ? "Checked with your account's permissions. The service checks again before turning anything on."
                         : "Checked by the service. Nothing is changed while checking.")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                .padding(.top, 6)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
    }

    @ViewBuilder private var checkSummary: some View {
        if let error = model.setupError {
            Label(error, systemImage: "exclamationmark.triangle.fill").foregroundStyle(.orange)
        } else if let report = model.setupReport {
            if report.isReady {
                Label("Everything needed is in place.", systemImage: "checkmark.circle.fill").foregroundStyle(.green)
            } else if report.dnsDependsOnVPN {
                Label("Your current VPN also supplies DNS. Keep it connected until we're ready to switch.", systemImage: "exclamationmark.triangle.fill")
                    .foregroundStyle(.orange)
            } else {
                let problems = report.checks.filter { $0.state != .pass }
                Label(problems.count > 1 ? "\(problems[0].title), and \(problems.count - 1) more" : problems.first?.title ?? "The checks didn't pass.",
                      systemImage: "exclamationmark.triangle.fill")
                    .foregroundStyle(.orange)
            }
        } else if model.checking {
            Text("Checking this Mac...").foregroundStyle(.secondary)
        } else {
            Text("Not checked yet.").foregroundStyle(.secondary)
        }
    }

    // MARK: Step 3

    private var enableState: StepState {
        switch model.stage {
        case .networkingActive: .done
        case .enableNetworking: model.networkTransition ? .working : .current
        default: .pending
        }
    }

    @ViewBuilder private var enableBody: some View {
        switch model.stage {
        case .enableNetworking, .networkingActive:
            NetworkToggle(title: "Tailmulti")
            NetworkErrorText()
        default:
            Text("Available once the background service is running.").foregroundStyle(.secondary)
        }
    }
}

private enum StepState { case pending, current, working, done, problem }

private struct SetupStep<Content: View>: View {
    let number: Int
    let title: String
    let state: StepState
    @ViewBuilder let content: Content

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            badge.frame(width: 22, height: 22)
            VStack(alignment: .leading, spacing: 8) {
                Text(title).font(.headline)
                    .foregroundStyle(state == .pending ? .secondary : .primary)
                content
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(14)
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Step \(number), \(title)")
    }

    @ViewBuilder private var badge: some View {
        switch state {
        case .done:
            Image(systemName: "checkmark.circle.fill").font(.title3).foregroundStyle(.green)
        case .problem:
            Image(systemName: "exclamationmark.circle.fill").font(.title3).foregroundStyle(.orange)
        case .working:
            ProgressView().controlSize(.small)
        case .current, .pending:
            Text("\(number)")
                .font(.callout.weight(.semibold))
                .foregroundStyle(state == .current ? Color.white : Color.secondary)
                .frame(width: 22, height: 22)
                .background(Circle().fill(state == .current ? Color.accentColor : Color.secondary.opacity(0.15)))
        }
    }
}

private struct CheckRow: View {
    let check: SetupCheck

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Image(systemName: symbol).foregroundStyle(tint).accessibilityLabel(stateLabel)
            VStack(alignment: .leading, spacing: 2) {
                Text(check.title)
                Text(check.detail).font(.callout).foregroundStyle(.secondary)
            }
        }
        .accessibilityElement(children: .combine)
    }

    private var symbol: String {
        switch check.state {
        case .pass: "checkmark.circle.fill"
        case .blocked: "xmark.circle.fill"
        case .unknown: "questionmark.circle.fill"
        }
    }
    private var tint: Color {
        switch check.state {
        case .pass: .green
        case .blocked: .red
        case .unknown: .orange
        }
    }
    private var stateLabel: String {
        switch check.state {
        case .pass: "Passed"
        case .blocked: "Blocked"
        case .unknown: "Couldn't check"
        }
    }
}

struct Notice: View {
    let symbol: String
    let tint: Color
    let title: String
    let detail: String

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Image(systemName: symbol).foregroundStyle(tint)
            VStack(alignment: .leading, spacing: 2) {
                Text(title).fontWeight(.semibold)
                Text(detail).foregroundStyle(.secondary)
            }
            Spacer(minLength: 0)
        }
        .font(.callout)
        .padding(10)
        .background(tint.opacity(0.08), in: RoundedRectangle(cornerRadius: 8))
        .accessibilityElement(children: .combine)
    }
}

/// Saved preview names. Copying adds names only; each account still signs in separately.
struct PreviewNamesNote: View {
    @EnvironmentObject var model: EngineModel
    let names: [String]

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Image(systemName: "tag").foregroundStyle(.secondary)
            VStack(alignment: .leading, spacing: 4) {
                Text("Saved from preview: \(names.joined(separator: ", "))")
                Text(model.source == .service
                     ? "Copy these names to the service. This adds the names only. Each account still needs its own Tailscale sign-in, and the preview copies stay as they are."
                     : "You can copy these names once the background service is running. Each account will still need its own Tailscale sign-in.")
                    .font(.callout)
                    .foregroundStyle(.secondary)
            }
            Spacer(minLength: 8)
            if model.source == .service {
                VStack(spacing: 6) {
                    Button("Copy Names") { model.copyPreviewNames() }.disabled(model.busy)
                    if model.isNetworkingActive {
                        Button("Not Now") { model.labelOfferDismissed = true }.buttonStyle(.link).font(.callout)
                    }
                }
            }
        }
        .padding(12)
        .background(.background.secondary, in: RoundedRectangle(cornerRadius: 8))
    }
}
