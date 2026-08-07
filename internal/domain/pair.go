package domain

import (
	"errors"
	"strings"
)

var (
	ErrInvalidPair         = errors.New("currency pair must have format BASE/QUOTE with different currencies")
	ErrUnsupportedCurrency = errors.New("currency pair contains an unsupported currency")
)

var supportedCurrencies = map[string]struct{}{
	"EUR": {},
	"MXN": {},
	"USD": {},
}

// ParsePair validates and normalizes a currency pair.
func ParsePair(value string) (Pair, error) {
	value = strings.ToUpper(strings.TrimSpace(value))
	parts := strings.Split(value, "/")
	if len(parts) != 2 || !validCurrencyCode(parts[0]) || !validCurrencyCode(parts[1]) || parts[0] == parts[1] {
		return "", ErrInvalidPair
	}
	if !supportedCurrency(parts[0]) || !supportedCurrency(parts[1]) {
		return "", ErrUnsupportedCurrency
	}

	return Pair(parts[0] + "/" + parts[1]), nil
}

func supportedCurrency(value string) bool {
	_, ok := supportedCurrencies[value]
	return ok
}

func validCurrencyCode(value string) bool {
	if len(value) != 3 {
		return false
	}

	for _, char := range value {
		if char < 'A' || char > 'Z' {
			return false
		}
	}

	return true
}
