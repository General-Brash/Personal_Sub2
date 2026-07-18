package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const missingPricingHandlerTestModel = "pricing-unknown-model-e2e"

func newMissingPricingOpenAIHandler(
	t *testing.T,
	account service.Account,
	upstream service.HTTPUpstream,
	configure func(*config.Config),
) (*OpenAIGatewayHandler, *service.APIKey) {
	return newPricingOpenAIHandler(t, []service.Account{account}, upstream, nil, configure)
}

func newPricingOpenAIHandler(
	t *testing.T,
	accounts []service.Account,
	upstream service.HTTPUpstream,
	channelService *service.ChannelService,
	configure func(*config.Config),
) (*OpenAIGatewayHandler, *service.APIKey) {
	t.Helper()

	groupID := int64(4301)
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Default.RateMultiplier = 1
	cfg.Security.URLAllowlist.Enabled = false
	if configure != nil {
		configure(cfg)
	}

	accountRepo := &openAIWSFailoverHandlerAccountRepoStub{accounts: accounts}
	billingCache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingCache.Stop)
	gateway := service.NewOpenAIGatewayService(
		accountRepo,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		cfg,
		nil,
		nil,
		service.NewBillingService(cfg, nil),
		nil,
		billingCache,
		upstream,
		&service.DeferredService{},
		nil,
		nil,
		nil,
		channelService,
		nil,
		nil,
		nil,
	)
	apiKey := &service.APIKey{
		ID:      1901,
		GroupID: &groupID,
		User:    &service.User{ID: 1801, Status: service.StatusActive},
		Group:   &service.Group{ID: groupID, Platform: service.PlatformOpenAI, Status: service.StatusActive},
	}
	cache := &concurrencyCacheMock{
		acquireUserSlotFn: func(context.Context, int64, int, string) (bool, error) {
			return true, nil
		},
		acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) {
			return true, nil
		},
	}
	handler := &OpenAIGatewayHandler{
		gatewayService:      gateway,
		billingCacheService: billingCache,
		apiKeyService:       &service.APIKeyService{},
		concurrencyHelper:   NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, time.Second),
		maxAccountSwitches:  cfg.Gateway.MaxAccountSwitches,
	}
	return handler, apiKey
}

type successfulEmbeddingsPricingUpstream struct {
	callCount atomic.Int64
	accountID atomic.Int64
}

type successfulResponsesPricingUpstream struct {
	callCount atomic.Int64
	accountID atomic.Int64
}

func (u *successfulResponsesPricingUpstream) Do(_ *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	u.callCount.Add(1)
	u.accountID.Store(accountID)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(
			`{"id":"resp_pricing_failover","object":"response","model":"gpt-5.1","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`,
		)),
	}, nil
}

func (u *successfulResponsesPricingUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func (u *successfulEmbeddingsPricingUpstream) Do(_ *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	u.callCount.Add(1)
	u.accountID.Store(accountID)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(
			`{"object":"list","data":[],"model":"gpt-5.1","usage":{"prompt_tokens":1,"total_tokens":1}}`,
		)),
	}, nil
}

func (u *successfulEmbeddingsPricingUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func missingPricingOpenAIAccount(baseURL string) service.Account {
	return service.Account{
		ID:          9920,
		Name:        "missing-pricing-openai",
		Platform:    service.PlatformOpenAI,
		Type:        service.AccountTypeAPIKey,
		Status:      service.StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "sk-pricing-test",
			"base_url": baseURL,
		},
		Extra: map[string]any{"openai_passthrough": true},
	}
}

