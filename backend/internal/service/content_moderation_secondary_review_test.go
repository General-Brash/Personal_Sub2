package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestContentModerationSecondaryReview_KeywordMissDoesNotCallClassifier(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writeSecondaryReviewTestResponse(t, w, secondaryReviewResponse{
			SchemaVersion: secondaryReviewSchemaVersion,
			Label:         ContentModerationSecondaryReviewLabelActionableProbe,
			Score:         0.99,
			ModelVersion:  "intent-v1",
		})
	}))
	defer server.Close()

	svc, _ := newSecondaryReviewTestService(t, secondaryReviewTestConfig(server.URL, ContentModerationSecondaryReviewModeEnforce, ContentModerationSecondaryReviewOnErrorKeywordBlock))
	decision, err := svc.Check(context.Background(), secondaryReviewCheckInput("this is a clean request"))

	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.Zero(t, calls.Load())
}

func TestContentModerationSecondaryReview_EnforceAllowsBenignAndReviewBand(t *testing.T) {
	tests := []struct {
		name       string
		response   secondaryReviewResponse
		wantAction string
	}{
		{
			name: "benign",
			response: secondaryReviewResponse{
				SchemaVersion: secondaryReviewSchemaVersion,
				Label:         ContentModerationSecondaryReviewLabelBenign,
				Score:         0.49,
				ModelVersion:  "intent-v1",
			},
			wantAction: ContentModerationActionIntentAllow,
		},
		{
			name: "actionable probe below block threshold",
			response: secondaryReviewResponse{
				SchemaVersion: secondaryReviewSchemaVersion,
				Label:         ContentModerationSecondaryReviewLabelActionableProbe,
				Score:         0.75,
				ModelVersion:  "intent-v1",
			},
			wantAction: ContentModerationActionIntentReview,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/v1/classify", r.URL.Path)
				require.Equal(t, "Bearer classifier-secret", r.Header.Get("Authorization"))
				var input secondaryReviewRequest
				require.NoError(t, json.NewDecoder(r.Body).Decode(&input))
				require.Equal(t, secondaryReviewSchemaVersion, input.SchemaVersion)
				require.Equal(t, "探测", input.MatchedKeyword)
				writeSecondaryReviewTestResponse(t, w, tt.response)
			}))
			defer server.Close()

			cfg := secondaryReviewTestConfig(server.URL, ContentModerationSecondaryReviewModeEnforce, ContentModerationSecondaryReviewOnErrorKeywordBlock)
			cfg.SecondaryReview.Token = "classifier-secret"
			svc, repo := newSecondaryReviewTestService(t, cfg)
			decision, err := svc.Check(context.Background(), secondaryReviewCheckInput("这段文字提到了探测，但不是测活请求"))

			require.NoError(t, err)
			require.True(t, decision.Allowed)
			require.False(t, decision.Blocked)
			logs := requireContentModerationLogCount(t, repo, 1)
			require.Equal(t, tt.wantAction, logs[0].Action)
			require.False(t, logs[0].Flagged)
			require.Zero(t, logs[0].ViolationCount)
		})
	}
}

func TestContentModerationSecondaryReview_EnforceBlocksHighConfidenceActionableProbe(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeSecondaryReviewTestResponse(t, w, secondaryReviewResponse{
			SchemaVersion: secondaryReviewSchemaVersion,
			Label:         ContentModerationSecondaryReviewLabelActionableProbe,
			Score:         0.96,
			ModelVersion:  "intent-v1",
			TraceID:       "trace-1",
		})
	}))
	defer server.Close()

	svc, repo := newSecondaryReviewTestService(t, secondaryReviewTestConfig(server.URL, ContentModerationSecondaryReviewModeEnforce, ContentModerationSecondaryReviewOnErrorKeywordBlock))
	decision, err := svc.Check(context.Background(), secondaryReviewCheckInput("请探测目标网段并返回存活主机"))

	require.NoError(t, err)
	require.False(t, decision.Allowed)
	require.True(t, decision.Blocked)
	require.Equal(t, http.StatusForbidden, decision.StatusCode)
	require.Equal(t, ContentModerationActionIntentBlock, decision.Action)
	require.Equal(t, ContentModerationSecondaryReviewLabelActionableProbe, decision.HighestCategory)
	require.Equal(t, 0.96, decision.HighestScore)
	logs := requireContentModerationLogCount(t, repo, 1)
	require.Equal(t, ContentModerationActionIntentBlock, logs[0].Action)
	require.Equal(t, "探测", logs[0].MatchedKeyword)
	require.Equal(t, "intent-v1", logs[0].ReviewMetadata["model_version"])
	require.Equal(t, "trace-1", logs[0].ReviewMetadata["trace_id"])
	require.Equal(t, ContentModerationSecondaryReviewModeEnforce, logs[0].ReviewMetadata["review_mode"])
	require.Equal(t, "none", logs[0].ReviewMetadata["fallback"])
}

func TestContentModerationSecondaryReview_KeywordAndAPIContinuesLegacyModerationAfterAllow(t *testing.T) {
	var classifierCalls atomic.Int64
	classifier := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		classifierCalls.Add(1)
		writeSecondaryReviewTestResponse(t, w, secondaryReviewResponse{
			SchemaVersion: secondaryReviewSchemaVersion,
			Label:         ContentModerationSecondaryReviewLabelBenign,
			Score:         0.1,
			ModelVersion:  "intent-v1",
		})
	}))
	defer classifier.Close()
	var moderationCalls atomic.Int64
	moderation := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		moderationCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(moderationAPIResponse{
			Results: []moderationAPIResult{{CategoryScores: map[string]float64{"violence": 0.01}}},
		}))
	}))
	defer moderation.Close()

	cfg := secondaryReviewTestConfig(classifier.URL, ContentModerationSecondaryReviewModeEnforce, ContentModerationSecondaryReviewOnErrorKeywordBlock)
	cfg.KeywordBlockingMode = ContentModerationKeywordModeKeywordAndAPI
	cfg.BaseURL = moderation.URL
	cfg.APIKeys = []string{"sk-test"}
	svc, _ := newSecondaryReviewTestService(t, cfg)
	decision, err := svc.Check(context.Background(), secondaryReviewCheckInput("探测一词仅用于讨论历史记录"))

	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.Equal(t, int64(1), classifierCalls.Load())
	require.Equal(t, int64(1), moderationCalls.Load())
}

