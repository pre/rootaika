package lock

import "fmt"

// These helpers are platform-neutral so they can be unit-tested on Linux CI,
// while the actual overlay + speech synthesis live in controller_windows.go.

// warnSpeakBoundary is the remaining-seconds threshold that switches the spoken
// reminder cadence: above it the reminder repeats once per minute, at or below
// it the reminder repeats every 10 seconds so the final countdown feels urgent.
const warnSpeakBoundary = 60

// countdownPhrase builds the Finnish spoken/overlay reminder for the time left
// before the screen locks. Above one minute it speaks in whole minutes, at or
// below one minute it speaks in seconds. The admin's lock message, when set, is
// appended after the time phrase so the player hears why the lock is coming.
func countdownPhrase(secondsLeft int, lockMessage string) string {
	if secondsLeft < 0 {
		secondsLeft = 0
	}
	var phrase string
	if secondsLeft > warnSpeakBoundary {
		minutes := (secondsLeft + 30) / 60
		if minutes < 1 {
			minutes = 1
		}
		unit := "minuuttia"
		if minutes == 1 {
			unit = "minuutti"
		}
		phrase = fmt.Sprintf("%d %s jäljellä ennen lukitusta", minutes, unit)
	} else {
		unit := "sekuntia"
		if secondsLeft == 1 {
			unit = "sekunti"
		}
		phrase = fmt.Sprintf("%d %s jäljellä ennen lukitusta", secondsLeft, unit)
	}
	if lockMessage != "" {
		phrase += ". " + lockMessage
	}
	return phrase
}

// speakSchedule returns the remaining-second marks at which the reminder should
// be spoken during a warning of total seconds. The first mark is the full
// duration (spoken immediately), then it steps down by one minute while more
// than a minute remains and by 10 seconds for the final minute.
func speakSchedule(total int) []int {
	marks := []int{}
	for remaining := total; remaining > 0; {
		marks = append(marks, remaining)
		step := 10
		if remaining > warnSpeakBoundary {
			step = 60
		}
		remaining -= step
	}
	return marks
}
