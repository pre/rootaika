import Foundation
import AppKit

/// On-screen debug trace window, the macOS equivalent of the Windows client's
/// debug console. When the server enables debug mode the agent makes this
/// visible; it shows, terminal-style, every observation, queued event, upload +
/// server response, config poll, and lock action so the user can see what the
/// client is doing and why screen time is (or isn't) reported.
///
/// Every line is also mirrored to stderr, so the same trace is captured in the
/// LaunchAgent log even when the window is hidden.
///
/// Thread-safety: `log(_:)` and `setVisible(_:)` are callable from any thread or
/// actor (the `Core` actor logs through this). All AppKit access is marshalled to
/// the main thread; the backlog buffer is guarded by a lock.
final class MacDebugConsole: NSObject, DebugLogging {
    // Backlog so lines logged before the window exists (or while hidden) are not
    // lost: they are flushed into the text view when it is first built.
    private let bufferLock = NSLock()
    private var backlog: [String] = []
    private var visible = false

    // Main-thread-only UI state.
    private var window: NSWindow?
    private var textView: NSTextView?

    // Cap the on-screen text so a long-running session does not grow unbounded.
    private let maxCharacters = 200_000

    private static let timestampFormatter: DateFormatter = {
        let f = DateFormatter()
        f.locale = Locale(identifier: "en_US_POSIX")
        f.dateFormat = "HH:mm:ss.SSS"
        return f
    }()

    override init() {
        super.init()
    }

    // MARK: - DebugLogging

    func log(_ line: String) {
        let stamped = "\(MacDebugConsole.timestampFormatter.string(from: Date())) \(line)"

        // Always mirror to stderr for the LaunchAgent log.
        FileHandle.standardError.write(Data((stamped + "\n").utf8))

        bufferLock.lock()
        backlog.append(stamped)
        // Bound the backlog independently of the text view.
        if backlog.count > 5_000 {
            backlog.removeFirst(backlog.count - 5_000)
        }
        let shouldRender = visible
        bufferLock.unlock()

        if shouldRender {
            runOnMain { self.appendToTextView(stamped + "\n") }
        }
    }

    func setVisible(_ show: Bool) {
        bufferLock.lock()
        let wasVisible = visible
        visible = show
        bufferLock.unlock()
        if show == wasVisible { return }

        runOnMain {
            if show {
                self.buildWindowIfNeeded()
                self.flushBacklog()
                self.window?.makeKeyAndOrderFront(nil)
                NSApp.activate(ignoringOtherApps: true)
            } else {
                self.window?.orderOut(nil)
            }
        }
    }

    // MARK: - Main-thread UI

    private func buildWindowIfNeeded() {
        if window != nil { return }

        let frame = NSRect(x: 0, y: 0, width: 760, height: 460)
        let win = NSWindow(
            contentRect: frame,
            styleMask: [.titled, .closable, .miniaturizable, .resizable],
            backing: .buffered,
            defer: false
        )
        win.title = "rootaika debug"
        win.isReleasedWhenClosed = false
        win.center()

        let scroll = NSScrollView(frame: frame)
        scroll.hasVerticalScroller = true
        scroll.autoresizingMask = [.width, .height]
        scroll.drawsBackground = true
        scroll.backgroundColor = NSColor(srgbRed: 0.07, green: 0.08, blue: 0.09, alpha: 1.0)

        let text = NSTextView(frame: frame)
        text.isEditable = false
        text.isSelectable = true
        text.drawsBackground = true
        text.backgroundColor = NSColor(srgbRed: 0.07, green: 0.08, blue: 0.09, alpha: 1.0)
        text.textColor = NSColor(srgbRed: 0.85, green: 0.87, blue: 0.88, alpha: 1.0)
        text.font = NSFont.monospacedSystemFont(ofSize: 12, weight: .regular)
        text.autoresizingMask = [.width]
        text.textContainerInset = NSSize(width: 8, height: 8)
        text.isVerticallyResizable = true
        text.isHorizontallyResizable = false
        text.textContainer?.widthTracksTextView = true

        scroll.documentView = text
        win.contentView = scroll

        self.window = win
        self.textView = text
    }

    private func flushBacklog() {
        guard let text = textView else { return }
        bufferLock.lock()
        let lines = backlog
        bufferLock.unlock()
        text.string = lines.joined(separator: "\n") + (lines.isEmpty ? "" : "\n")
        scrollToBottom()
    }

    private func appendToTextView(_ chunk: String) {
        guard let text = textView else { return }
        let attr = NSAttributedString(
            string: chunk,
            attributes: [
                .font: NSFont.monospacedSystemFont(ofSize: 12, weight: .regular),
                .foregroundColor: NSColor(srgbRed: 0.85, green: 0.87, blue: 0.88, alpha: 1.0),
            ]
        )
        text.textStorage?.append(attr)
        trimIfNeeded()
        scrollToBottom()
    }

    private func trimIfNeeded() {
        guard let storage = textView?.textStorage else { return }
        let overflow = storage.length - maxCharacters
        if overflow > 0 {
            storage.deleteCharacters(in: NSRange(location: 0, length: overflow))
        }
    }

    private func scrollToBottom() {
        guard let text = textView else { return }
        text.scrollToEndOfDocument(nil)
    }

    /// Run `work` on the main thread without deadlocking if already there.
    private func runOnMain(_ work: @escaping () -> Void) {
        if Thread.isMainThread {
            work()
        } else {
            DispatchQueue.main.async(execute: work)
        }
    }
}