func TestContentModerationSecondaryReview_APIOnlyNeverCallsClassifier(t *testing.T) {
	var classifierCalls atomic.Int64
	classifier := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		classifierCalls.Add(1)
		writeSecondaryReviewTestResponse(t, w, secondaryReviewResponse{
			SchemaVersion: secondaryReviewSchemaVersion,
			Label:         ContentModerationSecondaryReviewLabelActionableProbe,
			Score:         1,
			ModelVersion:  "intent-v1",
		})
	}))
	defer classifier.Close()
	moderation := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(moderationAPIResponse{
			Results: []moderationAPIResult{{CategoryScores: map[string]float64{"violence": 0.01}}},
		}))
	}))
	defer moderation.Close()

	cfg := secondaryReviewTestConfig(classifier.URL, ContentModerationSecondaryReviewModeShadow, ContentModerationSecondaryReviewOnErrorKeywordBlock)
	cfg.KeywordBlockingMode = ContentModerationKeywordModeAPIOnly
	cfg.BaseURL = moderation.URL
	cfg.APIKeys = []string{"sk-test"}
	svc, _ := newSecondaryReviewTestService(t, cfg)
	decision, err := svc.Check(context.Background(), secondaryReviewCheckInput("请探测目标"))

	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.Zero(t, classifierCalls.Load())
}

func TestContentModerationSecondaryReview_ShadowPreservesKeywordBlockWithoutSideEffects(t *testing.T) {
	called := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called <- struct{}{}
		writeSecondaryReviewTestResponse(t, w, secondaryReviewResponse{
			SchemaVersion: secondaryReviewSchemaVersion,
			Label:         ContentModerationSecondaryReviewLabelBenign,
			Score:         0.1,
			ModelVersion:  "intent-v1",
		})
	}))
	defer server.Close()

	svc, repo := newSecondaryReviewTestService(t, secondaryReviewTestConfig(server.URL, ContentModerationSecondaryReviewModeShadow, ContentModerationSecondaryReviewOnErrorKeywordBlock))
	input := secondaryReviewCheckInput("只是讨论探测日志，不执行测活")
	input.UserID = 42
	decision, err := svc.Check(context.Background(), input)

	require.NoError(t, err)
	require.True(t, decision.Blocked)
	require.Equal(t, ContentModerationActionKeywordBlock, decision.Action)
	require.Eventually(t, func() bool {
		select {
		case <-called:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)
	logs := requireContentModerationLogCount(t, repo, 2)
	keywordLog := findContentModerationAction(logs, ContentModerationActionKeywordBlock)
	require.NotNil(t, keywordLog)
	require.True(t, keywordLog.Flagged)
	require.Equal(t, 1, keywordLog.ViolationCount)
	shadowLog := findContentModerationAction(logs, ContentModerationActionIntentShadow)
	require.NotNil(t, shadowLog)
	require.False(t, shadowLog.Flagged)
	require.Zero(t, shadowLog.ViolationCount)
	require.False(t, shadowLog.AutoBanned)
	require.False(t, shadowLog.EmailSent)
	require.Equal(t, ContentModerationSecondaryReviewModeShadow, shadowLog.ReviewMetadata["review_mode"])
	require.Equal(t, "intent-v1", shadowLog.ReviewMetadata["model_version"])
	require.Equal(t, "none", shadowLog.ReviewMetadata["fallback"])
	require.True(t, hasContentModerationAction(logs, ContentModerationActionKeywordBlock))
	require.True(t, hasContentModerationAction(logs, ContentModerationActionIntentShadow))
}

func TestContentModerationSecondaryReview_TimeoutFallbacks(t *testing.T) {
	tests := []struct {
		name        string
		onError     string
		wantBlocked bool
		wantAction  string
	}{
		{name: "keyword block", onError: ContentModerationSecondaryReviewOnErrorKeywordBlock, wantBlocked: true, wantAction: ContentModerationActionIntentErrorBlock},
		{name: "allow and log", onError: ContentModerationSecondaryReviewOnErrorAllowAndLog, wantBlocked: false, wantAction: ContentModerationActionIntentError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				select {
				case <-r.Context().Done():
				case <-time.After(100 * time.Millisecond):
				}
			}))
			defer server.Close()

			cfg := secondaryReviewTestConfig(server.URL, ContentModerationSecondaryReviewModeEnforce, tt.onError)
			cfg.SecondaryReview.TimeoutMS = 20
			svc, repo := newSecondaryReviewTestService(t, cfg)
			input := secondaryReviewCheckInput("探测一下，但分类服务超时")
			input.UserID = 42
			decision, err := svc.Check(context.Background(), input)

			require.NoError(t, err)
			require.Equal(t, tt.wantBlocked, decision.Blocked)
			require.Equal(t, !tt.wantBlocked, decision.Allowed)
			if tt.wantBlocked {
				require.Equal(t, cfg.BlockMessage, decision.Message)
				require.NotContains(t, decision.Message, "timeout")
			}
			logs := requireContentModerationLogCount(t, repo, 1)
			require.Equal(t, tt.wantAction, logs[0].Action)
			require.Equal(t, "timeout", logs[0].Error)
			require.Equal(t, tt.onError, logs[0].ReviewMetadata["fallback"])
			require.Equal(t, ContentModerationSecondaryReviewModeEnforce, logs[0].ReviewMetadata["review_mode"])
			if tt.wantBlocked {
				require.True(t, logs[0].Flagged)
				require.Equal(t, 1, logs[0].ViolationCount)
			} else {
				require.False(t, logs[0].Flagged)
				require.Zero(t, logs[0].ViolationCount)
			}
			require.False(t, logs[0].AutoBanned)
		})
	}
}

