package domain_test

import (
	"errors"
	"testing"

	"fxrates/internal/domain"
)

func TestParseRate(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    domain.Rate
		wantErr error
	}{
		{name: "integer", input: "1", want: "1"},
		{name: "decimal", input: "19.909", want: "19.909"},
		{name: "maximum precision", input: "123456789012345678.123456789012", want: "123456789012345678.123456789012"},
		{name: "leading zeros do not consume integer precision", input: "0000000000000000001.2", want: "0000000000000000001.2"},
		{name: "smallest supported positive value", input: "0.000000000001", want: "0.000000000001"},
		{name: "rejects zero", input: "0", wantErr: domain.ErrInvalidRate},
		{name: "rejects decimal zero", input: "0.000", wantErr: domain.ErrInvalidRate},
		{name: "rejects negative value", input: "-1", wantErr: domain.ErrInvalidRate},
		{name: "rejects exponent", input: "1e3", wantErr: domain.ErrInvalidRate},
		{name: "rejects too many integer digits", input: "1234567890123456789", wantErr: domain.ErrInvalidRate},
		{name: "rejects too many fractional digits", input: "1.1234567890123", wantErr: domain.ErrInvalidRate},
		{name: "rejects missing integer part", input: ".1", wantErr: domain.ErrInvalidRate},
		{name: "rejects missing fractional part", input: "1.", wantErr: domain.ErrInvalidRate},
		{name: "rejects empty value", input: "", wantErr: domain.ErrInvalidRate},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := domain.ParseRate(test.input)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("ParseRate(%q) error = %v, want %v", test.input, err, test.wantErr)
			}
			if got != test.want {
				t.Errorf("ParseRate(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}
