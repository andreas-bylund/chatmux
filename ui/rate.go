package ui

import "time"

// rate counts messages over a sliding 60-second window using one bucket per
// second. Buckets are cleared as time advances rather than on a timer, so an
// idle pane costs nothing.
type rate struct {
	buckets [60]int
	last    int64 // unix second the window was last advanced to
	started bool
}

func (r *rate) advance(sec int64) {
	if !r.started {
		r.started = true
		r.last = sec
		return
	}
	if sec <= r.last {
		return
	}
	gap := sec - r.last
	if gap >= 60 {
		r.buckets = [60]int{}
	} else {
		for i := int64(1); i <= gap; i++ {
			r.buckets[(r.last+i)%60] = 0
		}
	}
	r.last = sec
}

func (r *rate) add(t time.Time) {
	sec := t.Unix()
	r.advance(sec)
	// A message timestamped in the past (YouTube backfills, clock skew) would
	// land in a stale bucket, so credit it to the current second instead.
	if sec < r.last {
		sec = r.last
	}
	r.buckets[bucket(sec)]++
}

// bucket maps a unix second onto the ring. Go's % keeps the sign of the
// dividend, so a timestamp before 1970 would index the array negatively and
// panic — and timestamps arrive from the network. Twitch's tmi-sent-ts is
// whatever the tag says, and a bogus value should misreport the message rate,
// not take the process down.
func bucket(sec int64) int64 {
	b := sec % 60
	if b < 0 {
		b += 60
	}
	return b
}

// perMinute is the number of messages seen in the last 60 seconds.
func (r *rate) perMinute(now time.Time) int {
	r.advance(now.Unix())
	sum := 0
	for _, b := range r.buckets {
		sum += b
	}
	return sum
}
