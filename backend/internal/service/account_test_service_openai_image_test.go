package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestAccountTestService_OpenAIImageOAuthHandlesOutputItemDoneFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/1/test", nil)

	upstream := &httpUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"text/event-stream"},
			},
			Body: io.NopCloser(strings.NewReader(
				"data: {\"type\":\"response.output_item.done\",\"item\":{\"id\":\"ig_123\",\"type\":\"image_generation_call\",\"result\":\"aGVsbG8=\",\"revised_prompt\":\"draw a cat\",\"output_format\":\"png\"}}\n\n" +
					"data: {\"type\":\"response.completed\",\"response\":{\"created_at\":1710000006,\"tool_usage\":{\"image_gen\":{\"images\":1}},\"output\":[]}}\n\n" +
					"data: [DONE]\n\n",
			)),
		},
	}
	svc := &AccountTestService{httpUpstream: upstream}
	account := &Account{
		ID:       53,
		Name:     "openai-oauth",
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "token-123",
		},
	}

	err := svc.testOpenAIImageOAuth(c, context.Background(), account, "gpt-image-2", "draw a cat")
	require.NoError(t, err)
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, HTTPUpstreamProfileOpenAI, HTTPUpstreamProfileFromContext(upstream.lastReq.Context()))
	require.Contains(t, rec.Body.String(), "Calling Codex /responses image tool")
	require.Contains(t, rec.Body.String(), "data:image/png;base64,aGVsbG8=")
	require.Contains(t, rec.Body.String(), "\"success\":true")
}

func TestAccountTestService_OpenAIImageAPIKeyUsesConfiguredV1BaseURL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/1/test", nil)

	upstream := &httpUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"application/json"},
			},
			Body: io.NopCloser(strings.NewReader(`{"data":[{"b64_json":"aGVsbG8=","revised_prompt":"draw a cat"}]}`)),
		},
	}
	svc := &AccountTestService{
		httpUpstream: upstream,
		cfg:          &config.Config{},
	}
	account := &Account{
		ID:       54,
		Name:     "openai-apikey",
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "test-api-key",
			"base_url": "https://image-upstream.example/v1",
		},
	}

	err := svc.testOpenAIImageAPIKey(c, context.Background(), account, "gpt-image-2", "draw a cat")
	require.NoError(t, err)
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, HTTPUpstreamProfileOpenAI, HTTPUpstreamProfileFromContext(upstream.lastReq.Context()))
	require.Equal(t, "https://image-upstream.example/v1/images/generations", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer test-api-key", upstream.lastReq.Header.Get("Authorization"))
	require.Contains(t, rec.Body.String(), "data:image/png;base64,aGVsbG8=")
	require.Contains(t, rec.Body.String(), "\"success\":true")
}

