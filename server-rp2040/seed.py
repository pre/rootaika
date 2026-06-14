#!/usr/bin/env python3
"""
seed.py — generate (and optionally push) example data for the RP2040 rootaika server.

Produces the files that ARE the board's database on LittleFS:
  seed/devices.json  device registry array (id/uuid/name/...), firmware schema
  seed/settings.json global settings + id counters (so API-created devices
                     after a flash do not reuse seeded ids)
  seed/events.jsonl  append-only activity_observed log, compact firmware format

Default dataset: 6 computers, each reporting every day for the last 30 days,
with a daily screen time randomly between 0 and 400 minutes. Computer names are
single English words.

The events.jsonl lines match exactly what the firmware writes in
handleEventsBatch(), so computeTodaySeconds() in RootaikaServer.ino parses them:
  {"d":1,"id":"...","t":"2026-06-14T09:00:00","s":"active","p":"chrome","seq":1}

Usage:
  ./seed.py                       # (re)generate seed/ files (deterministic)
  ./seed.py --devices 6 --days 30 --max-minutes 400
  ./seed.py --random-seed         # non-deterministic data
  ./seed.py --push http://nappi.local   # feed a LIVE board over the client API

Flashing the generated files onto the board (host with mklittlefs):
  mklittlefs -c seed -s 4194304 littlefs.bin   # 4 MB FS, see README
Then upload littlefs.bin to the FS partition (LittleFS uploader / picotool).
See seed/README.md for details.
"""
from __future__ import annotations

import argparse
import base64
import datetime as dt
import json
import os
import random
import sys
import urllib.request
import urllib.error

# A single English word is a name (computers). Keep > devices default so a
# larger --devices still gets distinct names.
COMPUTER_NAMES = [
    "falcon", "otter", "willow", "ember", "cobalt", "marble",
    "harbor", "thistle", "lantern", "quartz", "meadow", "cinder",
    "pebble", "raven", "saffron", "juniper",
]

# Process names attributed to active gaps. Single English words, app-like.
PROCESS_NAMES = [
    "chrome", "minecraft", "discord", "steam", "roblox",
    "firefox", "spotify", "fortnite", "notepad", "code",
]

GAP_SECONDS = 300  # firmware CFG_MAX_GAP: each active->active gap counts <=300s
AUTH_CLIENT = "Basic " + base64.b64encode(b"client:client").decode()


class Event:
    __slots__ = ("device_index", "event_id", "occurred_at", "state", "process", "sequence")

    def __init__(self, device_index, event_id, occurred_at, state, process, sequence):
        self.device_index = device_index
        self.event_id = event_id
        self.occurred_at = occurred_at  # naive UTC datetime
        self.state = state
        self.process = process
        self.sequence = sequence

    def occurred_at_str(self) -> str:
        return self.occurred_at.strftime("%Y-%m-%dT%H:%M:%S")

    def to_jsonl(self) -> str:
        # Mirror handleEventsBatch() field order/names exactly.
        return (
            '{"d":%d,"id":"%s","t":"%s","s":"%s","p":"%s","seq":%d}'
            % (
                self.device_index,
                self.event_id,
                self.occurred_at_str(),
                self.state,
                self.process,
                self.sequence,
            )
        )

    def to_api(self) -> dict:
        # Full schema the client API expects (handleEventsBatch reads these keys).
        return {
            "event_id": self.event_id,
            "type": "activity_observed",
            "occurred_at": self.occurred_at_str(),
            "state": self.state,
            "process_name": self.process if self.state == "active" else "",
            "sequence": self.sequence,
        }


