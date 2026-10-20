package bt

import (
	"math"

	"golang.org/x/time/rate"
)

// 1 MiB, HTTP/2's default largest read frame, as webseeds share the download limiter (Windows sockets
// once buffered 64 KiB).
const defaultMinDownloadRateLimiterBurst = 1 << 20

// Sets rate limiter burst if it's set to zero which is used to request the default by our API.
func setRateLimiterBurstIfZero(l *rate.Limiter, def int) {
	// A sensible burst even under Inf, as the limit may be changed later by someone not choosing one.
	if l != nil && l.Burst() == 0 {
		// What if the limit is greater than what can be represented by int?
		l.SetBurst(def)
	}
}

// Sets rate limiter burst if it's set to zero which is used to request the default by our API.
func setDefaultDownloadRateLimiterBurstIfZero(l *rate.Limiter) {
	setRateLimiterBurstIfZero(l, int(
		max(
			min(EffectiveDownloadRateLimit(l), math.MaxInt),
			defaultMinDownloadRateLimiterBurst)))
}
