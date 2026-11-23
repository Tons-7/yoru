package bt

import (
	"math"

	"golang.org/x/time/rate"
)

const defaultMinDownloadRateLimiterBurst = 1 << 20

func setRateLimiterBurstIfZero(l *rate.Limiter, def int) {
	if l != nil && l.Burst() == 0 {
		l.SetBurst(def)
	}
}

func setDefaultDownloadRateLimiterBurstIfZero(l *rate.Limiter) {
	setRateLimiterBurstIfZero(l, int(
		max(
			min(EffectiveDownloadRateLimit(l), math.MaxInt32),
			defaultMinDownloadRateLimiterBurst)))
}