func TestOpenAIResponsesMissingPricingRejectsBeforeHTTPUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &openAIHTTPPassthroughFailoverUpstream{}
	h, apiKey := newMissingPricingOpenAIHandler(t, missingPricingOpenAIAccount("https://api.example.test"), upstream, nil)

	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "non_stream", true: "stream"}[stream], func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			body := `{"model":"` + missingPricingHandlerTestModel + `","input":"hello","stream":` + map[bool]string{false: "false", true: "true"}[stream] + `}`
			c.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/responses", strings.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set(string(middleware.ContextKeyAPIKey), apiKey)
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.User.ID, Concurrency: 1})

			h.Responses(c)

			require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
			require.Equal(t, "BILLING_PRICING_UNAVAILABLE", gjson.GetBytes(recorder.Body.Bytes(), "error.type").String())
			require.NotContains(t, recorder.Body.String(), "event:", "stream requests must fail before the first SSE frame")
			require.Empty(t, upstream.calls(), "pricing failure must prevent every upstream HTTP attempt")
		})
	}
}

func TestOpenAIEmbeddingsMissingPricingRejectsBeforeHTTPUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &openAIHTTPPassthroughFailoverUpstream{}
	h, apiKey := newMissingPricingOpenAIHandler(t, missingPricingOpenAIAccount("https://api.example.test"), upstream, nil)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/embeddings", strings.NewReader(
		`{"model":"`+missingPricingHandlerTestModel+`","input":"hello"}`,
	))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(string(middleware.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.User.ID, Concurrency: 1})

	h.Embeddings(c)

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Equal(t, "BILLING_PRICING_UNAVAILABLE", gjson.GetBytes(recorder.Body.Bytes(), "error.type").String())
	require.Empty(t, upstream.calls(), "pricing failure must prevent every embeddings upstream attempt")
}

func TestOpenAIEmbeddingsPricingUnavailableSwitchesToPricedAccount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	first := missingPricingOpenAIAccount("https://api.example.test")
	first.ID = 9921
	first.Priority = 1
	first.Credentials["model_mapping"] = map[string]any{"gpt-5.1": missingPricingHandlerTestModel}
	second := missingPricingOpenAIAccount("https://api.example.test")
	second.ID = 9922
	second.Priority = 2

	groupID := int64(4301)
	channelService := service.NewChannelService(&openAIWSUsageHandlerChannelRepoStub{
		channels: []service.Channel{{
			ID:                 7702,
			Name:               "openai-embeddings-pricing-failover",
			Status:             service.StatusActive,
			GroupIDs:           []int64{groupID},
			BillingModelSource: service.BillingModelSourceUpstream,
		}},
		groupPlatforms: map[int64]string{groupID: service.PlatformOpenAI},
	}, nil, nil, nil)
	upstream := &successfulEmbeddingsPricingUpstream{}
	h, apiKey := newPricingOpenAIHandler(t, []service.Account{first, second}, upstream, channelService, func(cfg *config.Config) {
		cfg.Gateway.MaxAccountSwitches = 3
	})
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/embeddings", strings.NewReader(
		`{"model":"gpt-5.1","input":"hello"}`,
	))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(string(middleware.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.User.ID, Concurrency: 1})

	h.Embeddings(c)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, int64(1), upstream.callCount.Load())
	require.Equal(t, second.ID, upstream.accountID.Load(), "the unpriced account must be excluded before forwarding")
}

func TestOpenAIResponsesPricingUnavailableSwitchesToPricedAccount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	first := missingPricingOpenAIAccount("https://api.example.test")
	first.ID = 9923
	first.Priority = 1
	first.Credentials["model_mapping"] = map[string]any{"gpt-5.1": missingPricingHandlerTestModel}
	second := missingPricingOpenAIAccount("https://api.example.test")
	second.ID = 9924
	second.Priority = 2

	groupID := int64(4301)
	channelService := service.NewChannelService(&openAIWSUsageHandlerChannelRepoStub{
		channels: []service.Channel{{
			ID:                 7703,
			Name:               "openai-responses-pricing-failover",
			Status:             service.StatusActive,
			GroupIDs:           []int64{groupID},
			BillingModelSource: service.BillingModelSourceUpstream,
		}},
		groupPlatforms: map[int64]string{groupID: service.PlatformOpenAI},
	}, nil, nil, nil)
	upstream := &successfulResponsesPricingUpstream{}
	h, apiKey := newPricingOpenAIHandler(t, []service.Account{first, second}, upstream, channelService, func(cfg *config.Config) {
		cfg.Gateway.MaxAccountSwitches = 3
	})
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/responses", strings.NewReader(
		`{"model":"gpt-5.1","input":"hello","stream":false}`,
	))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(string(middleware.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.User.ID, Concurrency: 1})

	h.Responses(c)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, int64(1), upstream.callCount.Load())
	require.Equal(t, second.ID, upstream.accountID.Load(), "the unpriced account must be excluded before forwarding")
}

