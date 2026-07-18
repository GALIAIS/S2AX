package service

import (
	"math"
)

// MinimumRateMultiplier is the smallest non-zero multiplier that can be
// persisted. The database keeps eight fractional digits so personal
// operators can offer very small, but still auditable, rates without using
// floating-point rounding as an implicit business rule.
const MinimumRateMultiplier = 0.000001

// ValidateRateMultiplier validates a billing multiplier at the service
// boundary. Zero is useful for explicitly free image/video or peak windows,
// while a base group/user override must remain positive to avoid an accidental
// free route caused by an empty form value.
func ValidateRateMultiplier(value float64, allowZero bool) error {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return errorsRateMultiplier("must be a finite number")
	}
	if allowZero && value == 0 {
		return nil
	}
	if value < MinimumRateMultiplier {
		if allowZero {
			return errorsRateMultiplier("must be 0 or at least 0.000001")
		}
		return errorsRateMultiplier("must be at least 0.000001")
	}
	return nil
}

type rateMultiplierError string

func (e rateMultiplierError) Error() string { return string(e) }

func errorsRateMultiplier(message string) error {
	return rateMultiplierError("rate multiplier " + message)
}
