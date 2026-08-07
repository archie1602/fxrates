package domain_test

import (
	"errors"
	"testing"

	"fxrates/internal/domain"
)

func TestParsePair(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    domain.Pair
		wantErr error
	}{
		{
			name:  "normalizes supported pair",
			input: "  eur/mxn  ",
			want:  "EUR/MXN",
		},
		{
			name:    "requires both currencies",
			input:   "EUR",
			wantErr: domain.ErrInvalidPair,
		},
		{
			name:    "requires three-letter currency codes",
			input:   "EU/MXN",
			wantErr: domain.ErrInvalidPair,
		},
		{
			name:    "requires different currencies",
			input:   "USD/USD",
			wantErr: domain.ErrInvalidPair,
		},
		{
			name:    "rejects unsupported currency",
			input:   "EUR/GBP",
			wantErr: domain.ErrUnsupportedCurrency,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := domain.ParsePair(test.input)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("ParsePair(%q) error = %v, want %v", test.input, err, test.wantErr)
			}
			if got != test.want {
				t.Errorf("ParsePair(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}
