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
	r.buckets[sec%60]++
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
