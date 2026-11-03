import SwiftUI
import TailmultiKit

/// The one everyday on/off control, shared by the menu bar, main window, Settings and setup.
struct NetworkToggle: View {
    @EnvironmentObject var model: EngineModel
    var title = "Tailmulti"

    var body: some View {
        HStack(alignment: .center, spacing: 12) {
            VStack(alignment: .leading, spacing: 1) {
                Text(title).fontWeight(.semibold)
                Text(model.networkSummary)
                    .font(.callout)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }
            Spacer(minLength: 8)
            if model.networkTransition { ProgressView().controlSize(.small) }
            Toggle(title, isOn: binding)
                .toggleStyle(.switch)
                .labelsHidden()
                .disabled(!model.networkSwitchEnabled)
                .accessibilityLabel(title)
                .accessibilityHint(model.networkSummary)
        }
        .accessibilityElement(children: .contain)
    }

    private var binding: Binding<Bool> {
        Binding(get: { model.networkSwitchIsOn }, set: { model.setNetworkingEnabled($0) })
    }
}

/// Inline networking error, shown under the switch instead of in a dialog.
struct NetworkErrorText: View {
    @EnvironmentObject var model: EngineModel

    var body: some View {
        if let error = model.networkError {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Label(error, systemImage: "exclamationmark.triangle.fill")
                    .foregroundStyle(.orange)
                    .lineLimit(3)
                    .textSelection(.enabled)
                Spacer(minLength: 4)
                Button("Retry") { model.retryNetworking() }
                    .controlSize(.small)
                    .disabled(model.networkTransition || !model.networkSwitchEnabled)
            }
            .font(.callout)
        }
    }
}

/// Compact toolbar version for the main window.
struct ToolbarNetworkToggle: View {
    @EnvironmentObject var model: EngineModel

    var body: some View {
        HStack(spacing: 8) {
            if model.networkTransition { ProgressView().controlSize(.small) }
            Text(model.networkSummary).foregroundStyle(.secondary).lineLimit(1)
            Toggle("Tailmulti", isOn: Binding(get: { model.networkSwitchIsOn }, set: { model.setNetworkingEnabled($0) }))
                .toggleStyle(.switch)
                .labelsHidden()
                .controlSize(.small)
                .disabled(!model.networkSwitchEnabled)
                .accessibilityLabel("Tailmulti")
                .accessibilityHint(model.networkSummary)
        }
        // Inset the content from the toolbar's glass capsule.
        .padding(.leading, 12)
        .padding(.trailing, 6)
        .help(model.networkError ?? model.networkSummary)
        .accessibilityElement(children: .contain)
    }
}
