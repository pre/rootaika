import Foundation
import AppKit

// Entry point. Parses flags and dispatches to the requested mode.
//   --selftest            : no GUI; exercise config load + JSON round-trip + stub event build; print OK; exit 0
//   --test-lock <seconds> : show the lock overlay for N seconds, then exit
//   --server <url>        : override server URL for this run
//   --debug               : force debug mode on for this run (show the console immediately)
//   (default)             : run the agent loop as an accessory (LSUIElement) app

struct CLIOptions {
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
            break
        }
        i += 1
    }
    return opts
}

func loadConfig(_ opts: CLIOptions) throws -> Config {
    var config = try Config.load()
    if let server = opts.serverOverride, !server.isEmpty {
        config.serverURL = server
    }
    if opts.forceDebug {
        config.debugMode = true
    }
    return config
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
    controller.showLock(message: "Test lock", warningSeconds: 0)
    let deadline = Date().addingTimeInterval(TimeInterval(max(0, seconds)))
    while Date() < deadline {
        RunLoop.current.run(mode: .default, before: Date().addingTimeInterval(0.1))
    }
    controller.hideLock()
    return 0
}

func runAgent(_ opts: CLIOptions) -> Int32 {
    let config: Config
    do {
        config = try loadConfig(opts)
    } catch {
        FileHandle.standardError.write(Data("failed to load config: \(error)\n".utf8))
        return 1
    }

    let app = NSApplication.shared
    app.setActivationPolicy(.accessory) // LSUIElement-style: no Dock icon / menu bar

    let board = NetworkBoardClient(config: config)
    let probe = MacActivityProbe()
    let lock = MacLockController()
    let debug = MacDebugConsole()
    let core = Core(config: config, board: board, probe: probe, lock: lock, debug: debug)

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
} else {
    exitCode = runAgent(opts)
}

exit(exitCode)
