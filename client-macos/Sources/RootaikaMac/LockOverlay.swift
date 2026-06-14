import Foundation
import AppKit
import AVFoundation

/// Drives the full-screen lock overlay.
///
/// `showLock` engages an opaque black borderless `NSWindow` on EVERY screen at
/// `CGShieldingWindowLevel()` (above the menu bar), applies a kiosk
/// `NSApp.presentationOptions` set so Cmd-Tab / Cmd-Q / Mission Control are
/// blocked, hides the cursor, swallows key/mouse events, and shows the admin
/// message centered in large white text. `hideLock` tears everything down and
/// restores the previous presentation options.
///
/// The pre-lock WARNING countdown is owned by `Core` (it sleeps for
/// `warningSeconds` and only then calls `showLock`). This controller still
/// exposes `showWarning(message:remainingSeconds:)` / `hideWarning()` so the
/// caller may optionally drive a click-through banner during that countdown;
/// the banner is non-activating, semi-transparent, and does not block input.
///
/// All UI mutation is dispatched to the main thread; the public flags
/// (`isShowing`) are guarded so calls are safe from the `Core` actor.
final class MacLockController: LockControlling {

    // MARK: State (guarded by `stateLock`)

    private let stateLock = NSLock()
    private var _isShowing = false

    var isShowing: Bool {
        stateLock.lock(); defer { stateLock.unlock() }
        return _isShowing
    }

    // MARK: Main-thread-only UI state

    private var lockWindows: [NSWindow] = []
    private var warningWindows: [NSWindow] = []
    private var savedPresentationOptions: NSApplication.PresentationOptions?
    private var keepOnTopTimer: Timer?
    private var keyMonitor: Any?
    private var cursorHidden = false

    private var currentMessage: String = ""

    // Audio (best-effort).
    private var audioPlayer: AVAudioPlayer?

    init() {}

    // MARK: - LockControlling

    func showLock(message: String, warningSeconds: Int) {
        runOnMain {
            // Idempotent refresh: if already shown, just update the message.
            if !self.lockWindows.isEmpty {
                if message != self.currentMessage {
                    self.currentMessage = message
                    self.updateLockMessage(message)
                }
                return
            }

            // A pre-lock warning banner (if any) is now superseded by the real lock.
            self.tearDownWarningWindows()

            self.currentMessage = message
            self.engageKioskMode()
            self.buildLockWindows(message: message)
            self.startKeepOnTopTimer()
            self.installKeyMonitor()
            self.hideCursorIfNeeded()
            self.playLockAudioIfPresent()

            self.setShowing(true)
        }
    }

    func hideLock() {
        runOnMain {
            self.stopLockAudio()
            self.tearDownWarningWindows()
            self.tearDownLockWindows()
            self.removeKeyMonitor()
            self.stopKeepOnTopTimer()
            self.restoreCursorIfNeeded()
            self.restoreKioskMode()
            self.currentMessage = ""
            self.setShowing(false)
        }
    }

    // MARK: - Optional pre-lock warning banner (Core owns the countdown timing)

    /// Show / refresh a click-through warning banner. Non-activating and
    /// semi-transparent so the screen stays usable during the countdown. Safe to
    /// call repeatedly to update the remaining-seconds text.
    func showWarning(message: String, remainingSeconds: Int) {
        runOnMain {
            // Never compete with an engaged lock overlay.
            guard self.lockWindows.isEmpty else { return }
            if self.warningWindows.isEmpty {
                self.buildWarningWindows()
            }
            self.updateWarningText(message: message, remainingSeconds: remainingSeconds)
        }
    }

    /// Tear down the warning banner (e.g. on unlock-during-countdown).
    func hideWarning() {
        runOnMain { self.tearDownWarningWindows() }
    }

    // MARK: - Lock windows

    private func buildLockWindows(message: String) {
        let level = Int(CGShieldingWindowLevel())
        for screen in NSScreen.screens {
            let window = NSWindow(
                contentRect: screen.frame,
                styleMask: [.borderless],
                backing: .buffered,
                defer: false,
                screen: screen
            )
            window.isReleasedWhenClosed = false
            window.backgroundColor = .black
            window.isOpaque = true
            window.hasShadow = false
            window.level = NSWindow.Level(rawValue: level)
            window.collectionBehavior = [
                .canJoinAllSpaces,
                .fullScreenAuxiliary,
                .stationary,
                .ignoresCycle
            ]
            window.ignoresMouseEvents = false
            window.acceptsMouseMovedEvents = true
            window.setFrame(screen.frame, display: true)

            let content = LockContentView(frame: screen.frame)
            content.message = message
            window.contentView = content
            window.initialFirstResponder = content

            window.makeKeyAndOrderFront(nil)
            window.orderFrontRegardless()
            self.lockWindows.append(window)
        }
        // Make the first window key so it captures input.
        self.lockWindows.first?.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }

    private func updateLockMessage(_ message: String) {
        for window in lockWindows {
            (window.contentView as? LockContentView)?.message = message
        }
    }

    private func tearDownLockWindows() {
        for window in lockWindows {
            window.orderOut(nil)
            window.contentView = nil
        }
        lockWindows.removeAll()
    }

    // MARK: - Warning windows (click-through banner)

    private func buildWarningWindows() {
        guard let screen = NSScreen.main ?? NSScreen.screens.first else { return }
        let bannerHeight: CGFloat = 90
        let frame = NSRect(
            x: screen.frame.minX,
            y: screen.frame.maxY - bannerHeight - 40,
            width: screen.frame.width,
            height: bannerHeight
        )
        let window = NSWindow(
            contentRect: frame,
            styleMask: [.borderless],
            backing: .buffered,
            defer: false,
            screen: screen
        )
        window.isReleasedWhenClosed = false
        window.backgroundColor = NSColor(calibratedRed: 0.541, green: 0.353, blue: 0.0, alpha: 1.0) // amber
        window.isOpaque = false
        window.alphaValue = 0.55
        window.hasShadow = false
        window.level = .statusBar // above normal windows, below shielding
        window.collectionBehavior = [.canJoinAllSpaces, .stationary, .fullScreenAuxiliary, .ignoresCycle]
        window.ignoresMouseEvents = true // click-through
        window.styleMask.insert(.nonactivatingPanel)

        let label = NSTextField(labelWithString: "")
        label.font = .boldSystemFont(ofSize: 28)
        label.textColor = .white
        label.alignment = .center
        label.maximumNumberOfLines = 2
        label.lineBreakMode = .byTruncatingTail
        label.translatesAutoresizingMaskIntoConstraints = false

        let container = NSView(frame: frame)
        container.addSubview(label)
        NSLayoutConstraint.activate([
            label.centerXAnchor.constraint(equalTo: container.centerXAnchor),
            label.centerYAnchor.constraint(equalTo: container.centerYAnchor),
            label.leadingAnchor.constraint(greaterThanOrEqualTo: container.leadingAnchor, constant: 16),
            label.trailingAnchor.constraint(lessThanOrEqualTo: container.trailingAnchor, constant: -16)
        ])
        window.contentView = container
        window.orderFrontRegardless()
        warningWindows.append(window)
    }

    private func updateWarningText(message: String, remainingSeconds: Int) {
        var text = "\(max(0, remainingSeconds)) s jaljella ennen lukitusta"
        if !message.isEmpty {
            text += "\n" + message
        }
        for window in warningWindows {
            if let container = window.contentView,
               let label = container.subviews.compactMap({ $0 as? NSTextField }).first {
                label.stringValue = text
            }
            window.orderFrontRegardless()
        }
    }

    private func tearDownWarningWindows() {
        for window in warningWindows {
            window.orderOut(nil)
            window.contentView = nil
        }
        warningWindows.removeAll()
    }

    // MARK: - Kiosk presentation options

    private func engageKioskMode() {
        if savedPresentationOptions == nil {
            savedPresentationOptions = NSApp.presentationOptions
        }
        let options: NSApplication.PresentationOptions = [
            .disableProcessSwitching,
            .disableForceQuit,
            .disableSessionTermination,
            .hideDock,
            .hideMenuBar,
            .disableAppleMenu
            // NOTE: `.disableLaunchpad` is not available in the macOS SDK; the
            // kiosk set above already blocks Cmd-Tab / Cmd-Q / Mission Control.
        ]
        // hideMenuBar requires hideDock; the set above already includes both.
        NSApp.presentationOptions = options
    }

    private func restoreKioskMode() {
        if let saved = savedPresentationOptions {
            NSApp.presentationOptions = saved
            savedPresentationOptions = nil
        } else {
            NSApp.presentationOptions = []
        }
    }

    // MARK: - Keep-on-top re-assertion

    private func startKeepOnTopTimer() {
        stopKeepOnTopTimer()
        let timer = Timer(timeInterval: 0.5, repeats: true) { [weak self] _ in
            self?.reassertTop()
        }
        RunLoop.main.add(timer, forMode: .common)
        keepOnTopTimer = timer
    }

    private func reassertTop() {
        let level = Int(CGShieldingWindowLevel())
        for window in lockWindows {
            window.level = NSWindow.Level(rawValue: level)
            window.orderFrontRegardless()
        }
        lockWindows.first?.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }

    private func stopKeepOnTopTimer() {
        keepOnTopTimer?.invalidate()
        keepOnTopTimer = nil
    }

    // MARK: - Key event swallowing

    private func installKeyMonitor() {
        removeKeyMonitor()
        // Swallow key-down / key-equivalents (Cmd-Q, Cmd-Tab, Cmd-W, etc.) while
        // the lock is up. Returning nil consumes the event.
        keyMonitor = NSEvent.addLocalMonitorForEvents(
            matching: [.keyDown, .keyUp, .flagsChanged]
        ) { _ in
            return nil
        }
    }