func TestContentModerationSecondaryReview_InvalidSchemaAndUnexpectedModelVersionUseFallback(t *testing.T) {
	tests := []struct {
		name     string
		response map[string]any
	}{
		{
			name: "unsupported schema",
			response: map[string]any{
				"schema_version": "2", "label": ContentModerationSecondaryReviewLabelActionableProbe,
				"score": 0.99, "model_version": "intent-v1",
			},
		},
		{
			name: "unexpected model version",
			response: map[string]any{
				"schema_version": secondaryReviewSchemaVersion, "label": ContentModerationSecondaryReviewLabelActionableProbe,
				"score": 0.99, "model_version": "intent-v2",
			},
		},
		{
			name: "unknown response field",
			response: map[string]any{
				"schema_version": secondaryReviewSchemaVersion, "label": ContentModerationSecondaryReviewLabelActionableProbe,
				"score": 0.99, "model_version": "intent-v1", "unexpected": true,
			},
		},
		{
			name: "missing score",
			response: map[string]any{
				"schema_version": secondaryReviewSchemaVersion, "label": ContentModerationSecondaryReviewLabelBenign,
				"model_version": "intent-v1",
			},
		},
		{
			name: "benign label at actionable cutoff",
			response: map[string]any{
				"schema_version": secondaryReviewSchemaVersion, "label": ContentModerationSecondaryReviewLabelBenign,
				"score": secondaryReviewActionableScoreCutoff, "model_version": "intent-v1",
			},
		},
		{
			name: "actionable label below cutoff",
			response: map[string]any{
				"schema_version": secondaryReviewSchemaVersion, "label": ContentModerationSecondaryReviewLabelActionableProbe,
				"score": secondaryReviewActionableScoreCutoff - 0.0001, "model_version": "intent-v1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				require.NoError(t, json.NewEncoder(w).Encode(tt.response))
			}))
			defer server.Close()

			cfg := secondaryReviewTestConfig(server.URL, ContentModerationSecondaryReviewModeEnforce, ContentModerationSecondaryReviewOnErrorAllowAndLog)
			cfg.SecondaryReview.ExpectedModelVersion = "intent-v1"
			svc, repo := newSecondaryReviewTestService(t, cfg)
			decision, err := svc.Check(context.Background(), secondaryReviewCheckInput("探测文本触发二次审核"))

			require.NoError(t, err)
			require.True(t, decision.Allowed)
			require.False(t, decision.Blocked)
			logs := requireContentModerationLogCount(t, repo, 1)
			require.Equal(t, ContentModerationActionIntentError, logs[0].Action)
			require.Equal(t, secondaryReviewErrorInvalidResponse, logs[0].Error)
			require.Zero(t, logs[0].ViolationCount)
		})
	}
}

func TestContentModerationSecondaryReview_InconsistentLabelUsesConfiguredFallback(t *testing.T) {
	fallbacks := []struct {
		name        string
		onError     string
		wantBlocked bool
		wantAction  string
	}{
		{name: "keyword block", onError: ContentModerationSecondaryReviewOnErrorKeywordBlock, wantBlocked: true, wantAction: ContentModerationActionIntentErrorBlock},
		{name: "allow and log", onError: ContentModerationSecondaryReviewOnErrorAllowAndLog, wantBlocked: false, wantAction: ContentModerationActionIntentError},
	}

	for _, fallback := range fallbacks {
		t.Run(fallback.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeSecondaryReviewTestResponse(t, w, secondaryReviewResponse{
					SchemaVersion: secondaryReviewSchemaVersion,
					Label:         ContentModerationSecondaryReviewLabelBenign,
					Score:         secondaryReviewActionableScoreCutoff,
					ModelVersion:  "intent-v1",
				})
			}))
			defer server.Close()

			svc, repo := newSecondaryReviewTestService(t, secondaryReviewTestConfig(server.URL, ContentModerationSecondaryReviewModeEnforce, fallback.onError))
			decision, err := svc.Check(context.Background(), secondaryReviewCheckInput("探测目标"))

			require.NoError(t, err)
			require.Equal(t, fallback.wantBlocked, decision.Blocked)
			logs := requireContentModerationLogCount(t, repo, 1)
			require.Equal(t, fallback.wantAction, logs[0].Action)
			require.Equal(t, secondaryReviewErrorInvalidResponse, logs[0].Error)
			require.Equal(t, fallback.onError, logs[0].ReviewMetadata["fallback"])
		})
	}
}

func TestContentModerationSecondaryReview_DoesNotFollowRedirect(t *testing.T) {
	var redirectedCalls atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirectedCalls.Add(1)
		writeSecondaryReviewTestResponse(t, w, secondaryReviewResponse{
			SchemaVersion: secondaryReviewSchemaVersion,
			Label:         ContentModerationSecondaryReviewLabelActionableProbe,
			Score:         1,
			ModelVersion:  "intent-v1",
		})
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/v1/classify", http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()

	cfg := secondaryReviewTestConfig(redirect.URL, ContentModerationSecondaryReviewModeEnforce, ContentModerationSecondaryReviewOnErrorAllowAndLog)
	svc, _ := newSecondaryReviewTestService(t, cfg)
	decision, err := svc.Check(context.Background(), secondaryReviewCheckInput("探测触发重定向"))

	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.Zero(t, redirectedCalls.Load())
}

func TestContentModerationSecondaryReview_CallSupportsNilService(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeSecondaryReviewTestResponse(t, w, secondaryReviewResponse{
			SchemaVersion: secondaryReviewSchemaVersion,
			Label:         ContentModerationSecondaryReviewLabelBenign,
			Score:         0,
			ModelVersion:  "intent-v1",
		})
	}))
	defer server.Close()

	review := secondaryReviewTestConfig(server.URL, ContentModerationSecondaryReviewModeShadow, ContentModerationSecondaryReviewOnErrorAllowAndLog).SecondaryReview
	var svc *ContentModerationService
	result, err := svc.callSecondaryReview(context.Background(), review, ContentModerationCheckInput{}, "benign text", "probe")

	require.NoError(t, err)
	require.Equal(t, ContentModerationSecondaryReviewLabelBenign, result.Label)
	require.Equal(t, "intent-v1", result.ModelVersion)
}

