// swift-tools-version: 6.0
import PackageDescription
let package = Package(
    name: "Tailmulti",
    platforms: [.macOS("26.0")],
    products: [.executable(name: "Tailmulti", targets: ["Tailmulti"])],
    dependencies: [
        // Pinned exactly; update deliberately and re-check the embedded framework's signing.
        .package(url: "https://github.com/sparkle-project/Sparkle", exact: "2.10.1")
    ],
    targets: [
        .target(name: "TailmultiKit"),
        .executableTarget(name: "Tailmulti", dependencies: ["TailmultiKit", .product(name: "Sparkle", package: "Sparkle")]),
        .testTarget(name: "TailmultiKitTests", dependencies: ["TailmultiKit"])
    ]
)
