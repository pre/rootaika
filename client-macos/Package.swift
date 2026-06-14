// swift-tools-version:5.9
import PackageDescription

let package = Package(
    name: "RootaikaMac",
    platforms: [
        .macOS(.v13)
    ],
    products: [
        .executable(name: "RootaikaMac", targets: ["RootaikaMac"])
    ],
    targets: [
        .executableTarget(
            name: "RootaikaMac",
            path: "Sources/RootaikaMac"
        ),
        .testTarget(
            name: "RootaikaMacTests",
            dependencies: ["RootaikaMac"],
            path: "Tests/RootaikaMacTests"
        )
    ]
)
