package service

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestAvailableCreditEligibilityAppliesMinimumReserveAfterTemporaryCredit(t *testing.T) {
	testCases := []struct {
		name      string
		permanent string
		temporary string
		wantErr   bool
	}{
		{name: "legacy permanent balance below reserve without temporary credit", permanent: "0.00500000", temporary: "0.00000000", wantErr: true},
		{name: "temporary credit fully covers reserve", permanent: "0.00000000", temporary: "0.01000000", wantErr: false},
		{name: "permanent balance covers only temporary shortfall", permanent: "0.00400000", temporary: "0.00600000", wantErr: false},
		{name: "combined credit remains below reserve", permanent: "0.00400000", temporary: "0.00500000", wantErr: true},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			permanent, err := strconv.ParseFloat(tt.permanent, 64)
			require.NoError(t, err)
			temporary, err := strconv.ParseFloat(tt.temporary, 64)
			require.NoError(t, err)
			cache := &availableCreditCacheStub{getErr: errors.New("cache miss")}
			userRepo := &availableCreditSnapshotRepoStub{snapshot: AvailableCreditSnapshot{
				PermanentBalance: permanent,
				TemporaryCredit:  temporary,
			}}
			cfg := &config.Config{}
			cfg.Billing.MinimumBalanceReserve = 0.01
			svc := NewBillingCacheService(cache, userRepo, nil, nil, nil, nil, cfg, nil)
			t.Cleanup(svc.Stop)

			err = svc.CheckAvailableCreditEligibility(context.Background(), 42)

			if tt.wantErr {
				require.ErrorIs(t, err, ErrInsufficientBalance)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