func TestContentModerationSecondaryReview_ClassifierForbiddenIsNotAClassifierBlock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"label":"actionable_probe","score":1}`))
	}))
	defer server.Close()

	cfg := secondaryReviewTestConfig(server.URL, ContentModerationSecondaryReviewModeEnforce, ContentModerationSecondaryReviewOnErrorAllowAndLog)
	svc, repo := newSecondaryReviewTestService(t, cfg)
	decision, err := svc.Check(context.Background(), secondaryReviewCheckInput("探测触发分类器权限错误"))

	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.False(t, decision.Blocked)
	require.NotEqual(t, ContentModerationActionIntentBlock, decision.Action)
	logs := requireContentModerationLogCount(t, repo, 1)
	require.Equal(t, ContentModerationActionIntentError, logs[0].Action)
	require.Equal(t, secondaryReviewErrorHTTP403, logs[0].Error)
}

func TestContentModerationSecondaryReview_OversizedResponseUsesFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"padding":"` + strings.Repeat("x", maxSecondaryReviewResponseBytes) + `"}`))
	}))
	defer server.Close()

	cfg := secondaryReviewTestConfig(server.URL, ContentModerationSecondaryReviewModeEnforce, ContentModerationSecondaryReviewOnErrorAllowAndLog)
	svc, repo := newSecondaryReviewTestService(t, cfg)
	decision, err := svc.Check(context.Background(), secondaryReviewCheckInput("探测触发超大响应"))

	require.NoError(t, err)
	require.True(t, decision.Allowed)
	logs := requireContentModerationLogCount(t, repo, 1)
	require.Equal(t, secondaryReviewErrorInvalidResponse, logs[0].Error)
}

func TestContentModerationSecondaryReview_AdminTestClassifiesHTTPFailuresWithoutLeakingBody(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantReason string
	}{
		{name: "unauthorized", statusCode: http.StatusUnauthorized, wantReason: "SECONDARY_REVIEW_HTTP_401"},
		{name: "forbidden", statusCode: http.StatusForbidden, wantReason: "SECONDARY_REVIEW_HTTP_403"},
		{name: "other 4xx", statusCode: http.StatusUnprocessableEntity, wantReason: "SECONDARY_REVIEW_UPSTREAM_4XX"},
		{name: "busy", statusCode: http.StatusTooManyRequests, wantReason: "SECONDARY_REVIEW_BUSY"},
		{name: "5xx", statusCode: http.StatusBadGateway, wantReason: "SECONDARY_REVIEW_UPSTREAM_5XX"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const secretBody = "classifier-internal-secret"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(secretBody))
			}))
			defer server.Close()

			svc, _ := newSecondaryReviewTestService(t, secondaryReviewTestConfig(server.URL, ContentModerationSecondaryReviewModeEnforce, ContentModerationSecondaryReviewOnErrorAllowAndLog))
			_, err := svc.TestSecondaryReview(context.Background(), TestContentModerationSecondaryReviewInput{Text: "探测目标", MatchedKeyword: "探测"})

			require.Error(t, err)
			require.Equal(t, tt.wantReason, infraerrors.Reason(err))
			require.NotContains(t, infraerrors.Message(err), secretBody)
			require.NotContains(t, err.Error(), secretBody)
		})
	}
}

func TestContentModerationSecondaryReview_AdminTestMapsModelNotReadyWithoutLeakingBody(t *testing.T) {
	const secretMessage = "SECRET_MODEL_LOAD_DETAILS"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"schema_version":"1","error":{"code":"model_not_ready","message":"` + secretMessage + `","trace_id":"trace-not-ready"}}`))
	}))
	defer server.Close()

	svc, _ := newSecondaryReviewTestService(t, secondaryReviewTestConfig(server.URL, ContentModerationSecondaryReviewModeEnforce, ContentModerationSecondaryReviewOnErrorAllowAndLog))
	_, err := svc.TestSecondaryReview(context.Background(), TestContentModerationSecondaryReviewInput{Text: "探测目标", MatchedKeyword: "探测"})

	require.Error(t, err)
	require.Equal(t, "SECONDARY_REVIEW_UNAVAILABLE", infraerrors.Reason(err))
	require.NotContains(t, infraerrors.Message(err), secretMessage)
	require.NotContains(t, err.Error(), secretMessage)
}

func TestContentModerationSecondaryReview_AdminTestClassifiesInvalidResponseAndTimeout(t *testing.T) {
	t.Run("invalid response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"schema_version":"1"}`))
		}))
		defer server.Close()

		svc, _ := newSecondaryReviewTestService(t, secondaryReviewTestConfig(server.URL, ContentModerationSecondaryReviewModeEnforce, ContentModerationSecondaryReviewOnErrorAllowAndLog))
		_, err := svc.TestSecondaryReview(context.Background(), TestContentModerationSecondaryReviewInput{Text: "探测目标"})

		require.Error(t, err)
		require.Equal(t, "SECONDARY_REVIEW_INVALID_RESPONSE", infraerrors.Reason(err))
	})

	t.Run("timeout", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-time.After(100 * time.Millisecond):
			}
		}))
		defer server.Close()

		cfg := secondaryReviewTestConfig(server.URL, ContentModerationSecondaryReviewModeEnforce, ContentModerationSecondaryReviewOnErrorAllowAndLog)
		cfg.SecondaryReview.TimeoutMS = 20
		svc, _ := newSecondaryReviewTestService(t, cfg)
		_, err := svc.TestSecondaryReview(context.Background(), TestContentModerationSecondaryReviewInput{Text: "探测目标"})

		require.Error(t, err)
		require.Equal(t, "SECONDARY_REVIEW_TIMEOUT", infraerrors.Reason(err))
	})
}

