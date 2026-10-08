//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type plazaPricingSettingsRepo struct {
	service.SettingRepository
	mu     sync.Mutex
	values map[string]string
	writes int
}

func (r *plazaPricingSettingsRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[string]string{}
	for _, key := range keys {
		out[key] = r.values[key]
	}
	return out, nil
}
func (r *plazaPricingSettingsRepo) CompareAndSetMultiple(_ context.Context, expected, updates map[string]string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, value := range expected {
		if r.values[key] != value {
			return false, nil
		}
	}
	for key, value := range updates {
		r.values[key] = value
	}
	r.writes++
	return true, nil
}
func plazaPricingRouter(t *testing.T) (*gin.Engine, *plazaPricingSettingsRepo) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	repo := &plazaPricingSettingsRepo{values: map[string]string{}}
	defaults := service.NewDefaultModelPricingService(repo)
	require.NoError(t, defaults.Refresh(context.Background()))
	billing, err := service.ProvideBillingService(&config.Config{}, nil, defaults)
	require.NoError(t, err)
	h := NewModelPlazaPricingHandler(defaults, billing)
	r := gin.New()
	r.GET("/pricing", h.Get)
	r.PUT("/pricing", h.Put)
	r.POST("/pricing/reset", h.Reset)
	r.GET("/pricing/overrides", h.List)
	return r, repo
}
func plazaPricingRequest(r *gin.Engine, method, url, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}
func plazaPricingDetail(t *testing.T, w *httptest.ResponseRecorder) service.ModelDefaultPricingDetail {
	t.Helper()
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var response struct {
		Data service.ModelDefaultPricingDetail `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	return response.Data
}

func TestModelPlazaPricingCRUDVersionResetAndIndependentListing(t *testing.T) {
	r, repo := plazaPricingRouter(t)
	detail := plazaPricingDetail(t, plazaPricingRequest(r, "GET", "/pricing?model_id=Vendor%2Fnew.v1%3A1", ""))
	require.False(t, detail.EffectivePricingAvailable)
	require.Equal(t, "0", detail.Version)
	detail = plazaPricingDetail(t, plazaPricingRequest(r, "PUT", "/pricing", `{"model_id":"Vendor/new.v1:1","version":"0","patch":{"billing_mode":"token","input_price":0.000002,"output_price":0.000008}}`))
	require.Equal(t, "vendor/new.v1:1", detail.PricingKey)
	require.Equal(t, "1", detail.Version)
	require.Equal(t, "1", detail.LoadedVersion)
	require.Equal(t, 2e-6, *detail.EffectivePricing.InputPrice)
	require.True(t, detail.HasAdminOverride)
	conflict := plazaPricingRequest(r, "PUT", "/pricing", `{"model_id":"different","version":"0","patch":{"billing_mode":"token","input_price":1,"output_price":1}}`)
	require.Equal(t, http.StatusConflict, conflict.Code)
	require.Contains(t, conflict.Body.String(), "MODEL_DEFAULT_PRICING_VERSION_CONFLICT")
	reset := plazaPricingRequest(r, "POST", "/pricing/reset", `{"model_id":"Vendor/new.v1:1","version":"1"}`)
	require.Equal(t, http.StatusConflict, reset.Code)
	require.Contains(t, reset.Body.String(), "MODEL_DEFAULT_PRICING_RESET_UNAVAILABLE")
	list := plazaPricingRequest(r, "GET", "/pricing/overrides?search=vendor%2F&page=1", "")
	require.Equal(t, http.StatusOK, list.Code)
	require.Contains(t, list.Body.String(), "vendor/new.v1:1")
	require.Equal(t, 1, repo.writes)
	// It is stored independently of the model-plaza display document.
	require.NotContains(t, repo.values, service.SettingKeyModelPlazaOverrides)
}

func TestModelPlazaPricingRejectsUnknownFieldsUnitsAndOversizedBodies(t *testing.T) {
	r, repo := plazaPricingRouter(t)
	bodies := []string{
		`{"model_id":"new","version":"0","unit":"USD/million","patch":{"billing_mode":"token","input_price":1,"output_price":1}}`,
		`{"model_id":"new","version":"0","patch":{"billing_mode":"token","input_price":1,"output_price":1,"path":"config.yaml"}}`,
		`{"model_id":"new","version":"0","patch":{"billing_mode":"video","per_request_price":1}}`,
		`{"model_id":"new","version":"0","patch":{"billing_mode":"token","input_price":1e309,"output_price":1}}`,
		`{"model_id":"new","version":"0","patch":{"billing_mode":"token","input_price":1}}`,
		`{"model_id":"new","version":"0","patch":{"billing_mode":"token","input_price":0,"output_price":0}} {}`,
		`{"model_id":"` + strings.Repeat("a", 17000) + `","version":"0","patch":{}}`,
	}
	for _, body := range bodies {
		w := plazaPricingRequest(r, "PUT", "/pricing", body)
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		require.Zero(t, repo.writes)
	}
}

func TestModelPlazaPricingExistingFallbackCanBePartiallyUpdatedAndReset(t *testing.T) {
	r, _ := plazaPricingRouter(t)
	original := plazaPricingDetail(t, plazaPricingRequest(r, "GET", "/pricing?model_id=claude-sonnet-4", ""))
	saved := plazaPricingDetail(t, plazaPricingRequest(r, "PUT", "/pricing", `{"model_id":"claude-sonnet-4","version":"0","patch":{"input_price":0}}`))
	require.Zero(t, *saved.EffectivePricing.InputPrice)
	require.Equal(t, original.EffectivePricing.OutputPrice, saved.EffectivePricing.OutputPrice)
	restored := plazaPricingDetail(t, plazaPricingRequest(r, "POST", "/pricing/reset", `{"model_id":"claude-sonnet-4","version":"1"}`))
	require.False(t, restored.HasAdminOverride)
	require.Equal(t, original.EffectivePricing.InputPrice, restored.EffectivePricing.InputPrice)
}
