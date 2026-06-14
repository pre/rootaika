#!/usr/bin/env python3
"""
reset.py — clear the RP2040 rootaika server "database".

The board's database is two LittleFS files: devices.txt and events.jsonl.
There is no remote wipe endpoint in the firmware, so resetting means putting
an EMPTY filesystem back onto the board. This script:

  1. empties the local seed/ files (devices.txt + events.jsonl -> 0 bytes), and
  2. prints the mklittlefs/upload commands to flash the empty FS to the board.

After flashing the empty image and rebooting, the board boots with 0 devices.
(The firmware also self-formats if the FS fails to mount; physically that is
the other way to wipe it.)

Usage:
  ./reset.py                 # empty seed/ files, print reflash instructions
  ./reset.py --delete        # remove seed/ files entirely instead of emptying
  ./reset.py --yes           # skip the confirmation prompt
"""
from __future__ import annotations

import argparse
import os
import sys

SEED_FILES = ("devices.txt", "events.jsonl")


def main():
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--seed-dir", default=os.path.join(os.path.dirname(__file__), "seed"),
                    help="seed directory to clear (default ./seed)")
    ap.add_argument("--delete", action="store_true",
                    help="delete the files instead of truncating them to empty")
    ap.add_argument("--yes", action="store_true", help="do not ask for confirmation")
    args = ap.parse_args()

    seed_dir = args.seed_dir
    action = "delete" if args.delete else "empty"
    if not args.yes:
        reply = input(f"{action} {', '.join(SEED_FILES)} in {seed_dir}/ ? [y/N] ")
        if reply.strip().lower() not in ("y", "yes"):
            sys.exit("aborted")

    os.makedirs(seed_dir, exist_ok=True)
    for name in SEED_FILES:
        path = os.path.join(seed_dir, name)
        if args.delete:
            if os.path.exists(path):
                os.remove(path)
                print(f"deleted {path}")
        else:
            open(path, "w").close()
            print(f"emptied {path}")

    print()
    print("local seed/ is now clear. To wipe the BOARD, flash an empty filesystem:")
    print("  mklittlefs -c seed -s 4194304 littlefs.bin   # 4 MB FS (match your build)")
    print("  # then upload littlefs.bin to the FS partition (picotool / LittleFS uploader)")
    print("Or: erase the board and reflash the sketch; the firmware self-formats on")
    print("first mount failure, booting with 0 devices.")


if __name__ == "__main__":
    main()
