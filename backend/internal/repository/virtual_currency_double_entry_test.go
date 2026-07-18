package repository

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestBuildVirtualCurrencyPostingsBalancesEveryMutationKind(t *testing.T) {
	const userID int64 = 42
	tests := []struct {
		name     string
		input    service.VirtualCurrencyDeltaInput
		expected map[string]int64
	}{
		{
			name:     "grant issues units",
			input:    service.VirtualCurrencyDeltaInput{UserID: userID, DeltaUnits: 100, AvailableDeltaUnits: 100, EntryType: service.VirtualCurrencyEntryGrant},
			expected: map[string]int64{virtualCurrencyAccountUserAvailable: 100, virtualCurrencyAccountSystemIssuance: -100},
		},
		{
			name:     "spend moves units into sink",
			input:    service.VirtualCurrencyDeltaInput{UserID: userID, DeltaUnits: -25, AvailableDeltaUnits: -25, EntryType: service.VirtualCurrencyEntrySpend},
			expected: map[string]int64{virtualCurrencyAccountUserAvailable: -25, virtualCurrencyAccountSystemSink: 25},
		},
		{
			name:     "refund reverses sink",
			input:    service.VirtualCurrencyDeltaInput{UserID: userID, DeltaUnits: 10, AvailableDeltaUnits: 10, EntryType: service.VirtualCurrencyEntryRefund},
			expected: map[string]int64{virtualCurrencyAccountUserAvailable: 10, virtualCurrencyAccountSystemSink: -10},
		},
		{
			name:     "negative adjustment uses adjustment account",
			input:    service.VirtualCurrencyDeltaInput{UserID: userID, DeltaUnits: -5, AvailableDeltaUnits: -5, EntryType: service.VirtualCurrencyEntryAdjustment},
			expected: map[string]int64{virtualCurrencyAccountUserAvailable: -5, virtualCurrencyAccountSystemAdjust: 5},
		},
		{
			name:     "reserve transfers available to reserved",
			input:    service.VirtualCurrencyDeltaInput{UserID: userID, AvailableDeltaUnits: -20, ReservedDeltaUnits: 20, EntryType: service.VirtualCurrencyEntryReserve},
			expected: map[string]int64{virtualCurrencyAccountUserAvailable: -20, virtualCurrencyAccountUserReserved: 20},
		},
		{
			name:     "commit removes reserved units",
			input:    service.VirtualCurrencyDeltaInput{UserID: userID, DeltaUnits: -20, ReservedDeltaUnits: -20, EntryType: service.VirtualCurrencyEntryCommit},
			expected: map[string]int64{virtualCurrencyAccountUserReserved: -20, virtualCurrencyAccountSystemSink: 20},
		},
		{
			name:     "release returns reserved units",
			input:    service.VirtualCurrencyDeltaInput{UserID: userID, AvailableDeltaUnits: 20, ReservedDeltaUnits: -20, EntryType: service.VirtualCurrencyEntryRelease},
			expected: map[string]int64{virtualCurrencyAccountUserAvailable: 20, virtualCurrencyAccountUserReserved: -20},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			postings := buildVirtualCurrencyPostings(tt.input)
			require.GreaterOrEqual(t, len(postings), 2)

			actual := make(map[string]int64, len(postings))
			var total int64
			for _, posting := range postings {
				require.NotZero(t, posting.amountUnits)
				actual[posting.accountKind] = posting.amountUnits
				total += posting.amountUnits
				if posting.accountKind == virtualCurrencyAccountUserAvailable || posting.accountKind == virtualCurrencyAccountUserReserved {
					require.Equal(t, userID, posting.userID)
				} else {
					require.Nil(t, posting.userID)
				}
			}

			require.Zero(t, total, "every journal must conserve units")
			require.Equal(t, tt.expected, actual)
		})
	}
}
