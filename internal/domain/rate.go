package domain

import (
	"errors"
	"strings"
)

const (
	maxRateIntegerDigits    = 18
	maxRateFractionalDigits = 12
)

// ErrInvalidRate reports a rate that cannot be stored exactly by the service.
var ErrInvalidRate = errors.New("rate must be a positive decimal with at most 18 integer digits and 12 fractional digits")

// Rate is a positive decimal that fits PostgreSQL numeric(30, 12).
type Rate string

// ParseRate validates a decimal that can be stored exactly as numeric(30, 12).
func ParseRate(value string) (Rate, error) {
	integer, fraction, hasFraction := strings.Cut(value, ".")
	if integer == "" || (hasFraction && fraction == "") {
		return "", ErrInvalidRate
	}
	if !decimalDigits(integer) || (hasFraction && !decimalDigits(fraction)) {
		return "", ErrInvalidRate
	}

	significantInteger := strings.TrimLeft(integer, "0")
	if len(significantInteger) > maxRateIntegerDigits || len(fraction) > maxRateFractionalDigits {
		return "", ErrInvalidRate
	}
	if significantInteger == "" && strings.Trim(fraction, "0") == "" {
		return "", ErrInvalidRate
	}

	return Rate(value), nil
}

func decimalDigits(value string) bool {
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}

	return true
}