func TestAccountTestService_OpenAIImageNew25ModelsKeepRouteAndPayloadModel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	testCases := []struct {
		name         string
		modelID      string
		accountType  string
		baseURL      string
		accessToken  string
		apiKey       string
		upstreamResp *http.Response
		assertReq    func(*testing.T, *http.Request, []byte)
	}{
		{
			name:        "flare API key",
			modelID:     "gpt-image-2.5-flare",
			accountType: AccountTypeAPIKey,
			baseURL:     "https://image-upstream.example/v1",
			apiKey:      "test-api-key",
			upstreamResp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"data":[{"b64_json":"aGVsbG8=","revised_prompt":"draw a cat"}]}`)),
			},
			assertReq: func(t *testing.T, req *http.Request, body []byte) {
				t.Helper()
				require.Equal(t, "https://image-upstream.example/v1/images/generations", req.URL.String())
				require.Equal(t, "Bearer test-api-key", req.Header.Get("Authorization"))
				require.Equal(t, "gpt-image-2.5-flare", gjson.GetBytes(body, "model").String())
				require.Equal(t, "draw a cat", gjson.GetBytes(body, "prompt").String())
			},
		},
		{
			name:        "sunburst API key",
			modelID:     "gpt-image-2.5-sunburst",
			accountType: AccountTypeAPIKey,
			baseURL:     "https://image-upstream.example/v1",
			apiKey:      "test-api-key",
			upstreamResp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"data":[{"b64_json":"aGVsbG8=","revised_prompt":"draw a cat"}]}`)),
			},
			assertReq: func(t *testing.T, req *http.Request, body []byte) {
				t.Helper()
				require.Equal(t, "https://image-upstream.example/v1/images/generations", req.URL.String())
				require.Equal(t, "Bearer test-api-key", req.Header.Get("Authorization"))
				require.Equal(t, "gpt-image-2.5-sunburst", gjson.GetBytes(body, "model").String())
				require.Equal(t, "draw a cat", gjson.GetBytes(body, "prompt").String())
			},
		},
		{
			name:        "flare OAuth",
			modelID:     "gpt-image-2.5-flare",
			accountType: AccountTypeOAuth,
			accessToken: "token-123",
			upstreamResp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body: io.NopCloser(strings.NewReader(
					"data: {\"type\":\"response.output_item.done\",\"item\":{\"id\":\"ig_123\",\"type\":\"image_generation_call\",\"result\":\"aGVsbG8=\",\"revised_prompt\":\"draw a cat\",\"output_format\":\"png\"}}\n\n" +
						"data: {\"type\":\"response.completed\",\"response\":{\"created_at\":1710000006,\"tool_usage\":{\"image_gen\":{\"images\":1}},\"output\":[]}}\n\n" +
						"data: [DONE]\n\n",
				)),
			},
			assertReq: func(t *testing.T, req *http.Request, body []byte) {
				t.Helper()
				require.Equal(t, chatgptCodexAPIURL, req.URL.String())
				require.Equal(t, "chatgpt.com", req.Host)
				require.Equal(t, "Bearer token-123", req.Header.Get("Authorization"))
				require.Equal(t, openAIImagesResponsesMainModel, gjson.GetBytes(body, "model").String())
				require.Equal(t, "gpt-image-2.5-flare", gjson.GetBytes(body, "tools.0.model").String())
			},
		},
		{
			name:        "sunburst OAuth",
			modelID:     "gpt-image-2.5-sunburst",
			accountType: AccountTypeOAuth,
			accessToken: "token-123",
			upstreamResp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body: io.NopCloser(strings.NewReader(
					"data: {\"type\":\"response.output_item.done\",\"item\":{\"id\":\"ig_123\",\"type\":\"image_generation_call\",\"result\":\"aGVsbG8=\",\"revised_prompt\":\"draw a cat\",\"output_format\":\"png\"}}\n\n" +
						"data: {\"type\":\"response.completed\",\"response\":{\"created_at\":1710000006,\"tool_usage\":{\"image_gen\":{\"images\":1}},\"output\":[]}}\n\n" +
						"data: [DONE]\n\n",
				)),
			},
			assertReq: func(t *testing.T, req *http.Request, body []byte) {
				t.Helper()
				require.Equal(t, chatgptCodexAPIURL, req.URL.String())
				require.Equal(t, "chatgpt.com", req.Host)
				require.Equal(t, "Bearer token-123", req.Header.Get("Authorization"))
				require.Equal(t, openAIImagesResponsesMainModel, gjson.GetBytes(body, "model").String())
				require.Equal(t, "gpt-image-2.5-sunburst", gjson.GetBytes(body, "tools.0.model").String())
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/1/test", nil)

			upstream := &httpUpstreamRecorder{resp: tc.upstreamResp}
			svc := &AccountTestService{httpUpstream: upstream, cfg: &config.Config{}}
			credentials := map[string]any{}
			if tc.apiKey != "" {
				credentials["api_key"] = tc.apiKey
			}
			if tc.baseURL != "" {
				credentials["base_url"] = tc.baseURL
			}
			if tc.accessToken != "" {
				credentials["access_token"] = tc.accessToken
			}
			account := &Account{
				ID:          55,
				Name:        "openai-image-25",
				Platform:    PlatformOpenAI,
				Type:        tc.accountType,
				Credentials: credentials,
			}

			var err error
			if tc.accountType == AccountTypeAPIKey {
				err = svc.testOpenAIImageAPIKey(c, context.Background(), account, tc.modelID, "draw a cat")
			} else {
				err = svc.testOpenAIImageOAuth(c, context.Background(), account, tc.modelID, "draw a cat")
			}
			require.NoError(t, err)
			require.NotNil(t, upstream.lastReq)
			require.Equal(t, HTTPUpstreamProfileOpenAI, HTTPUpstreamProfileFromContext(upstream.lastReq.Context()))
			tc.assertReq(t, upstream.lastReq, upstream.lastBody)
			require.Contains(t, rec.Body.String(), "data:image/png;base64,aGVsbG8=")
			require.Contains(t, rec.Body.String(), "\"success\":true")
		})
	}
}

