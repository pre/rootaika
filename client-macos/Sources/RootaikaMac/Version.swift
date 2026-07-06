import Foundation

/// Build/version identity of this binary.
enum Version {
    /// Version label of the running build. scripts/build.sh rewrites this
    /// line for release builds (the SwiftPM equivalent of the Windows
    /// client's ldflags injection); source default stays "dev".
    static let current = "dev"

    /// Compile-time-fixed release location: OTA downloads are built only from
    /// these plus the server-provided tag/asset, so a compromised server can
    /// never redirect the download elsewhere (mirrors the Windows updater).
    static let gitHubOwner = "pre"
    static let gitHubRepo = "rootaika"
}
