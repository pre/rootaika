#!/usr/bin/env python3
"""rootaika iOS Screen Time collector.

Scrapes System Settings > Screen Time family-member per-app daily usage via
the System Events accessibility tree (verified navigation, see
plans/ios-screen-time-plan.md) and posts synthetic activity_observed events
to the rootaika server.

Per (member, day, app) the server-visible usage is laid out as a contiguous
"tape" starting at local midnight. A state file records how many seconds per
app have already been sent; each run appends only the delta at the current
tape end. Event UUIDs are pure functions of (member, day, app, tape offset),
so re-sends after a crash or a lost state file are deduplicated by the server
(INSERT OR IGNORE on event_uuid) and never corrupt attribution.

Usage: collector.py [--dry-run] [--selftest]
Config via environment, see README.md.
"""

import json
import os
import re
import subprocess
import sys
import time
import urllib.request
import uuid
from base64 import b64encode
from datetime import date, datetime, time as dtime, timedelta, timezone
from pathlib import Path

NAMESPACE = uuid.UUID("6ba7b810-9dad-11d1-80b4-00c04fd430c8")  # RFC 4122 DNS ns
NAME_PREFIX = "rootaika-ios-screentime"
HEARTBEAT_SECONDS = 240  # must stay under the server's max_countable_gap_seconds (300)
BATCH_LIMIT = 5000  # server rejects > 10000 events per request

SERVER_URL = os.environ.get("ROOTAIKA_SERVER_URL", "http://192.168.68.199:8080")
CLIENT_USER = os.environ.get("ROOTAIKA_CLIENT_USERNAME", "client")
CLIENT_PASSWORD = os.environ.get("ROOTAIKA_CLIENT_PASSWORD", "client")
MEMBERS = [m.strip() for m in os.environ.get("ROOTAIKA_MEMBERS", "").split(",") if m.strip()]
DAYS = int(os.environ.get("ROOTAIKA_DAYS", "7"))
STATE_FILE = Path(os.environ.get(
    "ROOTAIKA_STATE_FILE",
    str(Path.home() / "Library/Application Support/rootaika-ios-screentime/state.json")))

SCREEN_TIME_URL = "x-apple.systempreferences:com.apple.Screen-Time-Settings.extension"

# AppleScript template: navigation verified in the 2026-07-10 prototype
# (client-macos-screentime-collector/ax-probe.sh). Emits tab-separated machine lines:
#   MEMBER <popup value> / TITLE <window title> / DAY <index> <date label>
#   ROW <app display name> <duration text>
SCRAPE_SCRIPT = r'''
on cleanText(t)
    set res to t
    repeat with bad in {linefeed, tab, return}
        set AppleScript's text item delimiters to (contents of bad)
        set parts to text items of res
        set AppleScript's text item delimiters to " "
        set res to parts as text
    end repeat
    set AppleScript's text item delimiters to ""
    return res
end cleanText

tell application "System Events" to tell process "System Settings"
    set out to {}
    set frontmost to true
    delay 1

    -- 1. ensure we are at the Screen Time root pane
    repeat with attempt from 1 to 5
        if (name of window 1 as text) is "Screen Time" then exit repeat
        click button 1 of group 1 of group 1 of toolbar 1 of window 1
        delay 2
    end repeat
    if (name of window 1 as text) is not "Screen Time" then
        return "ERR" & tab & "not at Screen Time root: " & (name of window 1 as text)
    end if

    -- 2. select member via blind type-select (menu items are not in the AX tree)
    set pb to pop up button "Family Member" of group 1 of scroll area 1 of group 1 of group 3 of splitter group 1 of group 1 of window 1
    set focused of pb to true
    delay 0.5
    key code 49
    delay 1
    keystroke "__MEMBER__"
    delay 0.5
    key code 36
    delay 2
    set end of out to "MEMBER" & tab & my cleanText(value of pb as text)

    -- 3. open App & Website Activity (first row button of the member pane)
    click button 1 of group 2 of scroll area 1 of group 1 of group 3 of splitter group 1 of group 1 of window 1
    delay 3
    set end of out to "TITLE" & tab & my cleanText(name of window 1 as text)

    set pane to group 1 of group 3 of splitter group 1 of group 1 of window 1
    set g1 to group 1 of scroll area 1 of pane
    click button 2 of g1 -- normalize to Today
    delay 2

    -- 4. read the trailing window, newest first
    repeat with d from 0 to (__DAYS__ - 1)
        set end of out to "DAY" & tab & d & tab & my cleanText(value of pop up button 1 of g1 as text)
        try
            set theOutline to outline 1 of scroll area 1 of group 1 of scroll area 2 of pane
            repeat with i from 1 to (count of rows of theOutline)
                try
                    set r to row i of theOutline
                    set nameParts to {}
                    repeat with stEl in (every static text of group 1 of UI element 1 of r)
                        set end of nameParts to (value of stEl as text)
                    end repeat
                    set durParts to {}
                    repeat with stEl in (every static text of group 1 of UI element 2 of r)
                        set end of durParts to (value of stEl as text)
                    end repeat
                    set AppleScript's text item delimiters to " "
                    set rowName to nameParts as text
                    set rowDur to durParts as text
                    set AppleScript's text item delimiters to ""
                    if rowName is not "" and rowDur is not "" then
                        set end of out to "ROW" & tab & my cleanText(rowName) & tab & my cleanText(rowDur)
                    end if
                end try
            end repeat
        end try
        if d < (__DAYS__ - 1) then
            click button 1 of g1 -- previous day
            delay 2
        end if
    end repeat

    -- 5. restore Today and go back to the root pane
    click button 2 of g1
    delay 1.5
    click button 1 of group 1 of group 1 of toolbar 1 of window 1
    delay 2

    set AppleScript's text item delimiters to linefeed
    set res to out as text
    set AppleScript's text item delimiters to ""
    return res
end tell
'''