def build_day_events(device_index, name, day, target_minutes, seq_start, rng):
    """Return events for one device on one day producing exactly target_minutes
    of countable active time, per the firmware's capped-gap rule."""
    events = []
    seq = seq_start

    if target_minutes <= 0:
        # Machine was on but never active: a single idle observation.
        start = day + dt.timedelta(hours=rng.randint(9, 18))
        events.append(Event(device_index, f"seed-{name}-{day.date()}-0",
                            start, "idle", "", seq))
        return events, seq + 1

    total_seconds = target_minutes * 60
    full = total_seconds // GAP_SECONDS  # number of full 300s gaps
    rem = total_seconds - full * GAP_SECONDS

    # Session starts in the morning/afternoon; keep it within the same UTC day.
    start_hour = rng.randint(8, 13)
    t = day + dt.timedelta(hours=start_hour, minutes=rng.randint(0, 59))

    proc = rng.choice(PROCESS_NAMES)
    # full+1 active events spaced GAP_SECONDS apart => full counted gaps.
    for i in range(full + 1):
        if i > 0 and rng.random() < 0.15:
            proc = rng.choice(PROCESS_NAMES)  # occasional app switch
        events.append(Event(device_index, f"seed-{name}-{day.date()}-{i}",
                            t, "active", proc, seq))
        seq += 1
        t = t + dt.timedelta(seconds=GAP_SECONDS)

    # Terminating idle event closes the session. Its gap (rem) is counted
    # because the previous event was active, landing the total exactly on
    # target. When rem == 0 the gap is 0 and contributes nothing.
    idle_time = events[-1].occurred_at + dt.timedelta(seconds=rem)
    events.append(Event(device_index, f"seed-{name}-{day.date()}-end",
                        idle_time, "idle", "", seq))
    seq += 1
    return events, seq


def generate(devices, days, end_date, min_minutes, max_minutes, rng):
    if devices > len(COMPUTER_NAMES):
        sys.exit(f"error: only {len(COMPUTER_NAMES)} names available, "
                 f"asked for {devices} devices")
    names = COMPUTER_NAMES[:devices]
    all_events = []
    for di, name in enumerate(names, start=1):
        seq = 1
        for d in range(days):
            day_offset = days - 1 - d  # oldest first
            day = dt.datetime.combine(
                end_date - dt.timedelta(days=day_offset), dt.time.min)
            target = rng.randint(min_minutes, max_minutes)
            day_events, seq = build_day_events(di, name, day, target, seq, rng)
            all_events.extend(day_events)
    # Sort by time so the log reads chronologically (firmware reads file order).
    all_events.sort(key=lambda e: (e.occurred_at, e.device_index, e.sequence))
    return names, all_events


def device_record(device_id, name):
    """One entry of devices.json, matching the firmware's Device schema in
    storage.h. The uuid equals the name so a later --push (client_id=name) maps
    onto the same device the file seeded. Devices seed unassigned (userId 0)."""
    return {
        "id": device_id,
        "uuid": name,
        "name": name,
        "userId": 0,
        "locked": False,
        "lockMsg": "",
        "warnSeconds": 0,
        "lastStatus": "",
        "idle": 60,
        "upload": 60,
        "poll": 30,
        "lastSeen": events_last_seen.get(device_id, ""),
        # OTA auto-update: no per-device override and no reported version yet.
        "desiredVersion": "",
        "desiredArtifact": "",
        "desiredSha256": "",
        "lastVersion": "",
        "lastVersionAt": "",
    }


def write_files(seed_dir, names, events):
    os.makedirs(seed_dir, exist_ok=True)

    # newest occurred_at per device id, mirrored into Device.lastSeen
    events_last_seen.clear()
    for e in events:
        t = e.occurred_at_str()
        if t > events_last_seen.get(e.device_index, ""):
            events_last_seen[e.device_index] = t

    devices = [device_record(i, n) for i, n in enumerate(names, start=1)]
    with open(os.path.join(seed_dir, "devices.json"), "w") as f:
        json.dump(devices, f)

    # settings.json: defaults + id counters past the seeded ids, so devices/users
    # created via the API after flashing this image get fresh ids.
    settings = {
        "idle": 60, "upload": 60, "poll": 30, "maxGap": 300,
        "chartYMax": 720, "boardRefresh": 60,
        "debug": False, "debugUnassigned": False, "soundVer": 0,
        "nextDeviceId": len(names) + 1, "nextUserId": 1, "nextCategoryId": 1,
        # OTA auto-update: no global desired client version by default.
        "desiredVersion": "", "artifactName": "", "sha256": "",
    }
    with open(os.path.join(seed_dir, "settings.json"), "w") as f:
        json.dump(settings, f)

    with open(os.path.join(seed_dir, "events.jsonl"), "w") as f:
        for e in events:
            f.write(e.to_jsonl() + "\n")


