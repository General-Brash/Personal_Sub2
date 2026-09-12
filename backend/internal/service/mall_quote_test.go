//go:build unit

package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestValidateMallExpectedQuoteVersion_ConflictIsExplicitAndLegacyAllowed(t *testing.T) {
	require.NoError(t, validateMallExpectedQuoteVersion("", "sha256:new"))
	require.NoError(t, validateMallExpectedQuoteVersion("sha256:same", "sha256:same"))
	err := validateMallExpectedQuoteVersion("sha256:old", "sha256:new")
	require.ErrorIs(t, err, ErrMallQuoteChanged)
}

func TestMallQuoteVersionChangesWithPriceAndIsStableOtherwise(t *testing.T) {
	product := &mallCurrencyProduct{id: 7, name: "starter", price: 10, creditedAmount: 10, dailyLimit: 1, totalLimit: 2, updatedAt: time.Unix(1, 0).UTC()}
	first := mallCurrencyProductQuoteVersion(product)
	second := mallCurrencyProductQuoteVersion(product)
	require.Equal(t, first, second)
	product.price = 11
	require.NotEqual(t, first, mallCurrencyProductQuoteVersion(product))
}
