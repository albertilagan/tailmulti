// Renders Tailmulti's app icon into an .iconset directory: two accounts' networks in one dot grid.
// Usage: swift scripts/make-icon.swift OUTPUT.iconset
import AppKit

let blue = NSColor(srgbRed: 0.30, green: 0.55, blue: 1.00, alpha: 1)
let teal = NSColor(srgbRed: 0.18, green: 0.83, blue: 0.72, alpha: 1)
let faint = NSColor(white: 1, alpha: 0.16)

func draw(size: Int) -> Data {
    let rep = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: size, pixelsHigh: size, bitsPerSample: 8,
                               samplesPerPixel: 4, hasAlpha: true, isPlanar: false, colorSpaceName: .deviceRGB,
                               bytesPerRow: 0, bitsPerPixel: 0)!
    NSGraphicsContext.saveGraphicsState()
    NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: rep)
    let s = CGFloat(size) / 1024
    // macOS icon grid: an 824-point rounded square centered on the 1024 canvas.
    let tile = NSRect(x: 100 * s, y: 100 * s, width: 824 * s, height: 824 * s)
    let shape = NSBezierPath(roundedRect: tile, xRadius: 185 * s, yRadius: 185 * s)
    NSGradient(starting: NSColor(srgbRed: 0.15, green: 0.17, blue: 0.23, alpha: 1),
               ending: NSColor(srgbRed: 0.06, green: 0.07, blue: 0.10, alpha: 1))!.draw(in: shape, angle: -90)
    // 4x4 grid; the top-left 2x2 is one account, the bottom-right 2x2 another.
    let pitch = 150 * s, radius = 46 * s
    let origin = NSPoint(x: 512 * s - 1.5 * pitch, y: 512 * s + 1.5 * pitch)
    for row in 0..<4 {
        for column in 0..<4 {
            let color: NSColor = row < 2 && column < 2 ? blue : row >= 2 && column >= 2 ? teal : faint
            let center = NSPoint(x: origin.x + CGFloat(column) * pitch, y: origin.y - CGFloat(row) * pitch)
            color.setFill()
            NSBezierPath(ovalIn: NSRect(x: center.x - radius, y: center.y - radius, width: 2 * radius, height: 2 * radius)).fill()
        }
    }
    NSGraphicsContext.restoreGraphicsState()
    return rep.representation(using: .png, properties: [:])!
}

guard CommandLine.arguments.count == 2 else {
    FileHandle.standardError.write(Data("Usage: swift scripts/make-icon.swift OUTPUT.iconset\n".utf8))
    exit(2)
}
let output = URL(fileURLWithPath: CommandLine.arguments[1])
try FileManager.default.createDirectory(at: output, withIntermediateDirectories: true)
for points in [16, 32, 128, 256, 512] {
    try draw(size: points).write(to: output.appendingPathComponent("icon_\(points)x\(points).png"))
    try draw(size: points * 2).write(to: output.appendingPathComponent("icon_\(points)x\(points)@2x.png"))
}
