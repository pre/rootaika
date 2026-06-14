# seed/ — example data for the RP2040 rootaika server

These files **are** the board's database. The firmware stores everything on
LittleFS as plain files (no SQLite on the microcontroller):

| File | Meaning |
|------|---------|
| `devices.json` | Device registry array (`id`/`uuid`/`name`/lock/config), the firmware's `Device` schema. |
| `settings.json` | Global settings plus the `next*Id` counters, so devices/users created via the API after a flash get fresh ids. |
| `events.jsonl` | Append-only `activity_observed` log, one compact JSON object per line. The `d` field is the device `id` from `devices.json`. |

`users.json` and `categories.json` are written by the firmware when you create
users/categories from the Settings page; the seed set does not generate them.

The example set is **6 computers, each reporting every day for the last 30 days,
with a random daily screen time between 0 and 400 minutes**. Computer names are
single English words (`falcon`, `otter`, `willow`, `ember`, `cobalt`, `marble`),
seeded **unassigned** (assign a user on the Settings page to enable locking).

## Event line format

Each line matches exactly what the firmware writes in `handleEventsBatch()`, so
`computeTodaySeconds()` in `RootaikaServer.ino` can parse it:

```json
{"d":1,"id":"seed-falcon-2026-06-14-0","t":"2026-06-14T09:00:00","s":"active","p":"chrome","seq":1}
```

| Key | Field | Notes |
|-----|-------|-------|
| `d` | device id | matches an `id` in `devices.json` |
| `id` | event id | unique, for idempotent re-send |
| `t` | occurred at | RFC3339 UTC, seconds precision |
| `s` | state | `active` / `idle` / `locked` |
| `p` | process name | only meaningful when `active` |
| `seq` | sequence | per-device ordering tiebreaker |

Usage is computed at query time: the gap after an `active` event is attributed
to its process and capped at `max_countable_gap_seconds` (300 s in firmware).
The seed generator spaces `active` events 300 s apart and closes each session
with an `idle` event so each day's countable total lands exactly on its target.

> Note: the current firmware only renders the **newest day** present in the log
> (the root page and `/api/v1/board/today`). The full 30 days are stored but not
> shown; the board has no week/month or chart views (those stay in the Go
> server). A Settings page does exist at `/settings`.

## Regenerating

From `server-rp2040/`:

```sh
./seed.py                         # deterministic default set (6 devices, 30 days, 0-400 min)
./seed.py --random-seed           # non-deterministic
./seed.py --devices 4 --days 14 --max-minutes 240
```

## Flashing onto the board

LittleFS files are flashed as a filesystem image to the FS partition. With
[`mklittlefs`](https://github.com/earlephilhower/mklittlefs):

```sh
# Size must match the FS partition from your build fqbn, e.g.
#   ...:flash=8388608_4194304  -> 4 MB (4194304 bytes) filesystem
mklittlefs -c seed -s 4194304 littlefs.bin
# Then upload littlefs.bin to the FS partition with picotool or the
# Arduino "Pico LittleFS Data Upload" tool.
```

Reboot the board; `storageBegin()` reads `devices.json` + `settings.json` and the
status page shows the seeded totals.

## Pushing to a live board instead

If the board is already running on the LAN, feed it over the client API rather
than reflashing:

```sh
./seed.py --push http://nappi.local           # client:client basic auth
./seed.py --push http://nappi.local --no-files # push only, leave seed/ as-is
```

This POSTs to `/api/v1/events/batch` in small batches (board body limit ~8 KB).

## Resetting

```sh
./reset.py            # empty the local seed/ files, print reflash instructions
./reset.py --delete   # remove the files entirely
```

To wipe the board itself, flash an **empty** filesystem image (run `reset.py`,
then `mklittlefs -c seed ...` on the now-empty folder), or erase and reflash the
sketch, the firmware self-formats on first mount failure and boots with 0 devices.
