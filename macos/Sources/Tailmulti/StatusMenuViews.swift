import AppKit
import SwiftUI

/// Leading inset shared by custom menu rows so they line up with native item text.
let menuContentInset: CGFloat = 14
let menuContentWidth: CGFloat = 300

/// SwiftUI window actions captured from scene views, so the AppKit status menu can open them.
@MainActor
final class SceneActions {
    static let shared = SceneActions()
    var openMain: (() -> Void)?
    var openSettings: (() -> Void)?

    func showMain() {
        if let openMain {
            openMain()
        } else if let window = NSApp.windows.first(where: { $0.identifier?.rawValue.hasPrefix("main") == true }) {
            window.makeKeyAndOrderFront(nil)
        }
        NSApp.activate()
    }

    func showSettings() {
        NSApp.activate()
        if let openSettings {
            openSettings()
        } else if let item = Self.appMenuItem(keyEquivalent: ",") {
            item.menu?.performActionForItem(at: item.menu?.index(of: item) ?? 0)
        }
    }

    private static func appMenuItem(keyEquivalent: String) -> NSMenuItem? {
        NSApp.mainMenu?.items.first?.submenu?.items.first { $0.keyEquivalent == keyEquivalent && $0.keyEquivalentModifierMask == .command }
    }
}

private struct SceneActionsBridge: ViewModifier {
    @Environment(\.openWindow) private var openWindow
    @Environment(\.openSettings) private var openSettings

    func body(content: Content) -> some View {
        content.onAppear {
            let openWindow = openWindow, openSettings = openSettings
            SceneActions.shared.openMain = { openWindow(id: "main") }
            SceneActions.shared.openSettings = { openSettings() }
        }
    }
}

extension View {
    func registersSceneActions() -> some View { modifier(SceneActionsBridge()) }
}

/// Header row: product name, network summary and the global switch, like the native Tailscale menu.
final class MenuHeaderView: NSView {
    var onToggle: ((Bool) -> Void)?
    private let title = NSTextField(labelWithString: "Tailmulti")
    private let summary = NSTextField(labelWithString: "")
    private let spinner = NSProgressIndicator()
    private let toggle = NSSwitch()

    init() {
        super.init(frame: NSRect(x: 0, y: 0, width: menuContentWidth, height: 46))
        autoresizingMask = [.width]
        title.font = .systemFont(ofSize: NSFont.systemFontSize, weight: .semibold)
        summary.font = .systemFont(ofSize: NSFont.smallSystemFontSize)
        summary.textColor = .secondaryLabelColor
        summary.lineBreakMode = .byTruncatingTail
        spinner.style = .spinning
        spinner.controlSize = .small
        spinner.isDisplayedWhenStopped = false
        toggle.target = self
        toggle.action = #selector(switched)
        toggle.setAccessibilityLabel("Tailmulti")

        let text = NSStackView(views: [title, summary])
        text.orientation = .vertical
        text.alignment = .leading
        text.spacing = 1
        let row = NSStackView(views: [text, NSView(), spinner, toggle])
        row.orientation = .horizontal
        row.alignment = .centerY
        row.spacing = 8
        row.translatesAutoresizingMaskIntoConstraints = false
        text.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        addSubview(row)
        NSLayoutConstraint.activate([
            row.leadingAnchor.constraint(equalTo: leadingAnchor, constant: menuContentInset),
            row.trailingAnchor.constraint(equalTo: trailingAnchor, constant: -menuContentInset),
            row.centerYAnchor.constraint(equalTo: centerYAnchor),
        ])
    }

    required init?(coder: NSCoder) { fatalError("init(coder:) is unused") }

    func update(summary text: String, isOn: Bool, enabled: Bool, working: Bool) {
        summary.stringValue = text
        toggle.state = isOn ? .on : .off
        toggle.isEnabled = enabled
        toggle.setAccessibilityHelp(text)
        working ? spinner.startAnimation(nil) : spinner.stopAnimation(nil)
    }

    @objc private func switched() { onToggle?(toggle.state == .on) }
}