func TestAccountTestService_OpenAIImageDefaultModelRemainsGPTImage2(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/1/test", nil)

	upstream := &httpUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"data":[{"b64_json":"aGVsbG8=","revised_prompt":"draw a cat"}]}`)),
		},
	}
	svc := &AccountTestService{httpUpstream: upstream, cfg: &config.Config{}}
	account := &Account{
		ID:       56,
		Name:     "openai-apikey-default",
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "test-api-key",
			"base_url": "https://image-upstream.example/v1",
		},
	}

	err := svc.testOpenAIImageAPIKey(c, context.Background(), account, "gpt-image-2", "draw a cat")
	require.NoError(t, err)
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, "gpt-image-2", gjson.GetBytes(upstream.lastBody, "model").String())
}

func TestAccountTestService_OpenAIImageUpstreamErrorDoesNotSwapModel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	testCases := []struct {
		name         string
		modelID      string
		accountType  string
		baseURL      string
		apiKey       string
		accessToken  string
		upstreamResp *http.Response
		wantErrText  string
		assertReq    func(*testing.T, *http.Request, []byte)
	}{
		{
			name:        "API key permission error",
			modelID:     "gpt-image-2.5-flare",
			accountType: AccountTypeAPIKey,
			baseURL:     "https://image-upstream.example/v1",
			apiKey:      "test-api-key",
			upstreamResp: &http.Response{
				StatusCode: http.StatusForbidden,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"model not permitted","type":"insufficient_quota"}}`)),
			},
			wantErrText: "API returned 403:",
			assertReq: func(t *testing.T, req *http.Request, body []byte) {
				t.Helper()
				require.Equal(t, "gpt-image-2.5-flare", gjson.GetBytes(body, "model").String())
			},
		},
		{
			name:        "OAuth permission error",
			modelID:     "gpt-image-2.5-sunburst",
			accountType: AccountTypeOAuth,
			accessToken: "token-123",
			upstreamResp: &http.Response{
				StatusCode: http.StatusForbidden,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"model not permitted","type":"insufficient_quota"}}`)),
			},
			wantErrText: "model not permitted",
			assertReq: func(t *testing.T, req *http.Request, body []byte) {
				t.Helper()
				require.Equal(t, "gpt-image-2.5-sunburst", gjson.GetBytes(body, "tools.0.model").String())
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/1/test", nil)

			upstream := &httpUpstreamRecorder{resp: tc.upstreamResp}
			svc := &AccountTestService{httpUpstream: upstream, cfg: &config.Config{}}
			credentials := map[string]any{}
			if tc.apiKey != "" {
				credentials["api_key"] = tc.apiKey
			}
			if tc.baseURL != "" {
				credentials["base_url"] = tc.baseURL
			}
			if tc.accessToken != "" {
				credentials["access_token"] = tc.accessToken
			}
			account := &Account{
				ID:          57,
				Name:        "openai-image-25-denied",
				Platform:    PlatformOpenAI,
				Type:        tc.accountType,
				Credentials: credentials,
			}

			var err error
			if tc.accountType == AccountTypeAPIKey {
				err = svc.testOpenAIImageAPIKey(c, context.Background(), account, tc.modelID, "draw a cat")
			} else {
				err = svc.testOpenAIImageOAuth(c, context.Background(), account, tc.modelID, "draw a cat")
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantErrText)
			require.NotNil(t, upstream.lastReq)
			tc.assertReq(t, upstream.lastReq, upstream.lastBody)
			require.Contains(t, rec.Body.String(), "\"type\":\"error\"")
		})
	}
}
