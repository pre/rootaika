import Foundation
import AppKit
import CoreGraphics

/// Samples system idle time + frontmost process.
///
/// Permissions: neither API used here requires special entitlements or
/// TCC/Accessibility prompts.
///   - CGEventSource.secondsSinceLastEventType reads global HID idle time and
///     works without Accessibility permission.
///   - NSWorkspace.frontmostApplication exposes the frontmost app's identity
///     (name/bundle id) without Accessibility permission. (Reading another
///     app's window contents WOULD require Screen Recording permission, but we
///     only read its name, which is allowed.)
/// The class name and `init()` are part of the cross-module contract.
final class MacActivityProbe: ActivityProbing {
    /// kCGAnyInputEventType (0xFFFFFFFF): "any input event" selector for
    /// CGEventSource.secondsSinceLastEventType. Not exposed as a named CGEventType
    /// case in Swift, so it is reconstructed from its raw value here.
    static let anyInputEventType = CGEventType(rawValue: ~UInt32(0))!

    init() {}

    /// System-wide seconds since the last user input event (keyboard + mouse +
    /// any HID), mirroring Windows GetLastInputInfo. CoreGraphics returns the
    /// interval directly in seconds, so no tick-math is needed.
    func idleSeconds() -> Double {
        // .combinedSessionState = HID system state (all input devices).
        // kCGAnyInputEventType (= ~0, 0xFFFFFFFF) is the "any input event" type.
        // NOTE: do NOT use `.null` here: its rawValue is 0 (kCGEventNull), so the
        // API returns time since the last *null* event, which never fires and
        // yields a bogus, ever-growing idle of tens of thousands of seconds. That
        // pins the agent to state=idle and makes screen time report 0.
        let interval = CGEventSource.secondsSinceLastEventType(
            .combinedSessionState,
            eventType: MacActivityProbe.anyInputEventType
        )
        // Guard against NaN / negative values from the API.
        guard interval.isFinite, interval >= 0 else { return 0 }
        return interval
    }

    /// Lowercased basename / bundle id of the frontmost application, or nil if
    /// none. Mirrors the Windows foreground-process normalization (lowercase
    /// basename). Prefers the bundle identifier, then the executable file name,
    /// then the localized name.
    func frontmostProcessName() -> String? {
        guard let app = NSWorkspace.shared.frontmostApplication else { return nil }

        // 1) Bundle identifier (e.g. "com.google.chrome") — most stable.
        if let bundleID = app.bundleIdentifier, !bundleID.isEmpty {
            return bundleID.lowercased()
        }

        // 2) Executable file name (e.g. "Google Chrome").
        if let exeURL = app.executableURL {
            let name = exeURL.lastPathComponent
            if !name.isEmpty {
                return name.lowercased()
            }
        }

        // 3) Bundle URL last path component (e.g. "Google Chrome.app").
        if let bundleURL = app.bundleURL {
            let name = bundleURL.lastPathComponent
            if !name.isEmpty {
                return name.lowercased()
            }
        }

        // 4) Localized display name as a final fallback.
        if let localized = app.localizedName, !localized.isEmpty {
            return localized.lowercased()
        }

        return nil
    }
}