def log(msg):
    print(f"{datetime.now().strftime('%Y-%m-%d %H:%M:%S')} {msg}", flush=True)


def parse_duration(text):
    """'1 hour 23 minutes' / '44 minutes' / '22 seconds' -> seconds, or None.

    Requires the scraper account's UI language pinned to English.
    """
    total = 0
    matched = False
    for count, unit in re.findall(r"(\d+)\s*(hour|min|sec)", text):
        matched = True
        total += int(count) * {"hour": 3600, "min": 60, "sec": 1}[unit]
    return total if matched else None


def scrape_member(member, days):
    """Run the AX scrape for one member. Returns {day_index: {app: seconds}}."""
    script = SCRAPE_SCRIPT.replace(
        "__MEMBER__", member.replace("\\", "\\\\").replace('"', '\\"')
    ).replace("__DAYS__", str(days))
    proc = subprocess.run(["osascript", "-"], input=script, capture_output=True,
                          text=True, timeout=600)
    if proc.returncode != 0:
        raise RuntimeError(f"osascript failed: {proc.stderr.strip()}")

    days_usage = {}
    current = None
    member_ok = False
    for line in proc.stdout.splitlines():
        parts = line.split("\t")
        if parts[0] == "ERR":
            raise RuntimeError(f"scrape error: {line}")
        if parts[0] == "MEMBER":
            if member.lower() not in parts[1].lower():
                raise RuntimeError(
                    f"member type-select landed on {parts[1]!r}, wanted {member!r}")
            member_ok = True
            log(f"  member popup: {parts[1]}")
        elif parts[0] == "TITLE":
            log(f"  {parts[1]}")
        elif parts[0] == "DAY":
            current = {}
            days_usage[int(parts[1])] = current
            log(f"  day {parts[1]}: {parts[2]}")
        elif parts[0] == "ROW" and current is not None and len(parts) == 3:
            name, dur_text = parts[1].strip(), parts[2]
            seconds = parse_duration(dur_text)
            if seconds is None:
                log(f"  WARN unparseable duration {dur_text!r} for {name!r} — skipped"
                    " (UI language must be English)")
                continue
            if name == "All Usage":
                log(f"    total: {seconds}s")
                continue
            current[name] = current.get(name, 0) + seconds
    if not member_ok:
        raise RuntimeError("scrape produced no MEMBER confirmation")
    if not days_usage:
        raise RuntimeError("scrape produced no DAY sections")
    return days_usage


def day_start_utc(day):
    """Local midnight of `day` as an aware UTC datetime."""
    return datetime.combine(day, dtime()).astimezone().astimezone(timezone.utc)


def synthesize(member, day, appends, tape_start):
    """Lay `appends` [(app, delta_seconds)] on the day tape from `tape_start`.

    Active heartbeats every HEARTBEAT_SECONDS keep the server's gap cap from
    truncating; a trailing idle closes each app's segment. Sequence encodes
    the tape offset (idle before active on equal timestamps).
    """
    base = day_start_utc(day)
    events = []
    off = tape_start
    for app, delta in appends:
        for k in range(0, delta, HEARTBEAT_SECONDS):
            events.append({
                "event_id": str(uuid.uuid5(NAMESPACE, f"{NAME_PREFIX}|{member}|{day}|{app}|{off + k}")),
                "type": "activity_observed",
                "occurred_at": (base + timedelta(seconds=off + k)).strftime("%Y-%m-%dT%H:%M:%SZ"),
                "state": "active",
                "process_name": app,
                "sequence": (off + k) * 2 + 1,
            })
        off += delta
        events.append({
            "event_id": str(uuid.uuid5(NAMESPACE, f"{NAME_PREFIX}|{member}|{day}|idle|{off}")),
            "type": "activity_observed",
            "occurred_at": (base + timedelta(seconds=off)).strftime("%Y-%m-%dT%H:%M:%SZ"),
            "state": "idle",
            "sequence": off * 2,
        })
    return events