# newest occurred_at per device id, filled by write_files for Device.lastSeen
events_last_seen: dict[int, str] = {}


def push(base_url, names, events, batch_size):
    """Feed a live board via POST /api/v1/events/batch (client role)."""
    base_url = base_url.rstrip("/")
    by_device = {}
    for e in events:
        by_device.setdefault(e.device_index, []).append(e)

    total = 0
    for di, name in enumerate(names, start=1):
        evs = by_device.get(di, [])
        for i in range(0, len(evs), batch_size):
            chunk = evs[i:i + batch_size]
            body = json.dumps({
                "client_id": name,
                "events": [e.to_api() for e in chunk],
            }).encode()
            req = urllib.request.Request(
                base_url + "/api/v1/events/batch", data=body, method="POST")
            req.add_header("Authorization", AUTH_CLIENT)
            req.add_header("Content-Type", "application/json")
            try:
                with urllib.request.urlopen(req, timeout=10) as resp:
                    resp.read()
            except urllib.error.HTTPError as ex:
                sys.exit(f"push failed for {name}: HTTP {ex.code} {ex.read().decode(errors='replace')}")
            except urllib.error.URLError as ex:
                sys.exit(f"push failed for {name}: {ex.reason}")
            total += len(chunk)
        print(f"  pushed {len(evs):5d} events for {name}")
    print(f"pushed {total} events to {base_url}")
    print("note: the board auto-creates a device per client_id and only renders "
          "the newest day; historical days are stored but not shown (no charts).")


def main():
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--devices", type=int, default=6, help="number of computers (default 6)")
    ap.add_argument("--days", type=int, default=30, help="days of history (default 30)")
    ap.add_argument("--end-date", default="2026-06-14",
                    help="newest day, YYYY-MM-DD (default 2026-06-14)")
    ap.add_argument("--min-minutes", type=int, default=0, help="min daily minutes (default 0)")
    ap.add_argument("--max-minutes", type=int, default=400, help="max daily minutes (default 400)")
    ap.add_argument("--seed-dir", default=os.path.join(os.path.dirname(__file__), "seed"),
                    help="output directory (default ./seed)")
    ap.add_argument("--random-seed", action="store_true",
                    help="non-deterministic output (default is reproducible)")
    ap.add_argument("--push", metavar="URL",
                    help="also POST the data to a live board, e.g. http://nappi.local")
    ap.add_argument("--batch-size", type=int, default=20,
                    help="events per POST when pushing (default 20, board body limit ~8KB)")
    ap.add_argument("--no-files", action="store_true",
                    help="do not write seed/ files (useful with --push)")
    args = ap.parse_args()

    try:
        end_date = dt.date.fromisoformat(args.end_date)
    except ValueError:
        sys.exit(f"error: bad --end-date {args.end_date!r}, expected YYYY-MM-DD")
    if args.min_minutes < 0 or args.max_minutes < args.min_minutes:
        sys.exit("error: require 0 <= --min-minutes <= --max-minutes")

    rng = random.Random() if args.random_seed else random.Random(1337)

    names, events = generate(args.devices, args.days, end_date,
                             args.min_minutes, args.max_minutes, rng)

    if not args.no_files:
        write_files(args.seed_dir, names, events)
        print(f"wrote {len(names)} devices and {len(events)} events to {args.seed_dir}/")
        print(f"  newest day: {end_date}  ({args.days} days, "
              f"{args.min_minutes}-{args.max_minutes} min/day)")

    if args.push:
        push(args.push, names, events, args.batch_size)


if __name__ == "__main__":
    main()