/// Wrapping inline failure text; menu titles would truncate long errors.
final class MenuMessageView: NSView {
    init(_ message: String) {
        super.init(frame: .zero)
        autoresizingMask = [.width]
        let icon = NSImageView(image: NSImage(systemSymbolName: "exclamationmark.triangle.fill", accessibilityDescription: "Error") ?? NSImage())
        icon.contentTintColor = .systemOrange
        icon.setContentHuggingPriority(.required, for: .horizontal)
        let label = NSTextField(wrappingLabelWithString: message)
        label.font = .systemFont(ofSize: NSFont.smallSystemFontSize)
        label.textColor = .secondaryLabelColor
        label.isSelectable = true
        label.maximumNumberOfLines = 4
        let available = menuContentWidth - menuContentInset * 2 - 22
        label.preferredMaxLayoutWidth = available
        let row = NSStackView(views: [icon, label])
        row.alignment = .firstBaseline
        row.spacing = 6
        row.translatesAutoresizingMaskIntoConstraints = false
        addSubview(row)
        NSLayoutConstraint.activate([
            row.leadingAnchor.constraint(equalTo: leadingAnchor, constant: menuContentInset),
            row.trailingAnchor.constraint(lessThanOrEqualTo: trailingAnchor, constant: -menuContentInset),
            row.topAnchor.constraint(equalTo: topAnchor, constant: 3),
            row.bottomAnchor.constraint(equalTo: bottomAnchor, constant: -3),
        ])
        frame = NSRect(x: 0, y: 0, width: menuContentWidth, height: ceil(row.fittingSize.height) + 6)
    }

    required init?(coder: NSCoder) { fatalError("init(coder:) is unused") }
}

/// Search field at the top of an account's device submenu.
final class MenuSearchView: NSView {
    let field = NSSearchField()

    init(placeholder: String) {
        super.init(frame: NSRect(x: 0, y: 0, width: menuContentWidth, height: 32))
        autoresizingMask = [.width]
        field.placeholderString = placeholder
        field.sendsSearchStringImmediately = true
        field.sendsWholeSearchString = false
        field.controlSize = .regular
        field.translatesAutoresizingMaskIntoConstraints = false
        addSubview(field)
        NSLayoutConstraint.activate([
            field.leadingAnchor.constraint(equalTo: leadingAnchor, constant: 10),
            field.trailingAnchor.constraint(equalTo: trailingAnchor, constant: -10),
            field.centerYAnchor.constraint(equalTo: centerYAnchor),
        ])
    }

    required init?(coder: NSCoder) { fatalError("init(coder:) is unused") }
}

extension NSMenuItem {
    static func hosting(_ view: NSView) -> NSMenuItem {
        let item = NSMenuItem()
        item.view = view
        return item
    }

    static func note(_ title: String) -> NSMenuItem {
        let item = NSMenuItem(title: title, action: nil, keyEquivalent: "")
        item.isEnabled = false
        return item
    }
}

enum MenuImages {
    private static let palette: [NSColor] = [.systemBlue, .systemIndigo, .systemTeal, .systemOrange,
                                             .systemPink, .systemPurple, .systemGreen, .systemBrown]

    /// Account avatar matching the window's monogram, with a connection-state badge.
    static func monogram(name: String, badge: NSColor) -> NSImage {
        let sum = name.unicodeScalars.reduce(0) { $0 &+ Int($1.value) }
        let fill = palette[sum % palette.count]
        let letter = name.first.map { String($0).uppercased() } ?? "?"
        let base = NSFont.systemFont(ofSize: 12, weight: .semibold)
        let font = base.fontDescriptor.withDesign(.rounded).flatMap { NSFont(descriptor: $0, size: 12) } ?? base
        let image = NSImage(size: NSSize(width: 26, height: 26), flipped: false) { rect in
            fill.setFill()
            NSBezierPath(ovalIn: rect.insetBy(dx: 1, dy: 1)).fill()
            let text = NSAttributedString(string: letter, attributes: [.font: font, .foregroundColor: NSColor.white])
            let size = text.size()
            text.draw(at: NSPoint(x: rect.midX - size.width / 2, y: rect.midY - size.height / 2))
            let dot = NSRect(x: rect.maxX - 9, y: rect.minY, width: 9, height: 9)
            NSColor.windowBackgroundColor.setFill()
            NSBezierPath(ovalIn: dot).fill()
            badge.setFill()
            NSBezierPath(ovalIn: dot.insetBy(dx: 1.5, dy: 1.5)).fill()
            return true
        }
        image.accessibilityDescription = name
        return image
    }
}
