package service

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateRateMultiplier(t *testing.T) {
	tests := []struct {
		name      string
		value     float64
		allowZero bool
		wantErr   bool
	}{
		{name: "minimum positive value", value: MinimumRateMultiplier},
		{name: "small positive value", value: 0.0001},
		{name: "zero allowed for free mode", value: 0, allowZero: true},
		{name: "zero rejected for base rate", value: 0, wantErr: true},
		{name: "below minimum", value: 0.0000001, wantErr: true},
		{name: "negative", value: -1, allowZero: true, wantErr: true},
		{name: "nan", value: math.NaN(), wantErr: true},
		{name: "infinity", value: math.Inf(1), allowZero: true, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateRateMultiplier(test.value, test.allowZero)
			if test.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
