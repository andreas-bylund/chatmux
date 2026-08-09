package ui

import (
	"testing"
	"time"
)

func TestRateCountsAWindowOfMessages(t *testing.T) {
	var r rate
	now := time.Date(2026, 8, 9, 21, 14, 0, 0, time.UTC)

	for i := range 5 {
		r.add(now.Add(time.Duration(i) * time.Second))
	}
	if got := r.perMinute(now.Add(5 * time.Second)); got != 5 {
		t.Errorf("perMinute = %d, want 5", got)
	}

	// Everything above has aged out of the 60-second window.
	if got := r.perMinute(now.Add(2 * time.Minute)); got != 0 {
		t.Errorf("perMinute after two minutes = %d, want 0", got)
	}
}

// Message timestamps come off the network: Twitch's tmi-sent-ts is whatever the
// tag says. A pre-1970 value used to index the bucket ring negatively and take
// the whole client down with it.
func TestRateSurvivesTimestampsBefore1970(t *testing.T) {
	var r rate
	r.add(time.Date(1901, 1, 1, 0, 0, 0, 0, time.UTC))
	r.add(time.UnixMilli(-6795364578871))
	r.add(time.Time{})

	now := time.Date(2026, 8, 9, 21, 14, 0, 0, time.UTC)
	r.add(now)
	if got := r.perMinute(now); got < 1 {
		t.Errorf("perMinute = %d, want the recent message to still be counted", got)
	}
}