    private func removeKeyMonitor() {
        if let monitor = keyMonitor {
            NSEvent.removeMonitor(monitor)
            keyMonitor = nil
        }
    }

    // MARK: - Cursor

    private func hideCursorIfNeeded() {
        if !cursorHidden {
            NSCursor.hide()
            cursorHidden = true
        }
    }

    private func restoreCursorIfNeeded() {
        if cursorHidden {
            NSCursor.unhide()
            cursorHidden = false
        }
    }

    // MARK: - Audio (best-effort)

    private func playLockAudioIfPresent() {
        guard let url = Self.lockVoiceFileURL() else { return }
        do {
            let player = try AVAudioPlayer(contentsOf: url)
            player.prepareToPlay()
            player.play()
            audioPlayer = player
        } catch {
            // Best-effort: ignore missing/unreadable audio. Never block the lock.
            audioPlayer = nil
        }
    }

    private func stopLockAudio() {
        audioPlayer?.stop()
        audioPlayer = nil
    }

    /// Resolve ~/Library/Application Support/rootaika/lock-voice.* if present.
    private static func lockVoiceFileURL() -> URL? {
        let base = FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent("Library/Application Support/rootaika", isDirectory: true)
        let fm = FileManager.default
        guard let entries = try? fm.contentsOfDirectory(
            at: base,
            includingPropertiesForKeys: nil,
            options: [.skipsHiddenFiles]
        ) else { return nil }
        // Match lock-voice.<ext> for common audio extensions.
        let audioExts: Set<String> = ["m4a", "mp3", "aac", "wav", "aiff", "aif", "caf"]
        for url in entries where url.deletingPathExtension().lastPathComponent == "lock-voice" {
            if audioExts.contains(url.pathExtension.lowercased()) {
                return url
            }
        }
        return nil
    }

    // MARK: - Helpers

    private func setShowing(_ value: Bool) {
        stateLock.lock()
        _isShowing = value
        stateLock.unlock()
    }

    /// Run a UI block on the main thread synchronously when already on main,
    /// otherwise asynchronously dispatched. Keeps `Core` (an actor off the main
    /// thread) from touching AppKit on a background thread.
    private func runOnMain(_ block: @escaping () -> Void) {
        if Thread.isMainThread {
            block()
        } else {
            DispatchQueue.main.async(execute: block)
        }
    }
}

// MARK: - Lock content view

/// Opaque black view that draws the centered "rootaika" + admin message and
/// swallows mouse / key events so the user can't interact past the overlay.
private final class LockContentView: NSView {
    private let label = NSTextField(labelWithString: "rootaika")

    var message: String = "" {
        didSet { updateText() }
    }

    override init(frame frameRect: NSRect) {
        super.init(frame: frameRect)
        wantsLayer = true
        layer?.backgroundColor = NSColor.black.cgColor

        label.font = .boldSystemFont(ofSize: 48)
        label.textColor = .white
        label.alignment = .center
        label.backgroundColor = .clear
        label.isBordered = false
        label.isEditable = false
        label.isSelectable = false
        label.maximumNumberOfLines = 0
        label.lineBreakMode = .byWordWrapping
        label.translatesAutoresizingMaskIntoConstraints = false
        addSubview(label)
        NSLayoutConstraint.activate([
            label.centerXAnchor.constraint(equalTo: centerXAnchor),
            label.centerYAnchor.constraint(equalTo: centerYAnchor),
            label.leadingAnchor.constraint(greaterThanOrEqualTo: leadingAnchor, constant: 40),
            label.trailingAnchor.constraint(lessThanOrEqualTo: trailingAnchor, constant: -40)
        ])
        updateText()
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) { fatalError("init(coder:) is not supported") }

    private func updateText() {
        if message.isEmpty {
            label.stringValue = "rootaika"
        } else {
            label.stringValue = "rootaika\n\n\(message)"
        }
    }

    // Make this view (and thus its window) capture key input.
    override var acceptsFirstResponder: Bool { true }
    override func acceptsFirstMouse(for event: NSEvent?) -> Bool { true }

    // Swallow all input so nothing reaches apps behind the shield.
    override func keyDown(with event: NSEvent) { /* swallow */ }
    override func keyUp(with event: NSEvent) { /* swallow */ }
    override func mouseDown(with event: NSEvent) { /* swallow */ }
    override func mouseUp(with event: NSEvent) { /* swallow */ }
    override func rightMouseDown(with event: NSEvent) { /* swallow */ }
    override func otherMouseDown(with event: NSEvent) { /* swallow */ }

    override func performKeyEquivalent(with event: NSEvent) -> Bool {
        // Returning true marks the key equivalent as handled (consumed).
        return true
    }
}
