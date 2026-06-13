package lock

import (
	"reflect"
	"testing"
)

func TestCountdownPhrase(t *testing.T) {
	tests := []struct {
		name    string
		seconds int
		message string
		want    string
	}{
		{"two minutes", 120, "", "2 minuuttia jäljellä ennen lukitusta"},
		{"rounds up to two minutes", 90, "", "2 minuuttia jäljellä ennen lukitusta"},
		{"just over a minute is one minute", 61, "", "1 minuutti jäljellä ennen lukitusta"},
		{"fifty seconds", 50, "", "50 sekuntia jäljellä ennen lukitusta"},
		{"exactly a minute speaks seconds", 60, "", "60 sekuntia jäljellä ennen lukitusta"},
		{"one second singular", 1, "", "1 sekunti jäljellä ennen lukitusta"},
		{"zero seconds", 0, "", "0 sekuntia jäljellä ennen lukitusta"},
		{"negative clamps to zero", -5, "", "0 sekuntia jäljellä ennen lukitusta"},
		{"appends lock message", 50, "Aika lopettaa", "50 sekuntia jäljellä ennen lukitusta. Aika lopettaa"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := countdownPhrase(tt.seconds, tt.message); got != tt.want {
				t.Fatalf("countdownPhrase(%d, %q) = %q, want %q", tt.seconds, tt.message, got, tt.want)
			}
		})
	}
}

func TestSpeakSchedule(t *testing.T) {
	tests := []struct {
		name  string
		total int
		want  []int
	}{
		{"thirty seconds steps by ten", 30, []int{30, 20, 10}},
		{"sixty seconds steps by ten", 60, []int{60, 50, 40, 30, 20, 10}},
		{"ninety seconds: one minute then tens", 90, []int{90, 30, 20, 10}},
		{"two minutes: minute marks then tens", 120, []int{120, 60, 50, 40, 30, 20, 10}},
		{"zero produces no marks", 0, []int{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := speakSchedule(tt.total); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("speakSchedule(%d) = %v, want %v", tt.total, got, tt.want)
			}
		})
	}
}