def post_events(client_id, events):
    auth = b64encode(f"{CLIENT_USER}:{CLIENT_PASSWORD}".encode()).decode()
    for i in range(0, len(events), BATCH_LIMIT):
        chunk = events[i:i + BATCH_LIMIT]
        req = urllib.request.Request(
            f"{SERVER_URL}/api/v1/events/batch",
            data=json.dumps({"client_id": client_id, "events": chunk}).encode(),
            headers={"Content-Type": "application/json", "Authorization": f"Basic {auth}"},
            method="POST")
        with urllib.request.urlopen(req, timeout=30) as resp:
            body = json.loads(resp.read())
        log(f"  posted {len(chunk)} events: accepted={body['accepted']}"
            f" duplicate_or_ignored={body['duplicate_or_ignored']}")


def load_state():
    try:
        return json.loads(STATE_FILE.read_text())
    except FileNotFoundError:
        return {}


def save_state(state):
    cutoff = (date.today() - timedelta(days=35)).isoformat()
    for member_days in state.values():
        for day in [d for d in member_days if d < cutoff]:
            del member_days[day]
    STATE_FILE.parent.mkdir(parents=True, exist_ok=True)
    tmp = STATE_FILE.with_suffix(".tmp")
    tmp.write_text(json.dumps(state, indent=1, sort_keys=True))
    tmp.replace(STATE_FILE)


def run_member(member, state, dry_run):
    log(f"member {member}: scraping {DAYS} day(s)")
    today = date.today()
    days_usage = scrape_member(member, DAYS)
    if date.today() != today:
        raise RuntimeError("date changed during scrape — skipping this run")

    sent = state.setdefault(member, {})
    events = []
    for index in sorted(days_usage):
        day = today - timedelta(days=index)
        day_sent = sent.setdefault(day.isoformat(), {})
        tape_start = sum(day_sent.values())
        appends = []
        for app in sorted(days_usage[index]):
            total = days_usage[index][app]
            delta = total - day_sent.get(app, 0)
            if delta < 0:
                log(f"  WARN {day} {app!r}: UI total {total}s below sent"
                    f" {day_sent.get(app)}s — ignoring shrink")
            elif delta > 0:
                appends.append((app, delta))
                day_sent[app] = total
        if appends:
            log(f"  {day}: appending {len(appends)} app(s),"
                f" {sum(d for _, d in appends)}s at offset {tape_start}")
            events.extend(synthesize(member, day, appends, tape_start))

    if not events:
        log("  nothing new to send")
        return
    client_id = str(uuid.uuid5(NAMESPACE, f"{NAME_PREFIX}|member:{member}"))
    if dry_run:
        log(f"  dry-run: would post {len(events)} events as client {client_id}")
        for e in events[:10]:
            log(f"    {e['occurred_at']} {e['state']:6} {e.get('process_name', '')}")
        return
    post_events(client_id, events)
    save_state(state)  # only after every batch succeeded; a crash before this re-sends dedupe-safe


def selftest():
    assert parse_duration("1 hour 23 minutes") == 4980
    assert parse_duration("2 hours 1 minute") == 7260
    assert parse_duration("44 minutes") == 2640
    assert parse_duration("22 seconds") == 22
    assert parse_duration("1 tunti") is None

    day = date(2026, 7, 9)
    a = synthesize("TestMember", day, [("AppA", 600), ("AppB", 100)], 0)
    b = synthesize("TestMember", day, [("AppA", 600), ("AppB", 100)], 0)
    assert a == b, "synthesis must be deterministic"
    assert [e["state"] for e in a] == ["active"] * 3 + ["idle", "active", "idle"]
    assert a[0]["occurred_at"] == day_start_utc(day).strftime("%Y-%m-%dT%H:%M:%SZ")
    # idle closing AppA and active opening AppB share a timestamp; idle sorts first
    assert a[3]["occurred_at"] == a[4]["occurred_at"]
    assert a[3]["sequence"] < a[4]["sequence"]
    # growth appends at the tape end with fresh UUIDs, old UUIDs unchanged
    grown = synthesize("TestMember", day, [("AppA", 200)], 700)
    assert not {e["event_id"] for e in grown} & {e["event_id"] for e in a}
    assert len({e["event_id"] for e in a}) == len(a)
    print("selftest OK")


def main():
    if "--selftest" in sys.argv:
        selftest()
        return
    dry_run = "--dry-run" in sys.argv
    if not MEMBERS:
        log("ERROR: ROOTAIKA_MEMBERS is not set")
        sys.exit(2)

    log(f"run start: members={MEMBERS} days={DAYS} server={SERVER_URL} dry_run={dry_run}")
    subprocess.run(["open", SCREEN_TIME_URL], check=True)
    time.sleep(3)

    state = load_state()
    failures = 0
    for member in MEMBERS:
        try:
            run_member(member, state, dry_run)
        except Exception as exc:  # loud, but one member must not sink the others
            log(f"ERROR member {member}: {exc}")
            failures += 1
    log(f"run end: {failures} failure(s)")
    sys.exit(1 if failures else 0)


if __name__ == "__main__":
    main()
