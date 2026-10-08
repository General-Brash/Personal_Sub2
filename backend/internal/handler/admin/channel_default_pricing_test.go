//go:build unit

package admin

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type defaultPricingQueryRepo struct {
	service.SettingRepository
	values map[string]string
}

func (r *defaultPricingQueryRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	out := map[string]string{}
	for _, key := range keys {
		out[key] = r.values[key]
	}
	return out, nil
}
func (r *defaultPricingQueryRepo) CompareAndSetMultiple(_ context.Context, expected, updates map[string]string) (bool, error) {
	for key, value := range expected {
		if r.values[key] != value {
			return false, nil
		}
	}
	for key, value := range updates {
		r.values[key] = value
	}
	return true, nil
}

func TestDefaultPricingQueryImageRatesDistinguishMissingFromExplicitZero(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, explicitZero := range []bool{false, true} {
		t.Run(map[bool]string{false: "inherited", true: "explicit zero"}[explicitZero], func(t *testing.T) {
			billing := service.NewBillingService(&config.Config{}, nil)
			model := "claude-sonnet-4"
			if explicitZero {
				defaults := service.NewDefaultModelPricingService(&defaultPricingQueryRepo{values: map[string]string{}})
				require.NoError(t, defaults.Refresh(context.Background()))
				var err error
				billing, err = service.ProvideBillingService(&config.Config{}, nil, defaults)
				require.NoError(t, err)
				model = "custom-image-zero"
				patch := service.DefaultPricingPatch{}
				require.NoError(t, json.Unmarshal([]byte(`{"billing_mode":"token","input_price":0.000002,"output_price":0.000008,"image_input_price":0,"image_output_price":0}`), &patch))
				_, _, err = billing.SaveDefaultPricing(context.Background(), model, "0", patch, false)
				require.NoError(t, err)
			}
			h := &ChannelHandler{billingService: billing}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/model-pricing?model="+model, nil)
			h.GetModelDefaultPricing(c)
			require.Equal(t, 200, w.Code)
			var body struct {
				Data struct {
					Found       bool    `json:"found"`
					Input       float64 `json:"input_price"`
					Output      float64 `json:"output_price"`
					ImageInput  float64 `json:"image_input_price"`
					ImageOutput float64 `json:"image_output_price"`
				} `json:"data"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			require.True(t, body.Data.Found)
			if explicitZero {
				require.Zero(t, body.Data.ImageInput)
				require.Zero(t, body.Data.ImageOutput)
			} else {
				require.Equal(t, body.Data.Input, body.Data.ImageInput)
				require.Equal(t, body.Data.Output, body.Data.ImageOutput)
			}
		})
	}
}