func TestGatewayMessagesFallbackAttemptUsesCurrentChannelMappingEverywhere(t *testing.T) {
	source, err := os.ReadFile("gateway_handler.go")
	require.NoError(t, err)
	text := string(source)
	start := strings.Index(text, "attemptPricingMapping, _ :=")
	require.NotEqual(t, -1, start)
	end := strings.Index(text[start:], "if !retryWithFallback")
	require.NotEqual(t, -1, end)
	attemptLoop := text[start : start+end]

	require.Contains(t, attemptLoop, "PreflightTokenRequestPricing(c.Request.Context(), currentAPIKey, account, reqModel, attemptPricingMapping)")
	require.Contains(t, attemptLoop, "if attemptPricingMapping.Mapped")
	require.Contains(t, attemptLoop, "attemptPricingMapping.ToUsageFields(reqModel, result.UpstreamModel)")
	require.NotContains(t, attemptLoop, "if channelMapping.Mapped")
	require.NotContains(t, attemptLoop, "channelMapping.ToUsageFields(reqModel, result.UpstreamModel)")
}

func TestOpenAIResponsesWebSocketMissingPricingReturnsTypedErrorBeforeUpstreamDial(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var upstreamConnections atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamConnections.Add(1)
		http.Error(w, "unexpected upstream connection", http.StatusInternalServerError)
	}))
	defer upstream.Close()

	account := missingPricingOpenAIAccount(upstream.URL)
	account.Extra = map[string]any{
		"openai_passthrough":                            true,
		"openai_apikey_responses_websockets_v2_enabled": true,
		"openai_apikey_responses_websockets_v2_mode":    service.OpenAIWSIngressModePassthrough,
	}
	h, apiKey := newMissingPricingOpenAIHandler(t, account, nil, func(cfg *config.Config) {
		cfg.Security.URLAllowlist.AllowInsecureHTTP = true
		cfg.Gateway.OpenAIWS.Enabled = true
		cfg.Gateway.OpenAIWS.APIKeyEnabled = true
		cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
		cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
		cfg.Gateway.OpenAIWS.DialTimeoutSeconds = 1
		cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 1
		cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 1
	})

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyAPIKey), apiKey)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.User.ID, Concurrency: 1})
		c.Next()
	})
	router.GET("/openai/v1/responses", h.ResponsesWebSocket)
	handlerServer := httptest.NewServer(router)
	defer handlerServer.Close()

	dialCtx, cancelDial := context.WithTimeout(context.Background(), 3*time.Second)
	client, _, err := coderws.Dial(dialCtx, "ws"+strings.TrimPrefix(handlerServer.URL, "http")+"/openai/v1/responses", nil)
	cancelDial()
	require.NoError(t, err)
	defer func() { _ = client.CloseNow() }()

	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 3*time.Second)
	err = client.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"`+missingPricingHandlerTestModel+`","stream":true}`))
	cancelWrite()
	require.NoError(t, err)

	readCtx, cancelRead := context.WithTimeout(context.Background(), 3*time.Second)
	_, event, err := client.Read(readCtx)
	cancelRead()
	require.NoError(t, err)
	require.Equal(t, "error", gjson.GetBytes(event, "type").String())
	require.Equal(t, "BILLING_PRICING_UNAVAILABLE", gjson.GetBytes(event, "error.type").String())
	require.Equal(t, "BILLING_PRICING_UNAVAILABLE", gjson.GetBytes(event, "error.code").String())
	require.Zero(t, upstreamConnections.Load(), "pricing failure must happen before dialing the upstream websocket")
}

func TestOpenAIResponsesWebSocketFollowupMissingPricingRejectsBeforeUpstreamWrite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var upstreamFrames atomic.Int64
	firstUpstreamFrame := make(chan []byte, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, &coderws.AcceptOptions{CompressionMode: coderws.CompressionContextTakeover})
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()

		for {
			readCtx, cancelRead := context.WithTimeout(r.Context(), 5*time.Second)
			_, payload, readErr := conn.Read(readCtx)
			cancelRead()
			if readErr != nil {
				return
			}
			if upstreamFrames.Add(1) == 1 {
				firstUpstreamFrame <- payload
				writeCtx, cancelWrite := context.WithTimeout(r.Context(), 3*time.Second)
				_ = conn.Write(writeCtx, coderws.MessageText, []byte(
					`{"type":"response.completed","response":{"id":"resp_pricing_turn_1","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`,
				))
				cancelWrite()
			}
		}
	}))
	defer upstream.Close()

	account := missingPricingOpenAIAccount(upstream.URL)
	account.Extra = map[string]any{
		"openai_passthrough":                            true,
		"openai_apikey_responses_websockets_v2_enabled": true,
		"openai_apikey_responses_websockets_v2_mode":    service.OpenAIWSIngressModePassthrough,
	}
	h, apiKey := newMissingPricingOpenAIHandler(t, account, nil, func(cfg *config.Config) {
		cfg.Security.URLAllowlist.AllowInsecureHTTP = true
		cfg.Gateway.OpenAIWS.Enabled = true
		cfg.Gateway.OpenAIWS.APIKeyEnabled = true
		cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
		cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
		cfg.Gateway.OpenAIWS.DialTimeoutSeconds = 3
		cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 3
		cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3
	})

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyAPIKey), apiKey)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.User.ID, Concurrency: 1})
		c.Next()
	})
	router.GET("/openai/v1/responses", h.ResponsesWebSocket)
	handlerServer := httptest.NewServer(router)
	defer handlerServer.Close()

	dialCtx, cancelDial := context.WithTimeout(context.Background(), 3*time.Second)
	client, _, err := coderws.Dial(dialCtx, "ws"+strings.TrimPrefix(handlerServer.URL, "http")+"/openai/v1/responses", nil)
	cancelDial()
	require.NoError(t, err)
	defer func() { _ = client.CloseNow() }()

	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 3*time.Second)
	err = client.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","stream":false}`))
	cancelWrite()
	require.NoError(t, err)

	readCtx, cancelRead := context.WithTimeout(context.Background(), 3*time.Second)
	_, firstEvent, err := client.Read(readCtx)
	cancelRead()
	require.NoError(t, err)
	require.Equal(t, "response.completed", gjson.GetBytes(firstEvent, "type").String())

	select {
	case payload := <-firstUpstreamFrame:
		require.Equal(t, "gpt-5.1", gjson.GetBytes(payload, "model").String())
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for the first upstream websocket frame")
	}

	writeCtx, cancelWrite = context.WithTimeout(context.Background(), 3*time.Second)
	err = client.Write(writeCtx, coderws.MessageText, []byte(
		`{"type":"response.create","model":"`+missingPricingHandlerTestModel+`","stream":false}`,
	))
	cancelWrite()
	require.NoError(t, err)

	readCtx, cancelRead = context.WithTimeout(context.Background(), 3*time.Second)
	_, pricingEvent, err := client.Read(readCtx)
	cancelRead()
	require.NoError(t, err)
	require.Equal(t, "error", gjson.GetBytes(pricingEvent, "type").String())
	require.Equal(t, "BILLING_PRICING_UNAVAILABLE", gjson.GetBytes(pricingEvent, "error.type").String())
	require.Equal(t, "BILLING_PRICING_UNAVAILABLE", gjson.GetBytes(pricingEvent, "error.code").String())
	require.Never(t, func() bool { return upstreamFrames.Load() > 1 }, 300*time.Millisecond, 20*time.Millisecond,
		"the follow-up frame must be rejected before the upstream websocket write")
}
