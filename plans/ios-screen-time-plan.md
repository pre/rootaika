# iOS Screen Time integration plan

Goal: include the three children's iOS device usage in rootaika stats.

## Chosen approach (2026-07-10): dedicated-user UI scrape

The cloud SQLite route (below) is dead — data vault. Instead, read what Apple
already renders for a guardian: scrape the Screen Time pane of System Settings
via the Accessibility (AX) tree, from a dedicated macOS user whose only job is
this scrape. The vault is never touched — the privileged ScreenTimeAgent reads
its own vault and renders the UI; we read the render.

### Account & session model
- A dedicated local user `rootaika-scraper`, **signed into a guardian Apple ID**
  in the Family Sharing group. A plain local account sees no family data — the
  data is scoped to the organizer/guardian Apple ID (the organizer's own Apple
  ID on this second local user is fine).
- Runs as a **background (fast-user-switched) session**: the family uses the Mac
  under their own account; the scraper session stays logged in underneath. A
  `launchd` **LaunchAgent** in that session runs the scrape on `StartInterval`
  (~30 min).

### Lid-closed / headless operation — the load-bearing risk
Two macOS realities gate "lid closed, no external display, scrape in background":
1. **Sleep.** Lid-close sleeps the Mac. Override with `sudo pmset -a disablesleep
   1` (no external display needed). Realistic only on **AC power** — disablesleep
   on battery in a closed bag overheats/drains. So lid-closed scraping = AC only.
2. **No display → no pixels.** Internal display off + no external display =
   WindowServer has no framebuffer. **`screencapture`/OCR returns black — never
   depend on screenshots.** Read the **AX tree**: the app's accessibility model
   exists without rendered pixels, and AXPress navigation is delivered without a
   display. Design is AX-first, OCR-never.
   - **Unverified crux:** SwiftUI (macOS 26 System Settings) may build its view —
     and its AX tree — only when the window is actually laid out/shown. An
     offscreen window in a headless background session might expose an *empty* AX
     tree. This can sink the whole approach and MUST be prototyped first.

### Prototype FIRST (before any collector code) — stop if one fails
1. Sign `rootaika-scraper` into the guardian Apple ID; confirm all three
   children's per-app daily usage appears in its System Settings ▸ Screen Time.
2. With Accessibility Inspector (or an `osascript` AX-dump), confirm per-app rows
   (app name + duration text) are reachable as AX values — not opaque SwiftUI.
3. Fast-user-switch away (background session); re-run the AX dump via the
   LaunchAgent. Confirm the tree still populates and navigation still works.
4. Close the lid on AC with `pmset disablesleep 1`; confirm the AX dump still
   works with no display. ← make-or-break for the stated requirement.
