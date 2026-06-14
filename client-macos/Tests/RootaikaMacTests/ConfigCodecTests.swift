import XCTest
@testable import RootaikaMac

final class ConfigCodecTests: XCTestCase {

    func testConfigRoundTripPreservesWarningSoundVersion() throws {
        var cfg = Config.makeDefault()
        cfg.warningSoundVersion = "abc123"

        let data = try JSONEncoder().encode(cfg)
        let decoded = try JSONDecoder().decode(Config.self, from: data)
        XCTAssertEqual(decoded.warningSoundVersion, "abc123")
        XCTAssertEqual(decoded, cfg)
    }

    func testConfigDecodeToleratesMissingWarningSoundVersion() throws {
        // A config written by an older build that lacks the key must still decode,
        // defaulting the field to "" rather than failing the whole decode.
        let json = """
        {
          "server_url": "http://example:8080",
          "client_username": "client",
          "client_password": "client",
          "client_id": "11111111-1111-1111-1111-111111111111",
          "idle_threshold_seconds": 60,
          "upload_interval_seconds": 60,
          "poll_interval_seconds": 30,
          "poll_wait_seconds": 25,
          "observe_interval_seconds": 5,
          "max_countable_gap_seconds": 300,
          "batch_size": 100,
          "locked": false,
          "lock_message": "",
          "lock_warning_seconds": 0,
          "debug_mode": false
        }
        """
        let decoded = try JSONDecoder().decode(Config.self, from: Data(json.utf8))
        XCTAssertEqual(decoded.warningSoundVersion, "")
    }

    func testClientConfigDecodesWarningSoundVersion() throws {
        let json = """
        {
          "client_id": "id",
          "config_version": "deadbeef",
          "idle_threshold_seconds": 60,
          "upload_interval_seconds": 60,
          "poll_interval_seconds": 30,
          "max_countable_gap_seconds": 300,
          "debug_mode": false,
          "locked": true,
          "lock_message": "hi",
          "warning_seconds": 30,
          "warning_sound_version": "snd9",
          "categories": []
        }
        """
        let cfg = try JSONDecoder().decode(ClientConfig.self, from: Data(json.utf8))
        XCTAssertEqual(cfg.warningSoundVersion, "snd9")
        XCTAssertEqual(cfg.warningSeconds, 30)
    }

    func testClientConfigToleratesMissingWarningSoundVersion() throws {
        // An older server that omits warning_sound_version must still decode.
        let json = """
        {
          "client_id": "id",
          "config_version": "deadbeef",
          "idle_threshold_seconds": 60,
          "upload_interval_seconds": 60,
          "poll_interval_seconds": 30,
          "max_countable_gap_seconds": 300,
          "debug_mode": false,
          "locked": false,
          "lock_message": "",
          "warning_seconds": 0,
          "categories": []
        }
        """
        let cfg = try JSONDecoder().decode(ClientConfig.self, from: Data(json.utf8))
        XCTAssertEqual(cfg.warningSoundVersion, "")
    }

    func testFormatRemainingMatchesWindowsForms() {
        XCTAssertEqual(MacLockController.formatRemaining(0), "0 sekuntia jaljella ennen lukitusta")
        XCTAssertEqual(MacLockController.formatRemaining(1), "1 sekunti jaljella ennen lukitusta")
        XCTAssertEqual(MacLockController.formatRemaining(45), "45 sekuntia jaljella ennen lukitusta")
        XCTAssertEqual(MacLockController.formatRemaining(60), "60 sekuntia jaljella ennen lukitusta")
        XCTAssertEqual(MacLockController.formatRemaining(61), "2 minuuttia jaljella ennen lukitusta")
        XCTAssertEqual(MacLockController.formatRemaining(120), "2 minuuttia jaljella ennen lukitusta")
        XCTAssertEqual(MacLockController.formatRemaining(120 + 1), "3 minuuttia jaljella ennen lukitusta")
    }
}
