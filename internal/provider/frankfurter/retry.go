package frankfurter

import (
	"fmt"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func calculateRetryDelay(
	policy RetryPolicy,
	attempt int,
	retryAfterHeader string,
	now time.Time,
) (time.Duration, error) {
	backoffLimit := exponentialBackoffLimit(policy, attempt)
	delay := time.Duration(0)
	if backoffLimit > 0 {
		delay = rand.N(backoffLimit)
	}

	retryAfter, found := parseRetryAfter(retryAfterHeader, now)
	if !found {
		return delay, nil
	}
	if retryAfter > policy.MaxDelay {
		return 0, fmt.Errorf(
			"Retry-After delay %s exceeds configured maximum %s",
			retryAfter,
			policy.MaxDelay,
		)
	}
	if retryAfter > delay {
		return retryAfter, nil
	}

	return delay, nil
}

func exponentialBackoffLimit(policy RetryPolicy, attempt int) time.Duration {
	if attempt <= 0 || policy.InitialDelay <= 0 {
		return 0
	}

	delay := policy.InitialDelay
	for currentAttempt := 1; currentAttempt < attempt; currentAttempt++ {
		if delay >= policy.MaxDelay || delay > policy.MaxDelay/2 {
			return policy.MaxDelay
		}
		delay *= 2
	}
	if delay > policy.MaxDelay {
		return policy.MaxDelay
	}

	return delay
}

func parseRetryAfter(value string, now time.Time) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}

	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds < 0 {
			return 0, false
		}
		const maxDuration = time.Duration(1<<63 - 1)
		if seconds > int64(maxDuration/time.Second) {
			return maxDuration, true
		}
		return time.Duration(seconds) * time.Second, true
	}

	retryAt, err := http.ParseTime(value)
	if err != nil {
		return 0, false
	}
	delay := retryAt.Sub(now)
	if delay < 0 {
		return 0, true
	}

	return delay, true
}
