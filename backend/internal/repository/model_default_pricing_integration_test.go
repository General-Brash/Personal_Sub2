//go:build integration

package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDefaultModelPricingCompareAndSetConcurrentInitialization(t *testing.T) {
	ctx := context.Background()
	repo := NewSettingRepository(testEntClient(t)).(*settingRepository)
	key := fmt.Sprintf("test_default_model_pricing_cas_%d", time.Now().UnixNano())
	t.Cleanup(func() { require.NoError(t, repo.Delete(ctx, key)) })
	start := make(chan struct{})
	type result struct {
		ok  bool
		err error
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for i := 1; i <= 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			ok, err := repo.CompareAndSetMultiple(ctx, map[string]string{key: ""}, map[string]string{key: fmt.Sprintf(`{"revision":1,"winner":%d}`, i)})
			results <- result{ok, err}
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	winners := 0
	for result := range results {
		require.NoError(t, result.err)
		if result.ok {
			winners++
		}
	}
	require.Equal(t, 1, winners)
	value, err := repo.GetValue(ctx, key)
	require.NoError(t, err)
	require.Contains(t, value, `"revision":1`)
	ok, err := repo.CompareAndSetMultiple(ctx, map[string]string{key: ""}, map[string]string{key: "lost"})
	require.NoError(t, err)
	require.False(t, ok)
	same, err := repo.GetValue(ctx, key)
	require.NoError(t, err)
	require.Equal(t, value, same)
}

func TestDefaultModelPricingCompareAndSetRollbackKeepsWholeBundle(t *testing.T) {
	ctx := context.Background()
	repo := NewSettingRepository(testEntClient(t)).(*settingRepository)
	first := fmt.Sprintf("test_default_pricing_a_%d", time.Now().UnixNano())
	second := first + "_z"
	t.Cleanup(func() { require.NoError(t, repo.Delete(ctx, first)); require.NoError(t, repo.Delete(ctx, second)) })
	require.NoError(t, repo.Set(ctx, second, "revision-2"))
	ok, err := repo.CompareAndSetMultiple(ctx, map[string]string{first: "", second: "stale"}, map[string]string{first: "new", second: "new"})
	require.NoError(t, err)
	require.False(t, ok)
	values, err := repo.GetMultiple(ctx, []string{first, second})
	require.NoError(t, err)
	require.NotContains(t, values, first)
	require.Equal(t, "revision-2", values[second])
}