5. Check whether the member view exposes a per-device filter (iPhone vs iPad);
   if not, fall back to one rootaika device per child (decision #3 fallback).

### Collector (only if the prototype passes)
Prefer **`osascript` (System Events AX scripting) + shell — no compiled binary**:
osascript reads the rows, shell computes the event UUIDv5 (`python3 hashlib`
one-liner) and `curl`s the batch. Drop to a Swift `AXUIElement` binary only if
osascript's SwiftUI AX handling proves too flaky. LaunchAgent, `StartInterval`
~30 min, `caffeinate` around each run.
- For each family member, for each day in a trailing window, read per-app
  `(display name, duration)`.
- **Reuse the server-side design unchanged:** synthesize `activity_observed`
  events (per-app daily total → `active` heartbeats every ~4 min so the gap cap
  doesn't truncate), UUIDv5-idempotent, POST to `/api/v1/events` (client role).
  Zero server changes. Stateless — re-send the trailing window each run.

**Implementation deviations (2026-07-10, `client-macos-screentime-collector/collector.py`):**
- **Not fully stateless.** Daily totals grow during the day; re-laying the
  sequential layout each run would leave stale events at shifted positions in
  the append-only server DB and corrupt per-app attribution (old idle events
  clip active runs, neighbours' old heartbeats steal time). Instead a state
  file records sent seconds per (member, day, app) and each run appends only
  the **delta** at the end of the member-day "tape" (anchored at local
  midnight). Event UUIDs are pure functions of (member, day, app, tape
  offset), so a crash between POST and state save — or deleting the state
  file — re-sends byte-identical events the server dedupes. State loss is
  safe; state never risks inflation.
- **python3 (stdlib only) + embedded osascript** instead of shell+curl: the
  delta/state/JSON logic outgrew shell. Still no compiled binary, no deps.
- Idle terminator closes each appended segment; on equal timestamps sequence
  numbers order idle before the next active (seq = offset·2 / offset·2+1).

### Deviations from the cloud-store plan (record in the deviations table)
- **Source:** rendered Screen Time UI (AX tree), not `RMAdminStore-Cloud.sqlite`.
- **`process_name`:** app *display name* (UI gives no bundle id / web domain).
- **Granularity:** per-app **per-day** totals; intra-day placement approximate
  (week/month exact, today smeared — same accepted stance as the cloud route).
- **Per-device:** likely **per-member** (iPhone+iPad combined) unless the UI's
  device filter is scriptable → then one device per child.
- **New moving parts:** dedicated guardian-signed-in user, Accessibility TCC
  grant in that account, keep-awake power config.
- **Fragility:** SwiftUI AX tree + scripted navigation break on macOS UI changes
  (more frequent than schema changes). Accepted cost of the no-child-device route.

### Operational notes
- **Reboots break the background session** (macOS auto-logins only one account).
  After a reboot someone logs the scraper user in once, then switches back. For a
  rarely-rebooted home Mac, acceptable; otherwise make the scraper the auto-login
  account and have the family fast-switch to their own.
- Accessibility permission is per-account; grant it while switched into the
  scraper session.

### Prototype run log (2026-07-10)

**Environment.** macOS 26.5.1 (25F80). User `pre` is the Family organizer and the
active console user, so its System Settings ▸ Screen Time should show the family.

**AX access works in principle.** `System Events` returned the frontmost app
name fine — Accessibility APIs are reachable.

**Blocker found and (being) resolved — not Screen-Time-specific.** Reading a
process's *UI-element tree* failed with `-25211 "osascript is not allowed
assistive access"`. Cause: the host terminal app lacked the Accessibility TCC
grant. Parent chain of the shell:
`Terminal.app → login → -zsh → claude → zsh`, i.e. the app to authorize is
**`/System/Applications/Utilities/Terminal.app`**. This is exactly the plan's
"grant Accessibility to the collector's launchd context" step — the TCC grant is
the *only* gate for AX reads; nothing about Screen Time itself blocks it. (For
the real collector, the LaunchAgent's context gets this grant instead of
Terminal.)

**Action:** Accessibility granted to Terminal. Takes effect only after Terminal
relaunches — which kills the Claude session running inside it. Resume below.

**Useful fact:** open the pane directly with
`open "x-apple.systempreferences:com.apple.Screen-Time-Settings.extension"`.

### Prototype step 2 — PASSED (2026-07-10, after Terminal relaunch)

The Screen Time member view is fully scriptable and readable via `osascript`
System Events AX. Verified end to end on macOS 26.5.1:

- **Member selection.** `pop up button "Family Member"` in the Screen Time pane.
  Its menu is NOT in the AX tree (SwiftUI menus are opaque — `AXShowMenu`/click
  expose no menu items), but **blind type-select works**: focus the popup, key
  code 49 (space), `keystroke` the member's first name, return → popup `value`
  reads the full name. Selection is verifiable after the fact via `value of pop up
  button` even though the menu itself is invisible.
- **Navigation into usage.** Member pane rows are unnamed `button`s (labels not
  readable — only `AXAttributedDescription`, which AppleScript can't coerce).
  Blind click of `button 1 of group 2` of the member pane opens
  **App & Website Activity**. Window title carries sync freshness:
  "App & Website Activity – Updated today at 16.12".
- **Per-app rows are plain readable AX static texts** — the make-or-break
  passed: `outline 1 of scroll area 1 of group 1 of scroll area 2` has one row
  per app/domain; `static text` in `UI element 1` = display name, in
  `UI element 2` = duration text ("1 hour\n23 minutes", "44 minutes",
  "22 seconds"). Row 1 is "All Usage" with the day total. Web domains appear as
  their own rows (`event.supercell.com`). Category totals (Games/Entertainment/
  Social) also readable in the header area.
- **Day navigation for the trailing window.** Header `pop up button 1` shows the
  selected day ("Today, July 10"); `button 1` = previous day, `button 3` = next,
  `button 2` = jump to Today. Verified 7 days back, reading date + total + row
  count each day, then restored to Today. Each day loads in ~1 s.
- **Gotchas for the collector:** (a) on first render an app may show its raw
  bundle id (`com.questlab.draftwar`) which resolves to the display name
  ("Draft Showdown") moments later — read rows after a short delay, and expect
  either form; (b) durations are localized English text to parse
  ("N hours\nM minutes" / "M minutes" / "S seconds"); (c) UI language of the
  scraper account should be pinned (English) so parsing stays stable.
- **No per-device filter exists in this view** → per-member modeling confirmed
  (one rootaika device per child, iPhone+iPad combined — plan fallback #3).

### Prototype steps 3–4 — PASSED (2026-07-10): background session + lid closed

Test rig: a loop script waited for the session to leave the console
(`stat -f%Su /dev/console` != user), then ran the full scrape cycle
(`ax-probe.sh`: back-to-root → type-select member → open activity → read
today + yesterday rows → restore) every ~60 s, logging lid state from
`ioreg -k AppleClamshellState`. User fast-switched to the login window
(Control Center ▸ user menu — `CGSession -suspend` no longer exists on
macOS 26), waited 2 min, closed the lid ~7 min, logged back in.

**11/11 probes succeeded, exit 0, full row data every time:**
- probes 1–4: background session, lid open;
- probes 5–11: background session, **lid closed** (clamshell=Yes), AC power,
  `pmset -a disablesleep 1`, no external display.
- Keystroke type-select, AXPress clicks, day navigation and static-text reads
  all work without a framebuffer — the "unverified crux" (empty SwiftUI AX tree
  when headless) did NOT materialize; the tree stays fully populated.
- Data even refreshed live during the lid-closed window: today's total grew
  3 h 10 min → 3 h 27 min and the title's "Updated today at HH.MM" ticked
  forward — iCloud sync + UI refresh continue in the background session.

**Verdict: the UI-scrape approach is viable as specified.** Remaining
prototype item is only step 1's account variant (dedicated `rootaika-scraper`
user signed into a guardian Apple ID — tested here in the organizer's own
session, which is equivalent in kind).

### Next steps (as of 2026-07-10)

The working scrape cycle is committed as `client-macos-screentime-collector/ax-probe.sh` —
the collector grows out of it.

1. **Scraper account** (prototype step 1, final form): create local user
   `rootaika-scraper`, sign it into a guardian Apple ID, pin UI language to
   English (duration parsing depends on it), verify all three children's usage
   shows in its System Settings ▸ Screen Time.
2. **TCC grants in that account:** Accessibility + Automation ("System
   Events") for the collector's launchd context (grant while fast-switched in;
   per-account, one-time).
3. **Collector script** — DONE 2026-07-10: `client-macos-screentime-collector/collector.py`
   (see the deviations note in the Collector section). Verified end to end
   against a local server: 185 events for 2 days × 33 app-days accepted;
   server-computed totals match the UI (board 205 min vs 12286 s scraped;
   per-app Brawl Stars 28 min exact); immediate re-run sends nothing;
   state-file deletion re-sends all 185 → `accepted=0, duplicate_or_ignored=185`.
   `--selftest` covers duration parsing + synthesis determinism/idempotency.
4. **LaunchAgent** — DONE: `bin/start.sh` reads the gitignored `config.env`
   (member first names + server credentials — personal names are never
   committed to the repo; template in `config.env.example`), generates the
   plist (StartInterval 1800, caffeinate -i), loads it and runs immediately.
   The collector `open`s the Screen Time pane itself so a closed/relaunched
   System Settings doesn't strand it. Usage in
   `client-macos-screentime-collector/README.md`.
5. **Host-Mac power config** (only on the designated collector Mac, AC power):
   `sudo pmset -a disablesleep 1` for lid-closed operation. (Reverted on the
   dev laptop after the prototype — re-apply on the real host.)
6. **Operations:** after every reboot, log the scraper user in once and
   fast-switch away (or make it the auto-login account).
7. **Verification:** after a week, compare rootaika per-child totals against
   the Screen Time UI; then record the scrape-route deviations in the
   deviations table of `plans/ruutuaika-suunnitelma.html`.

## Cloud-store route — BLOCKED (rationale for the choice above), macOS 26.5.1 (2026-07-10)

Prerequisite step 3 (open the store manually before writing code) fails: the
chosen source is unreadable on this OS. Full Disk Access is confirmed working
(Safari `History.db`, Messages `chat.db`, Mail all open fine), but
`com.apple.ScreenTimeAgent/Store/` is a macOS **data vault**, which FDA does
not unlock:

- Listing the parent dir hides the entry: `com.apple.ScreenTimeAgent: Operation
  not permitted` (vault signature — not a normal TCC denial, which would show
  the entry and deny open).
- `cp RMAdminStore-Cloud.sqlite` → `Operation not permitted`.
- `sqlite3 file:…RMAdminStore-Cloud.sqlite?mode=ro` → `authorization denied`.
- No readable copy exists: CloudKit cache `~/Library/Caches/CloudKit/
  com.apple.ScreenTimeAgent/…` is empty; no APFS local snapshots; sibling
  `Store/ScreenTimeSettings.sqlite` (readable, non-vaulted) holds only
  limits/allowances, no usage/user/device tables.

Data vaults are enforced by the sandbox kext and deny even root; SIP is enabled
and **disabling SIP would not lift the vault**. The only known ways to read this
file are (a) a process holding Apple's private vault entitlement (not grantable)
or (b) reading the Data volume offline as a forensic image — neither fits an
automatable launchd collector. Forensic tools (mac_apt) read RMAdminStore from
offline images precisely for this reason, not from a live system.

Readable alternative, Petrus only: `~/Library/Application Support/Knowledge/
knowledgeC.db` (not vaulted) holds this Mac's local app usage. It does **not**
contain the children's separate-Apple-ID Family Sharing data.

Consequence: the collector below cannot be built as specified on macOS 26. See
the report for options (offline forensic read = one-off, not scheduled;
DeviceActivityMonitor sideload = the rejected on-device route; Petrus-Mac-only
via knowledgeC; or drop iOS). No code was written pending a decision.

## Background (research summary, 2026-07)

- Apple offers **no official API** (HTTP, CloudKit, or framework) for reading a
  family member's Screen Time data. The iOS Screen Time API
  (FamilyControls/DeviceActivity) deliberately cannot export usage off-device,
  and the DeviceActivityMonitor "threshold hack" would require a sideloaded app
  on every child device with ~5–10 min granularity and open iOS 26 bugs.
- **Chosen route:** macOS caches Family Sharing members' per-app usage, synced
  via iCloud, in an undocumented SQLite store on any Mac signed in as the
  Family Organizer:
  `$(getconf DARWIN_USER_DIR)com.apple.ScreenTimeAgent/Store/RMAdminStore-Cloud.sqlite`
  Relevant tables (schema known from the mac_apt forensic parser):
  `ZUSAGETIMEDITEM` (bundle id / web domain + seconds), `ZUSAGEBLOCK`
  (block start/end), `ZUSAGE` (user+device linkage), `ZCOREUSER` (Apple ID,
  name, family member type), `ZCOREDEVICE` (device name/UDID). Timestamps are
  Cocoa epoch (Unix + 978307200). Reading requires **Full Disk Access**.

## Decisions (made 2026-07-10; #1 and #3 superseded by the scrape route above)

1. **Collection:** ~~read `RMAdminStore-Cloud.sqlite` on a parent Mac~~ →
   superseded: scrape the Screen Time UI via AX. Still no software on the
   children's devices.
2. **Server ingestion: synthetic events, zero server changes.** The collector
   converts Apple usage blocks into ordinary `activity_observed` events and
   POSTs them to the existing `/api/v1/events` endpoint with client-role auth.
3. **Device modeling:** ~~one rootaika device per physical child device keyed
   by `ZCOREDEVICE`~~ → superseded: the UI has no device filter, so **one
   rootaika device per child** (iPhone+iPad combined).
4. **Host Mac: undecided.** The collector must be machine-agnostic: a single
   self-contained binary + a launchd plist, easy to move between Macs.

## New component: `client-macos-screentime-collector/` — OBSOLETE cloud-route design

**Do not implement this section** — it specifies the Go collector for the dead
`RMAdminStore-Cloud.sqlite` route. Kept for reference; the event-synthesis
ideas (UUIDv5 idempotency, heartbeats vs. gap cap, unassigned auto-create)
carry over to the scrape collector and are restated in the Collector section
above.

A small Go program (single `main.go`, module of its own, SQLite via
`modernc.org/sqlite` like the server — no cgo). Runs from launchd every
~30 min (`StartInterval` + `RunAtLoad`). Stateless: no local buffer, no
high-water mark.

Per run:

1. Glob the `RMAdminStore-Cloud.sqlite` path; copy the db + `-wal` file to a
   temp dir and open the copy read-only (avoids WAL/locking issues).
2. Query usage for a trailing window (default 7 days) joining
   `ZUSAGETIMEDITEM → ZUSAGEBLOCK → ZUSAGE → ZCOREUSER/ZCOREDEVICE`, filtered
   to family members other than the local user.
3. Map identities deterministically:
   - `client_uuid` = UUIDv5(fixed namespace, Apple user ID + device UDID).
     First events auto-create the device as `unassigned`; the admin assigns it
     to the child and names it in the existing UI (one-time, ~6 devices).
   - `process_name` = bundle id or web domain as-is (feeds the existing
     `program_categories` mapping).
4. Synthesize events per usage block starting at T: lay the block's apps
   sequentially — `active(app1)@T`, `active(app2)@T+d1`, …, `idle@T+Σd`. For an
   app duration longer than `max_countable_gap_seconds`, emit intermediate
   `active` heartbeats (e.g. every 4 min) so the server's gap cap doesn't
   truncate it. Within-block ordering is an approximation; per-hour totals are
   exact.
5. Event UUID = UUIDv5(client_uuid, block start, app, seq) → re-sending the
   whole window every run is idempotent (server already dedupes on event
   UUID). Upload failures need no retry state: the next run re-covers.

Config via env/flags: server URL, client credentials, window length, poll
interval lives in the plist. Logs to a file; failures must be loud (visible in
log + missing data on server UI).

## Prerequisites & verification (cloud route — obsolete; scrape-route steps are in "Next steps" above)

1. On the parent Mac: Family Screen Time must be enabled for the children
   (true if their usage is visible in System Settings ▸ Screen Time).
2. Grant Full Disk Access to Terminal (for inspection) and later to the
   collector binary / its launchd context.
3. **Before writing any code:** open the DB manually, confirm the children
   appear in `ZCOREUSER`, per-device rows exist via `ZCOREDEVICE`, and recent
   `ZUSAGETIMEDITEM` data matches what System Settings shows. Verify the
   mac_apt-era schema still holds on this macOS version.
4. After implementation: compare a week of rootaika totals against the Screen
   Time UI per child.

## Known limitations / risks

- **Undocumented schema.** Apple may change it in any macOS update; re-verify
  after major updates. This is the accepted cost of the no-app-on-child-device
  route.
- **iCloud sync lag** (minutes to hours): the "today" view undercounts live;
  week/month views are accurate. Accepted.
- **Per-device linkage** (`ZUSAGE → ZCOREDEVICE`) is believed present in the
  cloud store (forensic sources report device names/UDIDs for family members);
  if it proves unreliable, fall back to one device per child.
- The collector Mac must open regularly while signed in with the organizer
  Apple ID; the 7-day window plus idempotent re-send tolerates multi-day gaps.
  (Apple retains roughly 4 weeks of Screen Time history.)
- Plaintext client credentials on the collector Mac — same accepted MVP stance
  as other clients.
