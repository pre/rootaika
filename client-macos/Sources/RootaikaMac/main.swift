import Foundation
import AppKit

// Entry point. One binary, two long-lived roles (mirrors the Windows client's
// service/agent subcommand dispatch):
//   daemon                : root LaunchDaemon; config, SQLite buffer, upload +
//                           config long-poll, agent endpoint, watchdog
//   agent (default)       : user-session LaunchAgent; activity observation and
//                           the lock overlay, talking to the daemon on loopback
// plus dev helpers:
//   --selftest            : no GUI; exercise config load + JSON round-trip; exit 0
//   --test-lock <seconds> : show the lock overlay for N seconds, then exit
//   --server <url>        : override server URL (daemon only)
//   --debug               : force debug mode on for this run

struct CLIOptions {
    var command: String?
    var serverOverride: String?
    var selftest = false
    var testLockSeconds: Int?
    var forceDebug = false
}

func parseArgs(_ args: [String]) -> CLIOptions {
    var opts = CLIOptions()
    var i = 1
    while i < args.count {
        let arg = args[i]
        switch arg {
        case "--selftest":
            opts.selftest = true
        case "--server":
            if i + 1 < args.count { opts.serverOverride = args[i + 1]; i += 1 }
        case "--test-lock":
            if i + 1 < args.count { opts.testLockSeconds = Int(args[i + 1]); i += 1 }
        case "--debug":
            opts.forceDebug = true
        default:
            if !arg.hasPrefix("-") && opts.command == nil {
                opts.command = arg
            }
        }
        i += 1
    }
    return opts
}

func runSelftest() -> Int32 {
    do {
        let config = try Config.load()
        guard UUID(uuidString: config.clientID) != nil else {
            FileHandle.standardError.write(Data("FAIL: invalid client_id\n".utf8))
            return 1
        }

        // JSON round-trip of an event batch.
        let encoder = RootaikaJSON.makeEncoder()
        let decoder = RootaikaJSON.makeDecoder()
        let event = Event(
            eventID: UUID().uuidString,
            occurredAt: Date(),
            state: .active,
            processName: "selftest",
            sequence: 1
        )
        let batch = EventBatch(clientID: config.clientID, events: [event])
        let data = try encoder.encode(batch)
        let decoded = try decoder.decode(EventBatch.self, from: data)
        guard decoded.clientID == batch.clientID, decoded.events.count == 1 else {
            FileHandle.standardError.write(Data("FAIL: batch round-trip mismatch\n".utf8))
            return 1
        }

        // ClientConfig round-trip.
        let cfg = ClientConfig(
            clientID: config.clientID,
            configVersion: "0123456789abcdef",
            idleThresholdSeconds: 60,
            uploadIntervalSeconds: 60,
            pollIntervalSeconds: 30,
            maxCountableGapSeconds: 300,
            debugMode: false,
            locked: false,
            lockMessage: "",
            warningSeconds: 0,
            categories: [CategoryRule(matchType: "exact", pattern: "chrome", category: "web")]
        )
        let cfgData = try encoder.encode(cfg)
        _ = try decoder.decode(ClientConfig.self, from: cfgData)

        // AgentState round-trip (daemon <-> agent IPC payload).
        let stateData = try encoder.encode(AgentState.fallback)
        _ = try decoder.decode(AgentState.self, from: stateData)

        print("OK")
        return 0
    } catch {
        FileHandle.standardError.write(Data("FAIL: \(error)\n".utf8))
        return 1
    }
}

func runTestLock(seconds: Int) -> Int32 {
    let app = NSApplication.shared
    app.setActivationPolicy(.accessory)
    let controller = MacLockController()
    controller.showLock(message: "Test lock", warningSeconds: 0, debugShutdownAllowed: false)
    let deadline = Date().addingTimeInterval(TimeInterval(max(0, seconds)))
    while Date() < deadline {
        RunLoop.current.run(mode: .default, before: Date().addingTimeInterval(0.1))
    }
    controller.hideLock()
    return 0
}

func runDaemon(_ opts: CLIOptions) -> Int32 {
    let daemon: Daemon
    do {
        daemon = try Daemon(options: opts)
        try daemon.start()
    } catch {
        FileHandle.standardError.write(Data("daemon startup failed: \(error)\n".utf8))
        return 1
    }
    dispatchMain()
}

func runAgent(_ opts: CLIOptions) -> Int32 {
    let daemonClient = DaemonClient()
    if DebugShutdownGate.shouldStayStopped(daemon: daemonClient) {
        return 0
    }

    let app = NSApplication.shared
    app.setActivationPolicy(.accessory) // LSUIElement-style: no Dock icon / menu bar

    let probe = MacActivityProbe()
    let lock = MacLockController {
        DebugShutdownMarker.request()
        NSApp.terminate(nil)
    }
    let debug = MacDebugConsole()
    let core = Core(daemon: daemonClient, probe: probe, lock: lock, debug: debug, forceDebug: opts.forceDebug)

    Task.detached {
        await core.run()
    }

    app.run()
    return 0
}

// MARK: Dispatch

let opts = parseArgs(CommandLine.arguments)

let exitCode: Int32
if opts.selftest {
    exitCode = runSelftest()
} else if let seconds = opts.testLockSeconds {
    exitCode = runTestLock(seconds: seconds)
} else if opts.command == "daemon" {
    exitCode = runDaemon(opts)
} else {
    exitCode = runAgent(opts)
}

exit(exitCode)
