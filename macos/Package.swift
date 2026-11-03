// swift-tools-version: 6.0
import PackageDescription
let package = Package(
    name: "Tailmulti",
    platforms: [.macOS("26.0")],
    products: [.executable(name: "Tailmulti", targets: ["Tailmulti"])],
    targets: [
        .target(name: "TailmultiKit"),
        .executableTarget(name: "Tailmulti", dependencies: ["TailmultiKit"]),
        .testTarget(name: "TailmultiKitTests", dependencies: ["TailmultiKit"])
    ]
)
