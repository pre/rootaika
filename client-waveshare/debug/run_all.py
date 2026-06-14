#!/usr/bin/env python3
"""Run the non-interactive e-ink diagnostics in order, with section headers.

Runs 02, 01, 03, 04, then the blind paint (05). Each step is bounded by a
timeout so a hung BUSY wait can't wedge the suite. Watch the glass during the
paint step.

Run with sudo:  sudo python3 run_all.py
"""

import os
import subprocess
import sys

HERE = os.path.dirname(os.path.abspath(__file__))

# (script, timeout seconds)
STEPS = [
    ("02_gpio_toggle.py", 20),
    ("01_busy_level.py", 20),
    ("03_reset_busy_response.py", 30),
    ("04_refresh_busy_watch.py", 30),
    ("05_blind_paint.py", 60),
]


def main() -> int:
    for script, timeout in STEPS:
        print(f"\n{'=' * 60}\n=== {script}\n{'=' * 60}")
        try:
            subprocess.run(
                [sys.executable, os.path.join(HERE, script)],
                timeout=timeout,
                check=False,
            )
        except subprocess.TimeoutExpired:
            print(f"[{script} timed out after {timeout}s]")
    print("\nAll steps done. See debug/README.md for interpretation.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