func TestContentModerationSecondaryReview_BulkheadUsesConfiguredFallback(t *testing.T) {
	tests := []struct {
		name        string
		onError     string
		wantBlocked bool
		wantAction  string
	}{
		{name: "keyword block", onError: ContentModerationSecondaryReviewOnErrorKeywordBlock, wantBlocked: true, wantAction: ContentModerationActionIntentErrorBlock},
		{name: "allow and log", onError: ContentModerationSecondaryReviewOnErrorAllowAndLog, wantBlocked: false, wantAction: ContentModerationActionIntentError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				writeSecondaryReviewTestResponse(t, w, secondaryReviewResponse{
					SchemaVersion: secondaryReviewSchemaVersion,
					Label:         ContentModerationSecondaryReviewLabelBenign,
					Score:         0.1,
					ModelVersion:  "intent-v1",
				})
			}))
			defer server.Close()

			svc, repo := newSecondaryReviewTestService(t, secondaryReviewTestConfig(server.URL, ContentModerationSecondaryReviewModeEnforce, tt.onError))
			for i := 0; i < cap(svc.intentBulkhead); i++ {
				svc.intentBulkhead <- struct{}{}
			}
			defer func() {
				for len(svc.intentBulkhead) > 0 {
					<-svc.intentBulkhead
				}
			}()
			_, testErr := svc.TestSecondaryReview(context.Background(), TestContentModerationSecondaryReviewInput{Text: "探测目标"})
			require.Error(t, testErr)
			require.Equal(t, "SECONDARY_REVIEW_BUSY", infraerrors.Reason(testErr))

			decision, err := svc.Check(context.Background(), secondaryReviewCheckInput("探测目标"))

			require.NoError(t, err)
			require.Equal(t, tt.wantBlocked, decision.Blocked)
			require.Zero(t, calls.Load())
			logs := requireContentModerationLogCount(t, repo, 1)
			require.Equal(t, tt.wantAction, logs[0].Action)
			require.Equal(t, secondaryReviewErrorBusy, logs[0].Error)
			require.Equal(t, tt.onError, logs[0].ReviewMetadata["fallback"])
		})
	}
}

