#!/bin/sh
# ax-probe: full Screen Time scrape cycle via System Events AX.
# Usage: ax-probe.sh [member-first-name] [logfile]
# Cycle: back-to-root -> select member -> open App & Website Activity
#        -> read today's rows -> prev day -> read rows -> Today -> back.
MEMBER="${1:?usage: ax-probe.sh member-first-name [logfile]}"
LOG="${2:-$(dirname "$0")/ax-probe.log}"

{
echo "=== probe start $(date '+%Y-%m-%d %H:%M:%S') member=$MEMBER ==="
osascript <<EOF 2>&1
tell application "System Events" to tell process "System Settings"
  set out to {}
  try
    set frontmost to true
  on error e
    set end of out to "frontmost ERR: " & e
  end try
  delay 0.5

  -- 1. ensure we are at the Screen Time root pane
  repeat with attempt from 1 to 3
    if (name of window 1 as text) is "Screen Time" then exit repeat
    click button 1 of group 1 of group 1 of toolbar 1 of window 1
    delay 2
  end repeat
  set end of out to "root title: " & (name of window 1 as text)

  -- 2. select member via blind type-select
  set pb to pop up button "Family Member" of group 1 of scroll area 1 of group 1 of group 3 of splitter group 1 of group 1 of window 1
  set focused of pb to true
  delay 0.5
  key code 49
  delay 1
  keystroke "$MEMBER"
  delay 0.5
  key code 36
  delay 2
  set end of out to "member selected: " & (value of pb as text)

  -- 3. open App & Website Activity (first row button of member pane)
  click button 1 of group 2 of scroll area 1 of group 1 of group 3 of splitter group 1 of group 1 of window 1
  delay 3
  set end of out to "activity title: " & (name of window 1 as text)

  -- 4. read today + previous day
  set pane to group 1 of group 3 of splitter group 1 of group 1 of window 1
  set g1 to group 1 of scroll area 1 of pane
  repeat with pass from 1 to 2
    set dateVal to value of pop up button 1 of g1 as text
    set theOutline to outline 1 of scroll area 1 of group 1 of scroll area 2 of pane
    set end of out to "DAY [" & dateVal & "] rows: " & (count of rows of theOutline)
    repeat with i from 1 to (count of rows of theOutline)
      set rowTexts to {}
      try
        set cellList to UI elements of row i of theOutline
        repeat with el in cellList
          try
            set stList to every static text of group 1 of el
            repeat with j from 1 to (count of stList)
              set end of rowTexts to (value of item j of stList as text)
            end repeat
          end try
        end repeat
      end try
      set AppleScript's text item delimiters to " | "
      set end of out to "  " & (rowTexts as text)
      set AppleScript's text item delimiters to ""
    end repeat
    if pass is 1 then
      click button 1 of g1 -- previous day
      delay 2
    end if
  end repeat
  click button 2 of g1 -- back to Today
  delay 1.5

  -- 5. back to root
  click button 1 of group 1 of group 1 of toolbar 1 of window 1
  delay 2
  set end of out to "back at: " & (name of window 1 as text)

  set AppleScript's text item delimiters to linefeed
  set res to out as text
  set AppleScript's text item delimiters to ""
  return res
end tell
EOF
echo "=== probe end $(date '+%Y-%m-%d %H:%M:%S') exit=$? ==="
echo ""
} >> "$LOG" 2>&1