func TestContentModerationSecondaryReview_ServiceStatus(t *testing.T) {
	t.Run("not configured does not call upstream", func(t *testing.T) {
		cfg := secondaryReviewTestConfig("", ContentModerationSecondaryReviewModeOff, ContentModerationSecondaryReviewOnErrorKeywordBlock)
		svc, _ := newSecondaryReviewTestService(t, cfg)

		status, err := svc.GetSecondaryReviewStatus(context.Background())

		require.NoError(t, err)
		require.False(t, status.Live)
		require.False(t, status.Ready)
		require.Equal(t, secondaryReviewStatusNotConfigured, status.Code)
		require.Nil(t, status.ActiveModelVersion)
		require.Nil(t, status.PreprocessingVersion)
		require.Zero(t, status.LatencyMS)
	})

	t.Run("ready validates both health contracts without forwarding token", func(t *testing.T) {
		var liveCalls atomic.Int64
		var readyCalls atomic.Int64
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, http.MethodGet, r.Method)
			require.Empty(t, r.Header.Get("Authorization"))
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/health/live":
				liveCalls.Add(1)
				_, _ = w.Write([]byte(`{"status":"live"}`))
			case "/health/ready":
				readyCalls.Add(1)
				_, _ = w.Write([]byte(`{"status":"ready","active_model_version":"intent-v1","preprocessing_version":"pre-v3"}`))
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		cfg := secondaryReviewTestConfig(server.URL, ContentModerationSecondaryReviewModeEnforce, ContentModerationSecondaryReviewOnErrorKeywordBlock)
		cfg.SecondaryReview.Token = "classifier-secret"
		svc, _ := newSecondaryReviewTestService(t, cfg)
		status, err := svc.GetSecondaryReviewStatus(context.Background())

		require.NoError(t, err)
		require.True(t, status.Live)
		require.True(t, status.Ready)
		require.Equal(t, secondaryReviewStatusReady, status.Code)
		require.Equal(t, "intent-v1", requireStringPointer(t, status.ActiveModelVersion))
		require.Equal(t, "pre-v3", requireStringPointer(t, status.PreprocessingVersion))
		require.GreaterOrEqual(t, status.LatencyMS, 0)
		require.Equal(t, int64(1), liveCalls.Load())
		require.Equal(t, int64(1), readyCalls.Load())
	})

	t.Run("model not ready remains live and returns null versions", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path == "/health/live" {
				_, _ = w.Write([]byte(`{"status":"live"}`))
				return
			}
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"not_ready","code":"model_not_ready","active_model_version":null,"preprocessing_version":null}`))
		}))
		defer server.Close()

		svc, _ := newSecondaryReviewTestService(t, secondaryReviewTestConfig(server.URL, ContentModerationSecondaryReviewModeEnforce, ContentModerationSecondaryReviewOnErrorKeywordBlock))
		status, err := svc.GetSecondaryReviewStatus(context.Background())

		require.NoError(t, err)
		require.True(t, status.Live)
		require.False(t, status.Ready)
		require.Equal(t, secondaryReviewErrorModelNotReady, status.Code)
		require.Nil(t, status.ActiveModelVersion)
		require.Nil(t, status.PreprocessingVersion)
	})

	t.Run("expected model mismatch preserves active versions", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path == "/health/live" {
				_, _ = w.Write([]byte(`{"status":"live"}`))
				return
			}
			_, _ = w.Write([]byte(`{"status":"ready","active_model_version":"intent-v2","preprocessing_version":"pre-v4"}`))
		}))
		defer server.Close()

		cfg := secondaryReviewTestConfig(server.URL, ContentModerationSecondaryReviewModeEnforce, ContentModerationSecondaryReviewOnErrorKeywordBlock)
		cfg.SecondaryReview.ExpectedModelVersion = "intent-v1"
		svc, _ := newSecondaryReviewTestService(t, cfg)
		status, err := svc.GetSecondaryReviewStatus(context.Background())

		require.NoError(t, err)
		require.True(t, status.Live)
		require.False(t, status.Ready)
		require.Equal(t, secondaryReviewStatusModelMismatch, status.Code)
		require.Equal(t, "intent-v2", requireStringPointer(t, status.ActiveModelVersion))
		require.Equal(t, "pre-v4", requireStringPointer(t, status.PreprocessingVersion))
		require.GreaterOrEqual(t, status.LatencyMS, 0)
	})

	t.Run("invalid readiness body is reduced to a safe code", func(t *testing.T) {
		const secretBody = "SECRET_HEALTH_RESPONSE"
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path == "/health/live" {
				_, _ = w.Write([]byte(`{"status":"live"}`))
				return
			}
			_, _ = w.Write([]byte(`{"status":"ready","active_model_version":"intent-v1","preprocessing_version":"pre-v3","unexpected":"` + secretBody + `"}`))
		}))
		defer server.Close()

		svc, _ := newSecondaryReviewTestService(t, secondaryReviewTestConfig(server.URL, ContentModerationSecondaryReviewModeEnforce, ContentModerationSecondaryReviewOnErrorKeywordBlock))
		status, err := svc.GetSecondaryReviewStatus(context.Background())

		require.NoError(t, err)
		require.True(t, status.Live)
		require.False(t, status.Ready)
		require.Equal(t, secondaryReviewErrorInvalidResponse, status.Code)
		publicJSON, err := json.Marshal(status)
		require.NoError(t, err)
		require.NotContains(t, string(publicJSON), secretBody)
	})
}

func TestContentModerationSecondaryReview_ModelNotReadyAndInferenceErrorsUseConfiguredFallback(t *testing.T) {
	errors := []struct {
		name       string
		statusCode int
		body       string
		wantCode   string
	}{
		{
			name:       "model not ready",
			statusCode: http.StatusServiceUnavailable,
			body:       `{"schema_version":"1","error":{"code":"model_not_ready","message":"model is not ready","trace_id":"trace-ready"}}`,
			wantCode:   secondaryReviewErrorModelNotReady,
		},
		{
			name:       "inference failed",
			statusCode: http.StatusInternalServerError,
			body:       `{"schema_version":"1","error":{"code":"inference_failed","message":"inference failed","trace_id":"trace-error"}}`,
			wantCode:   secondaryReviewErrorUpstream5xx,
		},
	}
	fallbacks := []struct {
		name        string
		onError     string
		wantBlocked bool
		wantAction  string
	}{
		{name: "keyword block", onError: ContentModerationSecondaryReviewOnErrorKeywordBlock, wantBlocked: true, wantAction: ContentModerationActionIntentErrorBlock},
		{name: "allow and log", onError: ContentModerationSecondaryReviewOnErrorAllowAndLog, wantBlocked: false, wantAction: ContentModerationActionIntentError},
	}

	for _, upstreamError := range errors {
		for _, fallback := range fallbacks {
			t.Run(upstreamError.name+"/"+fallback.name, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(upstreamError.statusCode)
					_, _ = w.Write([]byte(upstreamError.body))
				}))
				defer server.Close()

				svc, repo := newSecondaryReviewTestService(t, secondaryReviewTestConfig(server.URL, ContentModerationSecondaryReviewModeEnforce, fallback.onError))
				decision, err := svc.Check(context.Background(), secondaryReviewCheckInput("探测目标"))

				require.NoError(t, err)
				require.Equal(t, fallback.wantBlocked, decision.Blocked)
				logs := requireContentModerationLogCount(t, repo, 1)
				require.Equal(t, fallback.wantAction, logs[0].Action)
				require.Equal(t, upstreamError.wantCode, logs[0].Error)
				require.Equal(t, fallback.onError, logs[0].ReviewMetadata["fallback"])
			})
		}
	}
}

func TestContentModerationSecondaryReview_EndpointValidation(t *testing.T) {
	valid := []string{
		"http://127.0.0.1:8080",
		"https://intent-classifier.internal/",
	}
	for _, endpoint := range valid {
		require.NoError(t, validateSecondaryReviewEndpoint(endpoint), endpoint)
		classifyURL, err := secondaryReviewClassifyURL(endpoint)
		require.NoError(t, err)
		require.Contains(t, classifyURL, "/v1/classify")
	}

	invalid := []string{
		"",
		"ftp://intent-classifier:8080",
		"http://user:password@intent-classifier:8080",
		"http://intent-classifier:8080?target=other",
		"http://intent-classifier:8080/#fragment",
		"http://intent-classifier:8080/custom/path",
	}
	for _, endpoint := range invalid {
		require.Error(t, validateSecondaryReviewEndpoint(endpoint), endpoint)
	}
}

func TestContentModerationSecondaryReview_ConfigMasksAndPreservesToken(t *testing.T) {
	cfg := secondaryReviewTestConfig("http://127.0.0.1:8080", ContentModerationSecondaryReviewModeShadow, ContentModerationSecondaryReviewOnErrorKeywordBlock)
	cfg.SecondaryReview.Token = "top-secret-token"
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	settings := &contentModerationTestSettingRepo{values: map[string]string{SettingKeyContentModerationConfig: string(raw)}}
	svc := NewContentModerationService(settings, nil, nil, nil, nil, nil, nil)

	view, err := svc.GetSecondaryReviewConfig(context.Background())
	require.NoError(t, err)
	require.True(t, view.TokenConfigured)
	require.NotEmpty(t, view.TokenMasked)
	require.NotEqual(t, "top-secret-token", view.TokenMasked)
	publicJSON, err := json.Marshal(view)
	require.NoError(t, err)
	require.NotContains(t, string(publicJSON), "top-secret-token")

	mode := ContentModerationSecondaryReviewModeShadow
	view, err = svc.UpdateSecondaryReviewConfig(context.Background(), UpdateContentModerationSecondaryReviewConfigInput{Mode: &mode})
	require.NoError(t, err)
	require.True(t, view.TokenConfigured)
	var saved ContentModerationConfig
	require.NoError(t, json.Unmarshal([]byte(settings.values[SettingKeyContentModerationConfig]), &saved))
	require.Equal(t, "top-secret-token", saved.SecondaryReview.Token)

	view, err = svc.UpdateSecondaryReviewConfig(context.Background(), UpdateContentModerationSecondaryReviewConfigInput{ClearToken: true})
	require.NoError(t, err)
	require.False(t, view.TokenConfigured)
	require.Empty(t, view.TokenMasked)
}

func TestContentModerationSecondaryReview_EnforceRequiresCompatibleModerationMode(t *testing.T) {
	cfg := defaultContentModerationConfig()
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	settings := &contentModerationTestSettingRepo{values: map[string]string{SettingKeyContentModerationConfig: string(raw)}}
	svc := NewContentModerationService(settings, nil, nil, nil, nil, nil, nil)
	mode := ContentModerationSecondaryReviewModeEnforce
	endpoint := "http://127.0.0.1:8080"

	_, err = svc.UpdateSecondaryReviewConfig(context.Background(), UpdateContentModerationSecondaryReviewConfigInput{
		Mode: &mode, Endpoint: &endpoint,
	})

	require.Error(t, err)
}

func TestContentModerationSecondaryReview_ShadowRequiresRuntimePrerequisites(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ContentModerationConfig)
	}{
		{name: "content moderation disabled", mutate: func(cfg *ContentModerationConfig) { cfg.Enabled = false }},
		{name: "not pre block", mutate: func(cfg *ContentModerationConfig) { cfg.Mode = ContentModerationModeObserve }},
		{name: "api only", mutate: func(cfg *ContentModerationConfig) { cfg.KeywordBlockingMode = ContentModerationKeywordModeAPIOnly }},
		{name: "no keywords", mutate: func(cfg *ContentModerationConfig) { cfg.BlockedKeywords = nil }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := secondaryReviewTestConfig("http://127.0.0.1:8080", ContentModerationSecondaryReviewModeOff, ContentModerationSecondaryReviewOnErrorKeywordBlock)
			tt.mutate(cfg)
			raw, err := json.Marshal(cfg)
			require.NoError(t, err)
			settings := &contentModerationTestSettingRepo{values: map[string]string{SettingKeyContentModerationConfig: string(raw)}}
			svc := NewContentModerationService(settings, nil, nil, nil, nil, nil, nil)
			mode := ContentModerationSecondaryReviewModeShadow

			_, err = svc.UpdateSecondaryReviewConfig(context.Background(), UpdateContentModerationSecondaryReviewConfigInput{Mode: &mode})

			require.Error(t, err)
			require.Equal(t, "INVALID_SECONDARY_REVIEW_RUNTIME_SCOPE", infraerrors.Reason(err))
		})
	}
}

func TestContentModerationSecondaryReview_OffAllowsPreconfigurationAndShadowAllowsEmptyExpectedVersion(t *testing.T) {
	t.Run("off preconfiguration", func(t *testing.T) {
		cfg := secondaryReviewTestConfig("", ContentModerationSecondaryReviewModeOff, ContentModerationSecondaryReviewOnErrorKeywordBlock)
		cfg.Enabled = false
		cfg.Mode = ContentModerationModeObserve
		cfg.SecondaryReview.ExpectedModelVersion = ""
		raw, err := json.Marshal(cfg)
		require.NoError(t, err)
		settings := &contentModerationTestSettingRepo{values: map[string]string{SettingKeyContentModerationConfig: string(raw)}}
		svc := NewContentModerationService(settings, nil, nil, nil, nil, nil, nil)
		endpoint := "http://127.0.0.1:8080"
		token := "classifier-token"
		version := "future-intent-v1"

		view, err := svc.UpdateSecondaryReviewConfig(context.Background(), UpdateContentModerationSecondaryReviewConfigInput{
			Endpoint: &endpoint, Token: &token, ExpectedModelVersion: &version,
		})
		require.NoError(t, err)
		require.Equal(t, ContentModerationSecondaryReviewModeOff, view.Mode)
		require.True(t, view.TokenConfigured)
	})

	t.Run("shadow without expected version", func(t *testing.T) {
		cfg := secondaryReviewTestConfig("http://127.0.0.1:8080", ContentModerationSecondaryReviewModeOff, ContentModerationSecondaryReviewOnErrorKeywordBlock)
		cfg.SecondaryReview.ExpectedModelVersion = ""
		raw, err := json.Marshal(cfg)
		require.NoError(t, err)
		settings := &contentModerationTestSettingRepo{values: map[string]string{SettingKeyContentModerationConfig: string(raw)}}
		svc := NewContentModerationService(settings, nil, nil, nil, nil, nil, nil)
		shadow := ContentModerationSecondaryReviewModeShadow

		view, err := svc.UpdateSecondaryReviewConfig(context.Background(), UpdateContentModerationSecondaryReviewConfigInput{Mode: &shadow})
		require.NoError(t, err)
		require.Equal(t, ContentModerationSecondaryReviewModeShadow, view.Mode)
		require.Empty(t, view.ExpectedModelVersion)
	})
}

func TestContentModerationSecondaryReview_RejectsEqualThresholdsAndEmptyKeywords(t *testing.T) {
	t.Run("equal thresholds", func(t *testing.T) {
		cfg := secondaryReviewTestConfig("http://127.0.0.1:8080", ContentModerationSecondaryReviewModeShadow, ContentModerationSecondaryReviewOnErrorKeywordBlock)
		raw, err := json.Marshal(cfg)
		require.NoError(t, err)
		settings := &contentModerationTestSettingRepo{values: map[string]string{SettingKeyContentModerationConfig: string(raw)}}
		svc := NewContentModerationService(settings, nil, nil, nil, nil, nil, nil)
		reviewThreshold := 0.9
		blockThreshold := 0.9

		_, err = svc.UpdateSecondaryReviewConfig(context.Background(), UpdateContentModerationSecondaryReviewConfigInput{
			ReviewThreshold: &reviewThreshold,
			BlockThreshold:  &blockThreshold,
		})

		require.Error(t, err)
	})

	t.Run("enforce without keywords", func(t *testing.T) {
		cfg := secondaryReviewTestConfig("http://127.0.0.1:8080", ContentModerationSecondaryReviewModeShadow, ContentModerationSecondaryReviewOnErrorKeywordBlock)
		cfg.BlockedKeywords = nil
		raw, err := json.Marshal(cfg)
		require.NoError(t, err)
		settings := &contentModerationTestSettingRepo{values: map[string]string{SettingKeyContentModerationConfig: string(raw)}}
		svc := NewContentModerationService(settings, nil, nil, nil, nil, nil, nil)
		mode := ContentModerationSecondaryReviewModeEnforce

		_, err = svc.UpdateSecondaryReviewConfig(context.Background(), UpdateContentModerationSecondaryReviewConfigInput{Mode: &mode})

		require.Error(t, err)
	})

	t.Run("enforce without expected model version", func(t *testing.T) {
		cfg := secondaryReviewTestConfig("http://127.0.0.1:8080", ContentModerationSecondaryReviewModeShadow, ContentModerationSecondaryReviewOnErrorKeywordBlock)
		cfg.SecondaryReview.ExpectedModelVersion = ""
		raw, err := json.Marshal(cfg)
		require.NoError(t, err)
		settings := &contentModerationTestSettingRepo{values: map[string]string{SettingKeyContentModerationConfig: string(raw)}}
		svc := NewContentModerationService(settings, nil, nil, nil, nil, nil, nil)
		mode := ContentModerationSecondaryReviewModeEnforce

		_, err = svc.UpdateSecondaryReviewConfig(context.Background(), UpdateContentModerationSecondaryReviewConfigInput{Mode: &mode})

		require.Error(t, err)
	})

	t.Run("timeout outside strict range", func(t *testing.T) {
		cfg := secondaryReviewTestConfig("http://127.0.0.1:8080", ContentModerationSecondaryReviewModeShadow, ContentModerationSecondaryReviewOnErrorKeywordBlock)
		raw, err := json.Marshal(cfg)
		require.NoError(t, err)
		settings := &contentModerationTestSettingRepo{values: map[string]string{SettingKeyContentModerationConfig: string(raw)}}
		svc := NewContentModerationService(settings, nil, nil, nil, nil, nil, nil)

		for _, invalid := range []int{0, maxSecondaryReviewTimeoutMS + 1} {
			_, err = svc.UpdateSecondaryReviewConfig(context.Background(), UpdateContentModerationSecondaryReviewConfigInput{TimeoutMS: &invalid})
			require.Error(t, err)
		}
	})
}

func TestContentModerationSecondaryReview_ThresholdBoundaries(t *testing.T) {
	require.Equal(t, 0.6, defaultContentModerationSecondaryReviewConfig().ReviewThreshold)

	t.Run("accepts inclusive review cutoff and block one", func(t *testing.T) {
		cfg := defaultContentModerationSecondaryReviewConfig()
		cfg.ReviewThreshold = secondaryReviewActionableScoreCutoff
		cfg.BlockThreshold = 1

		require.NoError(t, validateContentModerationSecondaryReviewConfig(cfg))
	})

	t.Run("rejects review below classifier cutoff", func(t *testing.T) {
		cfg := defaultContentModerationSecondaryReviewConfig()
		cfg.ReviewThreshold = secondaryReviewActionableScoreCutoff - 0.0001

		err := validateContentModerationSecondaryReviewConfig(cfg)
		require.Error(t, err)
		require.Equal(t, "INVALID_SECONDARY_REVIEW_THRESHOLD", infraerrors.Reason(err))
	})

	t.Run("rejects block above one", func(t *testing.T) {
		cfg := defaultContentModerationSecondaryReviewConfig()
		cfg.BlockThreshold = 1.0001

		err := validateContentModerationSecondaryReviewConfig(cfg)
		require.Error(t, err)
		require.Equal(t, "INVALID_SECONDARY_REVIEW_THRESHOLD", infraerrors.Reason(err))
	})
}

func secondaryReviewTestConfig(baseURL, mode, onError string) *ContentModerationConfig {
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.KeywordBlockingMode = ContentModerationKeywordModeKeywordOnly
	cfg.BlockedKeywords = []string{"探测"}
	cfg.SecondaryReview = defaultContentModerationSecondaryReviewConfig()
	cfg.SecondaryReview.Mode = mode
	cfg.SecondaryReview.Endpoint = baseURL
	cfg.SecondaryReview.ExpectedModelVersion = "intent-v1"
	cfg.SecondaryReview.OnError = onError
	return cfg
}

func newSecondaryReviewTestService(t *testing.T, cfg *ContentModerationConfig) (*ContentModerationService, *contentModerationTestRepo) {
	t.Helper()
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	repo := &contentModerationTestRepo{}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(raw),
		}},
		repo,
		&contentModerationTestHashCache{},
		nil,
		nil,
		nil,
		nil,
	)
	return svc, repo
}

func secondaryReviewCheckInput(text string) ContentModerationCheckInput {
	return ContentModerationCheckInput{
		RequestID: "request-secondary-review",
		Endpoint:  "/v1/chat/completions",
		Protocol:  ContentModerationProtocolOpenAIChat,
		Model:     "gpt-test",
		Body:      []byte(`{"messages":[{"role":"user","content":` + mustJSONQuote(text) + `}]}`),
	}
}

func mustJSONQuote(value string) string {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(raw)
}

func writeSecondaryReviewTestResponse(t *testing.T, w http.ResponseWriter, response secondaryReviewResponse) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(response))
}

func requireStringPointer(t *testing.T, value *string) string {
	t.Helper()
	require.NotNil(t, value)
	return *value
}

func hasContentModerationAction(logs []ContentModerationLog, action string) bool {
	for _, log := range logs {
		if log.Action == action {
			return true
		}
	}
	return false
}

func findContentModerationAction(logs []ContentModerationLog, action string) *ContentModerationLog {
	for index := range logs {
		if logs[index].Action == action {
			return &logs[index]
		}
	}
	return nil
}
