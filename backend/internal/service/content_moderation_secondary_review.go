package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/servertiming"
)

const (
	ContentModerationSecondaryReviewModeOff     = "off"
	ContentModerationSecondaryReviewModeShadow  = "shadow"
	ContentModerationSecondaryReviewModeEnforce = "enforce"

	ContentModerationSecondaryReviewOnErrorKeywordBlock = "keyword_block"
	ContentModerationSecondaryReviewOnErrorAllowAndLog  = "allow_and_log"

	ContentModerationSecondaryReviewLabelBenign          = "benign"
	ContentModerationSecondaryReviewLabelActionableProbe = "actionable_probe"

	ContentModerationActionIntentAllow      = "intent_allow"
	ContentModerationActionIntentReview     = "intent_review"
	ContentModerationActionIntentBlock      = "intent_block"
	ContentModerationActionIntentShadow     = "intent_shadow"
	ContentModerationActionIntentError      = "intent_error"
	ContentModerationActionIntentErrorBlock = "intent_error_block"

	defaultSecondaryReviewTimeoutMS       = 300
	secondaryReviewActionableScoreCutoff  = 0.50
	maxSecondaryReviewTimeoutMS           = 30000
	defaultSecondaryReviewReviewThreshold = 0.60
	defaultSecondaryReviewBlockThreshold  = 0.90
	maxSecondaryReviewResponseBytes       = 32 * 1024
	maxSecondaryReviewEndpointRunes       = 2048
	maxSecondaryReviewTokenRunes          = 8192
	maxSecondaryReviewVersionRunes        = 200
	maxSecondaryReviewConcurrentCalls     = 64
	secondaryReviewSchemaVersion          = "1"

	secondaryReviewErrorHTTP401         = "http_401"
	secondaryReviewErrorHTTP403         = "http_403"
	secondaryReviewErrorUpstream4xx     = "upstream_4xx"
	secondaryReviewErrorUpstream5xx     = "upstream_5xx"
	secondaryReviewErrorTimeout         = "timeout"
	secondaryReviewErrorInvalidResponse = "invalid_response"
	secondaryReviewErrorBusy            = "busy"
	secondaryReviewErrorModelNotReady   = "model_not_ready"
	secondaryReviewErrorUnavailable     = "unavailable"

	secondaryReviewStatusReady         = "ready"
	secondaryReviewStatusNotConfigured = "not_configured"
	secondaryReviewStatusModelMismatch = "model_version_mismatch"
)

type ContentModerationSecondaryReviewConfig struct {
	Mode                 string  `json:"mode"`
	Endpoint             string  `json:"endpoint"`
	Token                string  `json:"token,omitempty"`
	TimeoutMS            int     `json:"timeout_ms"`
	ReviewThreshold      float64 `json:"review_threshold"`
	BlockThreshold       float64 `json:"block_threshold"`
	ExpectedModelVersion string  `json:"expected_model_version"`
	OnError              string  `json:"on_error"`
}

type ContentModerationSecondaryReviewConfigView struct {
	Mode                 string  `json:"mode"`
	Endpoint             string  `json:"endpoint"`
	TokenConfigured      bool    `json:"token_configured"`
	TokenMasked          string  `json:"token_masked"`
	TimeoutMS            int     `json:"timeout_ms"`
	ReviewThreshold      float64 `json:"review_threshold"`
	BlockThreshold       float64 `json:"block_threshold"`
	ExpectedModelVersion string  `json:"expected_model_version"`
	OnError              string  `json:"on_error"`
}

type UpdateContentModerationSecondaryReviewConfigInput struct {
	Mode                 *string  `json:"mode"`
	Endpoint             *string  `json:"endpoint"`
	Token                *string  `json:"token"`
	ClearToken           bool     `json:"clear_token"`
	TimeoutMS            *int     `json:"timeout_ms"`
	ReviewThreshold      *float64 `json:"review_threshold"`
	BlockThreshold       *float64 `json:"block_threshold"`
	ExpectedModelVersion *string  `json:"expected_model_version"`
	OnError              *string  `json:"on_error"`
}

type TestContentModerationSecondaryReviewInput struct {
	Text           string `json:"text"`
	MatchedKeyword string `json:"matched_keyword"`
}

type TestContentModerationSecondaryReviewResult struct {
	Label        string  `json:"label"`
	Score        float64 `json:"score"`
	ModelVersion string  `json:"model_version"`
	TraceID      string  `json:"trace_id"`
	LatencyMS    int     `json:"latency_ms"`
	WouldReview  bool    `json:"would_review"`
	WouldBlock   bool    `json:"would_block"`
}

type ContentModerationSecondaryReviewStatus struct {
	Live                 bool    `json:"live"`
	Ready                bool    `json:"ready"`
	Code                 string  `json:"code"`
	ActiveModelVersion   *string `json:"active_model_version"`
	PreprocessingVersion *string `json:"preprocessing_version"`
	LatencyMS            int     `json:"latency_ms"`
}

type secondaryReviewRequest struct {
	SchemaVersion  string                 `json:"schema_version"`
	RequestID      string                 `json:"request_id,omitempty"`
	Text           string                 `json:"text"`
	MatchedKeyword string                 `json:"matched_keyword"`
	Context        secondaryReviewContext `json:"context"`
}

type secondaryReviewContext struct {
	Protocol string `json:"protocol,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
	Model    string `json:"model,omitempty"`
}

type secondaryReviewResponse struct {
	SchemaVersion string  `json:"schema_version"`
	Label         string  `json:"label"`
	Score         float64 `json:"score"`
	ModelVersion  string  `json:"model_version"`
	TraceID       string  `json:"trace_id,omitempty"`
}

type secondaryReviewResponseWire struct {
	SchemaVersion string   `json:"schema_version"`
	Label         string   `json:"label"`
	Score         *float64 `json:"score"`
	ModelVersion  string   `json:"model_version"`
	TraceID       string   `json:"trace_id,omitempty"`
}

type secondaryReviewLiveResponseWire struct {
	Status string `json:"status"`
}

type secondaryReviewReadyResponseWire struct {
	Status               string          `json:"status"`
	ActiveModelVersion   json.RawMessage `json:"active_model_version"`
	PreprocessingVersion json.RawMessage `json:"preprocessing_version"`
}

type secondaryReviewNotReadyResponseWire struct {
	Status               string          `json:"status"`
	Code                 string          `json:"code"`
	ActiveModelVersion   json.RawMessage `json:"active_model_version"`
	PreprocessingVersion json.RawMessage `json:"preprocessing_version"`
}

type secondaryReviewErrorResponseWire struct {
	SchemaVersion string `json:"schema_version"`
	Error         struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		TraceID string `json:"trace_id"`
	} `json:"error"`
}

type secondaryReviewReadyProbeResult struct {
	ready                bool
	modelNotReady        bool
	activeModelVersion   string
	preprocessingVersion string
}

type secondaryReviewCallError struct {
	code  string
	cause error
}

func (e *secondaryReviewCallError) Error() string {
	if e == nil || e.code == "" {
		return "secondary_review_failed"
	}
	return e.code
}

func (e *secondaryReviewCallError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func defaultContentModerationSecondaryReviewConfig() ContentModerationSecondaryReviewConfig {
	return ContentModerationSecondaryReviewConfig{
		Mode:            ContentModerationSecondaryReviewModeOff,
		TimeoutMS:       defaultSecondaryReviewTimeoutMS,
		ReviewThreshold: defaultSecondaryReviewReviewThreshold,
		BlockThreshold:  defaultSecondaryReviewBlockThreshold,
		OnError:         ContentModerationSecondaryReviewOnErrorKeywordBlock,
	}
}

func (cfg *ContentModerationSecondaryReviewConfig) normalize() {
	if cfg == nil {
		return
	}
	cfg.Mode = strings.ToLower(strings.TrimSpace(cfg.Mode))
	if cfg.Mode == "" {
		cfg.Mode = ContentModerationSecondaryReviewModeOff
	}
	cfg.Endpoint = strings.TrimSpace(cfg.Endpoint)
	cfg.Token = strings.TrimSpace(cfg.Token)
	if cfg.TimeoutMS <= 0 {
		cfg.TimeoutMS = defaultSecondaryReviewTimeoutMS
	}
	cfg.ExpectedModelVersion = strings.TrimSpace(cfg.ExpectedModelVersion)
	cfg.OnError = strings.ToLower(strings.TrimSpace(cfg.OnError))
	if cfg.OnError == "" {
		cfg.OnError = ContentModerationSecondaryReviewOnErrorKeywordBlock
	}
}

func validateContentModerationSecondaryReviewConfig(cfg ContentModerationSecondaryReviewConfig) error {
	cfg.normalize()
	switch cfg.Mode {
	case ContentModerationSecondaryReviewModeOff,
		ContentModerationSecondaryReviewModeShadow,
		ContentModerationSecondaryReviewModeEnforce:
	default:
		return infraerrors.BadRequest("INVALID_SECONDARY_REVIEW_MODE", "二次审核模式无效")
	}
	if cfg.TimeoutMS <= 0 || cfg.TimeoutMS > maxSecondaryReviewTimeoutMS {
		return infraerrors.BadRequest("INVALID_SECONDARY_REVIEW_TIMEOUT", "二次审核超时时间必须在 1-30000 毫秒之间")
	}
	if math.IsNaN(cfg.ReviewThreshold) || math.IsInf(cfg.ReviewThreshold, 0) || cfg.ReviewThreshold < secondaryReviewActionableScoreCutoff || cfg.ReviewThreshold > 1 {
		return infraerrors.BadRequest("INVALID_SECONDARY_REVIEW_THRESHOLD", "二次审核复核阈值必须在 0.5-1 之间")
	}
	if math.IsNaN(cfg.BlockThreshold) || math.IsInf(cfg.BlockThreshold, 0) || cfg.BlockThreshold < 0 || cfg.BlockThreshold > 1 {
		return infraerrors.BadRequest("INVALID_SECONDARY_REVIEW_THRESHOLD", "二次审核拦截阈值必须在 0-1 之间")
	}
	if cfg.ReviewThreshold >= cfg.BlockThreshold {
		return infraerrors.BadRequest("INVALID_SECONDARY_REVIEW_THRESHOLD", "二次审核复核阈值必须低于拦截阈值")
	}
	switch cfg.OnError {
	case ContentModerationSecondaryReviewOnErrorKeywordBlock, ContentModerationSecondaryReviewOnErrorAllowAndLog:
	default:
		return infraerrors.BadRequest("INVALID_SECONDARY_REVIEW_ON_ERROR", "二次审核异常策略无效")
	}
	if len([]rune(cfg.Endpoint)) > maxSecondaryReviewEndpointRunes {
		return infraerrors.BadRequest("INVALID_SECONDARY_REVIEW_ENDPOINT", "二次审核服务地址过长")
	}
	if len([]rune(cfg.Token)) > maxSecondaryReviewTokenRunes {
		return infraerrors.BadRequest("INVALID_SECONDARY_REVIEW_TOKEN", "二次审核访问令牌过长")
	}
	if len([]rune(cfg.ExpectedModelVersion)) > maxSecondaryReviewVersionRunes {
		return infraerrors.BadRequest("INVALID_SECONDARY_REVIEW_MODEL_VERSION", "二次审核模型版本过长")
	}
	if cfg.Mode != ContentModerationSecondaryReviewModeOff || cfg.Endpoint != "" {
		if err := validateSecondaryReviewEndpoint(cfg.Endpoint); err != nil {
			return infraerrors.BadRequest("INVALID_SECONDARY_REVIEW_ENDPOINT", "二次审核服务地址无效")
		}
	}
	return nil
}

func validateSecondaryReviewEndpoint(raw string) error {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(raw))
	if err != nil || parsed == nil || !parsed.IsAbs() || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("secondary review endpoint invalid")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("secondary review endpoint scheme invalid")
	}
	if parsed.EscapedPath() != "" && parsed.EscapedPath() != "/" {
		return errors.New("secondary review endpoint path invalid")
	}
	return nil
}

func secondaryReviewClassifyURL(raw string) (string, error) {
	return secondaryReviewServiceURL(raw, "/v1/classify")
}

func secondaryReviewServiceURL(raw, path string) (string, error) {
	if err := validateSecondaryReviewEndpoint(raw); err != nil {
		return "", err
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	parsed.Path = path
	parsed.RawPath = ""
	return parsed.String(), nil
}

func (s *ContentModerationService) GetSecondaryReviewConfig(ctx context.Context) (*ContentModerationSecondaryReviewConfigView, error) {
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	return secondaryReviewConfigView(cfg.SecondaryReview), nil
}

func (s *ContentModerationService) GetSecondaryReviewStatus(ctx context.Context) (*ContentModerationSecondaryReviewStatus, error) {
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	review := cfg.SecondaryReview
	review.normalize()
	status := &ContentModerationSecondaryReviewStatus{Code: secondaryReviewStatusNotConfigured}
	if review.Endpoint == "" {
		return status, nil
	}

	started := time.Now()
	liveErr := s.probeSecondaryReviewLiveness(ctx, review)
	readyResult, readyErr := s.probeSecondaryReviewReadiness(ctx, review)
	status.LatencyMS = int(time.Since(started).Milliseconds())
	if liveErr != nil {
		status.Code = secondaryReviewErrorCode(liveErr)
		return status, nil
	}
	status.Live = true
	if readyErr != nil {
		status.Code = secondaryReviewErrorCode(readyErr)
		return status, nil
	}
	if readyResult == nil {
		status.Code = secondaryReviewErrorInvalidResponse
		return status, nil
	}
	if readyResult.modelNotReady {
		status.Code = secondaryReviewErrorModelNotReady
		return status, nil
	}
	if !readyResult.ready {
		status.Code = secondaryReviewErrorInvalidResponse
		return status, nil
	}
	status.ActiveModelVersion = secondaryReviewStringPointer(readyResult.activeModelVersion)
	status.PreprocessingVersion = secondaryReviewStringPointer(readyResult.preprocessingVersion)
	if review.ExpectedModelVersion != "" && readyResult.activeModelVersion != review.ExpectedModelVersion {
		status.Code = secondaryReviewStatusModelMismatch
		return status, nil
	}
	status.Ready = true
	status.Code = secondaryReviewStatusReady
	return status, nil
}

func (s *ContentModerationService) UpdateSecondaryReviewConfig(ctx context.Context, input UpdateContentModerationSecondaryReviewConfigInput) (*ContentModerationSecondaryReviewConfigView, error) {
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	if input.TimeoutMS != nil && (*input.TimeoutMS <= 0 || *input.TimeoutMS > maxSecondaryReviewTimeoutMS) {
		return nil, infraerrors.BadRequest("INVALID_SECONDARY_REVIEW_TIMEOUT", "二次审核超时时间必须在 1-30000 毫秒之间")
	}
	review := cfg.SecondaryReview
	if input.Mode != nil {
		review.Mode = *input.Mode
	}
	if input.Endpoint != nil {
		review.Endpoint = *input.Endpoint
	}
	if input.TimeoutMS != nil {
		review.TimeoutMS = *input.TimeoutMS
	}
	if input.ReviewThreshold != nil {
		review.ReviewThreshold = *input.ReviewThreshold
	}
	if input.BlockThreshold != nil {
		review.BlockThreshold = *input.BlockThreshold
	}
	if input.ExpectedModelVersion != nil {
		review.ExpectedModelVersion = *input.ExpectedModelVersion
	}
	if input.OnError != nil {
		review.OnError = *input.OnError
	}
	if input.ClearToken {
		review.Token = ""
	} else if input.Token != nil && strings.TrimSpace(*input.Token) != "" {
		review.Token = *input.Token
	}
	review.normalize()
	if err := validateContentModerationSecondaryReviewConfig(review); err != nil {
		return nil, err
	}
	cfg.SecondaryReview = review
	if err := s.validateConfig(ctx, cfg); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal content moderation config: %w", err)
	}
	if err := s.settingRepo.Set(ctx, SettingKeyContentModerationConfig, string(raw)); err != nil {
		return nil, fmt.Errorf("save content moderation config: %w", err)
	}
	s.replaceRuntimeConfig(cfg, raw)
	return secondaryReviewConfigView(review), nil
}

func secondaryReviewConfigView(cfg ContentModerationSecondaryReviewConfig) *ContentModerationSecondaryReviewConfigView {
	cfg.normalize()
	token := strings.TrimSpace(cfg.Token)
	return &ContentModerationSecondaryReviewConfigView{
		Mode:                 cfg.Mode,
		Endpoint:             cfg.Endpoint,
		TokenConfigured:      token != "",
		TokenMasked:          maskSecretTail(token),
		TimeoutMS:            cfg.TimeoutMS,
		ReviewThreshold:      cfg.ReviewThreshold,
		BlockThreshold:       cfg.BlockThreshold,
		ExpectedModelVersion: cfg.ExpectedModelVersion,
		OnError:              cfg.OnError,
	}
}

func (s *ContentModerationService) probeSecondaryReviewLiveness(ctx context.Context, cfg ContentModerationSecondaryReviewConfig) error {
	response, err := s.callSecondaryReviewHealthEndpoint(ctx, cfg, "/health/live")
	if err != nil {
		return err
	}
	if response.statusCode != http.StatusOK {
		return &secondaryReviewCallError{code: secondaryReviewHTTPStatusErrorCode(response.statusCode)}
	}
	var wire secondaryReviewLiveResponseWire
	if err := decodeSecondaryReviewJSON(response.contentType, response.body, &wire); err != nil || wire.Status != "live" {
		return &secondaryReviewCallError{code: secondaryReviewErrorInvalidResponse, cause: err}
	}
	return nil
}

func (s *ContentModerationService) probeSecondaryReviewReadiness(ctx context.Context, cfg ContentModerationSecondaryReviewConfig) (*secondaryReviewReadyProbeResult, error) {
	response, err := s.callSecondaryReviewHealthEndpoint(ctx, cfg, "/health/ready")
	if err != nil {
		return nil, err
	}
	switch response.statusCode {
	case http.StatusOK:
		var wire secondaryReviewReadyResponseWire
		if err := decodeSecondaryReviewJSON(response.contentType, response.body, &wire); err != nil || wire.Status != "ready" {
			return nil, &secondaryReviewCallError{code: secondaryReviewErrorInvalidResponse, cause: err}
		}
		modelVersion, ok := secondaryReviewRequiredVersion(wire.ActiveModelVersion)
		if !ok {
			return nil, &secondaryReviewCallError{code: secondaryReviewErrorInvalidResponse}
		}
		preprocessingVersion, ok := secondaryReviewRequiredVersion(wire.PreprocessingVersion)
		if !ok {
			return nil, &secondaryReviewCallError{code: secondaryReviewErrorInvalidResponse}
		}
		return &secondaryReviewReadyProbeResult{
			ready:                true,
			activeModelVersion:   modelVersion,
			preprocessingVersion: preprocessingVersion,
		}, nil
	case http.StatusServiceUnavailable:
		var wire secondaryReviewNotReadyResponseWire
		if err := decodeSecondaryReviewJSON(response.contentType, response.body, &wire); err != nil ||
			wire.Status != "not_ready" || wire.Code != secondaryReviewErrorModelNotReady ||
			!secondaryReviewIsJSONNull(wire.ActiveModelVersion) || !secondaryReviewIsJSONNull(wire.PreprocessingVersion) {
			return nil, &secondaryReviewCallError{code: secondaryReviewErrorInvalidResponse, cause: err}
		}
		return &secondaryReviewReadyProbeResult{modelNotReady: true}, nil
	default:
		return nil, &secondaryReviewCallError{code: secondaryReviewHTTPStatusErrorCode(response.statusCode)}
	}
}

type secondaryReviewHealthHTTPResponse struct {
	statusCode  int
	contentType string
	body        []byte
}

func (s *ContentModerationService) callSecondaryReviewHealthEndpoint(ctx context.Context, cfg ContentModerationSecondaryReviewConfig, path string) (*secondaryReviewHealthHTTPResponse, error) {
	targetURL, err := secondaryReviewServiceURL(cfg.Endpoint, path)
	if err != nil {
		return nil, &secondaryReviewCallError{code: secondaryReviewErrorInvalidResponse, cause: err}
	}
	reqCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.TimeoutMS)*time.Millisecond)
	defer cancel()
	reqCtx = servertiming.WithDependencyModule(reqCtx, "secondary_review_health")
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, &secondaryReviewCallError{code: secondaryReviewErrorInvalidResponse, cause: err}
	}
	req.Header.Set("Accept", "application/json")
	client := s.intentHTTPClient
	if client == nil {
		client = newSecondaryReviewHTTPClient()
	}
	resp, err := client.Do(req)
	if err != nil {
		code := secondaryReviewErrorUnavailable
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(reqCtx.Err(), context.DeadlineExceeded) {
			code = secondaryReviewErrorTimeout
		}
		return nil, &secondaryReviewCallError{code: code, cause: err}
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSecondaryReviewResponseBytes+1))
	if err != nil || len(body) > maxSecondaryReviewResponseBytes {
		return nil, &secondaryReviewCallError{code: secondaryReviewErrorInvalidResponse, cause: err}
	}
	return &secondaryReviewHealthHTTPResponse{
		statusCode:  resp.StatusCode,
		contentType: resp.Header.Get("Content-Type"),
		body:        body,
	}, nil
}

func secondaryReviewRequiredVersion(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || secondaryReviewIsJSONNull(raw) {
		return "", false
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	value = strings.TrimSpace(value)
	return value, value != "" && len([]rune(value)) <= maxSecondaryReviewVersionRunes
}

func secondaryReviewIsJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func secondaryReviewStringPointer(value string) *string {
	copy := value
	return &copy
}

func (s *ContentModerationService) TestSecondaryReview(ctx context.Context, input TestContentModerationSecondaryReviewInput) (*TestContentModerationSecondaryReviewResult, error) {
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	review := cfg.SecondaryReview
	review.normalize()
	if err := validateSecondaryReviewEndpoint(review.Endpoint); err != nil {
		return nil, infraerrors.BadRequest("INVALID_SECONDARY_REVIEW_ENDPOINT", "请先配置有效的二次审核服务地址")
	}
	text := trimRunes(normalizeContentModerationText(input.Text), maxModerationInputRunes)
	if text == "" {
		return nil, infraerrors.BadRequest("INVALID_SECONDARY_REVIEW_TEST_TEXT", "二次审核测试文本不能为空")
	}
	keyword := trimRunes(strings.TrimSpace(input.MatchedKeyword), maxContentModerationBlockedKeywordRunes)
	if keyword == "" {
		keyword = "manual_test"
	}
	started := time.Now()
	result, err := s.callSecondaryReview(ctx, review, ContentModerationCheckInput{}, text, keyword)
	latency := int(time.Since(started).Milliseconds())
	if err != nil {
		slog.Warn("content_moderation.secondary_review_test_failed", "error_code", secondaryReviewErrorCode(err), "latency_ms", latency)
		return nil, secondaryReviewAdminError(err)
	}
	return &TestContentModerationSecondaryReviewResult{
		Label:        result.Label,
		Score:        result.Score,
		ModelVersion: result.ModelVersion,
		TraceID:      result.TraceID,
		LatencyMS:    latency,
		WouldReview:  secondaryReviewWouldReview(review, result),
		WouldBlock:   secondaryReviewWouldBlock(review, result),
	}, nil
}

func (s *ContentModerationService) callSecondaryReview(ctx context.Context, cfg ContentModerationSecondaryReviewConfig, input ContentModerationCheckInput, text, keyword string) (*secondaryReviewResponse, error) {
	cfg.normalize()
	if err := validateContentModerationSecondaryReviewConfig(cfg); err != nil {
		return nil, &secondaryReviewCallError{code: "invalid_config", cause: err}
	}
	if cfg.Mode == ContentModerationSecondaryReviewModeEnforce && cfg.ExpectedModelVersion == "" {
		return nil, &secondaryReviewCallError{code: "missing_expected_model_version"}
	}
	classifyURL, err := secondaryReviewClassifyURL(cfg.Endpoint)
	if err != nil {
		return nil, &secondaryReviewCallError{code: "invalid_endpoint", cause: err}
	}
	payload := secondaryReviewRequest{
		SchemaVersion:  secondaryReviewSchemaVersion,
		RequestID:      strings.TrimSpace(input.RequestID),
		Text:           text,
		MatchedKeyword: keyword,
		Context: secondaryReviewContext{
			Protocol: strings.TrimSpace(input.Protocol),
			Endpoint: strings.TrimSpace(input.Endpoint),
			Model:    strings.TrimSpace(input.Model),
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, &secondaryReviewCallError{code: "invalid_request", cause: err}
	}
	reqCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.TimeoutMS)*time.Millisecond)
	defer cancel()
	reqCtx = servertiming.WithDependencyModule(reqCtx, "secondary_review")
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, classifyURL, bytes.NewReader(raw))
	if err != nil {
		return nil, &secondaryReviewCallError{code: "invalid_request", cause: err}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Token)
	}
	var client *http.Client
	if s != nil {
		client = s.intentHTTPClient
	}
	if client == nil {
		client = newSecondaryReviewHTTPClient()
	}
	if s != nil && s.intentBulkhead != nil {
		select {
		case s.intentBulkhead <- struct{}{}:
			defer func() { <-s.intentBulkhead }()
		default:
			return nil, &secondaryReviewCallError{code: secondaryReviewErrorBusy}
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		code := secondaryReviewErrorUnavailable
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(reqCtx.Err(), context.DeadlineExceeded) {
			code = secondaryReviewErrorTimeout
		}
		return nil, &secondaryReviewCallError{code: code, cause: err}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, &secondaryReviewCallError{code: secondaryReviewClassifyErrorCode(resp)}
	}
	var wire secondaryReviewResponseWire
	if err := decodeSecondaryReviewHTTPJSON(resp, &wire); err != nil {
		return nil, &secondaryReviewCallError{code: secondaryReviewErrorInvalidResponse, cause: err}
	}
	if wire.Score == nil {
		return nil, &secondaryReviewCallError{code: secondaryReviewErrorInvalidResponse}
	}
	result := secondaryReviewResponse{
		SchemaVersion: strings.TrimSpace(wire.SchemaVersion),
		Label:         strings.TrimSpace(wire.Label),
		Score:         *wire.Score,
		ModelVersion:  strings.TrimSpace(wire.ModelVersion),
		TraceID:       strings.TrimSpace(wire.TraceID),
	}
	if result.SchemaVersion != secondaryReviewSchemaVersion {
		return nil, &secondaryReviewCallError{code: secondaryReviewErrorInvalidResponse}
	}
	if result.Label != ContentModerationSecondaryReviewLabelBenign && result.Label != ContentModerationSecondaryReviewLabelActionableProbe {
		return nil, &secondaryReviewCallError{code: secondaryReviewErrorInvalidResponse}
	}
	if math.IsNaN(result.Score) || math.IsInf(result.Score, 0) || result.Score < 0 || result.Score > 1 {
		return nil, &secondaryReviewCallError{code: secondaryReviewErrorInvalidResponse}
	}
	expectedLabel := ContentModerationSecondaryReviewLabelBenign
	if result.Score >= secondaryReviewActionableScoreCutoff {
		expectedLabel = ContentModerationSecondaryReviewLabelActionableProbe
	}
	if result.Label != expectedLabel {
		return nil, &secondaryReviewCallError{code: secondaryReviewErrorInvalidResponse}
	}
	if result.ModelVersion == "" || len([]rune(result.ModelVersion)) > maxSecondaryReviewVersionRunes {
		return nil, &secondaryReviewCallError{code: secondaryReviewErrorInvalidResponse}
	}
	if len([]rune(result.TraceID)) > maxSecondaryReviewVersionRunes {
		return nil, &secondaryReviewCallError{code: secondaryReviewErrorInvalidResponse}
	}
	if cfg.ExpectedModelVersion != "" && result.ModelVersion != cfg.ExpectedModelVersion {
		return nil, &secondaryReviewCallError{code: secondaryReviewErrorInvalidResponse}
	}
	return &result, nil
}

func secondaryReviewClassifyErrorCode(resp *http.Response) string {
	if resp == nil {
		return secondaryReviewErrorUnavailable
	}
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxSecondaryReviewResponseBytes))
		return secondaryReviewErrorHTTP401
	case http.StatusForbidden:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxSecondaryReviewResponseBytes))
		return secondaryReviewErrorHTTP403
	case http.StatusTooManyRequests:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxSecondaryReviewResponseBytes))
		return secondaryReviewErrorBusy
	case http.StatusServiceUnavailable:
		var wire secondaryReviewErrorResponseWire
		if err := decodeSecondaryReviewHTTPJSON(resp, &wire); err == nil &&
			wire.SchemaVersion == secondaryReviewSchemaVersion && wire.Error.Code == secondaryReviewErrorModelNotReady {
			return secondaryReviewErrorModelNotReady
		}
		return secondaryReviewErrorUpstream5xx
	default:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxSecondaryReviewResponseBytes))
		return secondaryReviewHTTPStatusErrorCode(resp.StatusCode)
	}
}

func secondaryReviewHTTPStatusErrorCode(statusCode int) string {
	switch {
	case statusCode == http.StatusUnauthorized:
		return secondaryReviewErrorHTTP401
	case statusCode == http.StatusForbidden:
		return secondaryReviewErrorHTTP403
	case statusCode == http.StatusTooManyRequests:
		return secondaryReviewErrorBusy
	case statusCode >= http.StatusBadRequest && statusCode < http.StatusInternalServerError:
		return secondaryReviewErrorUpstream4xx
	case statusCode >= http.StatusInternalServerError:
		return secondaryReviewErrorUpstream5xx
	default:
		return secondaryReviewErrorInvalidResponse
	}
}

func decodeSecondaryReviewHTTPJSON(resp *http.Response, target any) error {
	if resp == nil {
		return errors.New("secondary review response missing")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSecondaryReviewResponseBytes+1))
	if err != nil {
		return err
	}
	if len(body) > maxSecondaryReviewResponseBytes {
		return errors.New("secondary review response too large")
	}
	return decodeSecondaryReviewJSON(resp.Header.Get("Content-Type"), body, target)
}

func decodeSecondaryReviewJSON(contentType string, body []byte, target any) error {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "application/json" {
		return errors.New("secondary review response content type invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("secondary review response has trailing content")
	}
	return nil
}

func newSecondaryReviewHTTPClient() *http.Client {
	baseTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok || baseTransport == nil {
		baseTransport = &http.Transport{}
	}
	transport := baseTransport.Clone()
	transport.Proxy = nil
	return servertiming.InstrumentClient(&http.Client{
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	})
}

func secondaryReviewWouldReview(cfg ContentModerationSecondaryReviewConfig, result *secondaryReviewResponse) bool {
	return result != nil && result.Score >= cfg.ReviewThreshold
}

func secondaryReviewWouldBlock(cfg ContentModerationSecondaryReviewConfig, result *secondaryReviewResponse) bool {
	return result != nil && result.Score >= cfg.BlockThreshold
}

func secondaryReviewErrorCode(err error) string {
	var callErr *secondaryReviewCallError
	if errors.As(err, &callErr) && callErr != nil && callErr.code != "" {
		return callErr.code
	}
	return secondaryReviewErrorUnavailable
}

func secondaryReviewAdminError(err error) error {
	code := secondaryReviewErrorCode(err)
	message := "二次审核服务暂时不可用"
	switch code {
	case secondaryReviewErrorHTTP401:
		message = "二次审核服务认证失败"
	case secondaryReviewErrorHTTP403:
		message = "二次审核服务拒绝访问"
	case secondaryReviewErrorUpstream4xx:
		message = "二次审核服务拒绝了测试请求"
	case secondaryReviewErrorUpstream5xx:
		message = "二次审核服务发生上游错误"
	case secondaryReviewErrorTimeout:
		message = "二次审核服务响应超时"
	case secondaryReviewErrorInvalidResponse:
		message = "二次审核服务返回了无效响应"
	case secondaryReviewErrorBusy:
		message = "二次审核服务当前请求过多"
	case secondaryReviewErrorModelNotReady:
		code = secondaryReviewErrorUnavailable
		message = "二次审核模型尚未就绪"
	default:
		code = secondaryReviewErrorUnavailable
	}
	return infraerrors.ServiceUnavailable("SECONDARY_REVIEW_"+strings.ToUpper(code), message)
}

func (s *ContentModerationService) handleMatchedKeyword(ctx context.Context, input ContentModerationCheckInput, cfg *ContentModerationConfig, content ContentModerationInput, hashText, keyword string) (*ContentModerationDecision, bool) {
	review := cfg.SecondaryReview
	review.normalize()
	switch review.Mode {
	case ContentModerationSecondaryReviewModeShadow:
		decision := s.keywordBlockDecision(input, cfg, content, hashText, keyword, true)
		s.enqueueSecondaryReviewShadow(input, cfg, content, hashText, keyword)
		return decision, false
	case ContentModerationSecondaryReviewModeEnforce:
		started := time.Now()
		result, err := s.callSecondaryReview(ctx, review, input, content.Text, keyword)
		latency := int(time.Since(started).Milliseconds())
		if err != nil {
			errorCode := secondaryReviewErrorCode(err)
			slog.Warn("content_moderation.secondary_review_failed",
				"request_id", input.RequestID,
				"user_id", input.UserID,
				"api_key_id", input.APIKeyID,
				"endpoint", input.Endpoint,
				"protocol", input.Protocol,
				"matched_keyword", keyword,
				"error_code", errorCode,
				"latency_ms", latency,
				"on_error", review.OnError)
			if review.OnError == ContentModerationSecondaryReviewOnErrorAllowAndLog {
				s.enqueueSecondaryReviewLog(input, cfg, content, hashText, keyword, ContentModerationActionIntentError, false, "intent_classifier_error", 0, nil, latency, errorCode, nil, review.OnError, false)
				return nil, true
			}
			s.recordPreBlockSyncMetric(latency, ContentModerationActionIntentErrorBlock)
			s.enqueueSecondaryReviewLog(input, cfg, content, hashText, keyword, ContentModerationActionIntentErrorBlock, true, contentModerationKeywordCategory, 1, map[string]float64{contentModerationKeywordCategory: 1}, latency, errorCode, nil, review.OnError, true)
			return &ContentModerationDecision{
				Allowed: false, Blocked: true, Flagged: true, Message: cfg.BlockMessage, StatusCode: cfg.BlockStatus,
				HighestCategory: contentModerationKeywordCategory, HighestScore: 1,
				CategoryScores: map[string]float64{contentModerationKeywordCategory: 1}, Action: ContentModerationActionIntentErrorBlock,
			}, false
		}
		action := ContentModerationActionIntentAllow
		if secondaryReviewWouldReview(review, result) {
			action = ContentModerationActionIntentReview
		}
		if secondaryReviewWouldBlock(review, result) {
			action = ContentModerationActionIntentBlock
			s.recordPreBlockSyncMetric(latency, action)
			scores := map[string]float64{result.Label: result.Score}
			s.enqueueSecondaryReviewLog(input, cfg, content, hashText, keyword, action, true, result.Label, result.Score, scores, latency, "", result, "none", true)
			slog.Info("content_moderation.secondary_review_block",
				"request_id", input.RequestID,
				"user_id", input.UserID,
				"api_key_id", input.APIKeyID,
				"matched_keyword", keyword,
				"label", result.Label,
				"score", result.Score,
				"model_version", result.ModelVersion,
				"trace_id", result.TraceID,
				"latency_ms", latency)
			return &ContentModerationDecision{
				Allowed: false, Blocked: true, Flagged: true, Message: cfg.BlockMessage, StatusCode: cfg.BlockStatus,
				HighestCategory: result.Label, HighestScore: result.Score, CategoryScores: scores, Action: action,
			}, false
		}
		s.enqueueSecondaryReviewLog(input, cfg, content, hashText, keyword, action, false, result.Label, result.Score, map[string]float64{result.Label: result.Score}, latency, "", result, "none", false)
		slog.Info("content_moderation.secondary_review_allow",
			"request_id", input.RequestID,
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"matched_keyword", keyword,
			"label", result.Label,
			"score", result.Score,
			"model_version", result.ModelVersion,
			"trace_id", result.TraceID,
			"latency_ms", latency,
			"action", action)
		return nil, true
	default:
		return s.keywordBlockDecision(input, cfg, content, hashText, keyword, true), false
	}
}

func (s *ContentModerationService) keywordBlockDecision(input ContentModerationCheckInput, cfg *ContentModerationConfig, content ContentModerationInput, hashText, keyword string, applySideEffects bool) *ContentModerationDecision {
	s.recordPreBlockSyncMetric(0, ContentModerationActionKeywordBlock)
	slog.Info("content_moderation.keyword_block",
		"user_id", input.UserID,
		"api_key_id", input.APIKeyID,
		"group_id", contentModerationLogGroupID(input.GroupID),
		"endpoint", input.Endpoint,
		"protocol", input.Protocol,
		"keyword_blocking_mode", cfg.KeywordBlockingMode,
		"keyword", keyword)
	scores := map[string]float64{contentModerationKeywordCategory: 1}
	log := s.buildLog(input, cfg, ContentModerationActionKeywordBlock, applySideEffects, contentModerationKeywordCategory, 1, scores, content.ExcerptText(), nil, nil, "")
	log.MatchedKeyword = keyword
	s.enqueueRecord(input, cfg, log, hashText, false, applySideEffects)
	return &ContentModerationDecision{
		Allowed: false, Blocked: true, Flagged: true, Message: cfg.BlockMessage, StatusCode: cfg.BlockStatus,
		HighestCategory: contentModerationKeywordCategory, HighestScore: 1, CategoryScores: scores, Action: ContentModerationActionKeywordBlock,
	}
}

func (s *ContentModerationService) enqueueSecondaryReviewShadow(input ContentModerationCheckInput, cfg *ContentModerationConfig, content ContentModerationInput, hashText, keyword string) {
	if s == nil || s.asyncQueue == nil {
		return
	}
	queueSize := defaultContentModerationQueueSize
	if cfg != nil && cfg.QueueSize > 0 {
		queueSize = cfg.QueueSize
	}
	if len(s.asyncQueue) >= queueSize {
		slog.Warn("content_moderation.secondary_review_shadow_queue_full", "user_id", input.UserID, "endpoint", input.Endpoint, "queue_size", queueSize)
		s.asyncDropped.Add(1)
		return
	}
	input.Body = nil
	task := contentModerationTask{
		input:         input,
		content:       content,
		inputHash:     hashText,
		config:        cloneContentModerationConfig(cfg),
		intentKeyword: keyword,
		enqueuedAt:    time.Now(),
	}
	select {
	case s.asyncQueue <- task:
		s.asyncEnqueued.Add(1)
	default:
		slog.Warn("content_moderation.secondary_review_shadow_queue_full", "user_id", input.UserID, "endpoint", input.Endpoint)
		s.asyncDropped.Add(1)
	}
}

func (s *ContentModerationService) runSecondaryReviewShadow(ctx context.Context, task contentModerationTask) {
	cfg := task.config
	if cfg == nil {
		return
	}
	started := time.Now()
	result, err := s.callSecondaryReview(ctx, cfg.SecondaryReview, task.input, task.content.Text, task.intentKeyword)
	latency := int(time.Since(started).Milliseconds())
	queueDelay := int(started.Sub(task.enqueuedAt).Milliseconds())
	if err != nil {
		errorCode := secondaryReviewErrorCode(err)
		log := s.buildSecondaryReviewLog(task.input, cfg, task.content, task.intentKeyword, ContentModerationActionIntentError, false, "intent_classifier_error", 0, nil, latency, errorCode, nil, "none")
		log.QueueDelayMS = &queueDelay
		s.persistContentModerationLog(ctx, cfg, log, task.inputHash, false, false)
		s.asyncErrors.Add(1)
		return
	}
	log := s.buildSecondaryReviewLog(task.input, cfg, task.content, task.intentKeyword, ContentModerationActionIntentShadow, false, result.Label, result.Score, map[string]float64{result.Label: result.Score}, latency, "", result, "none")
	log.QueueDelayMS = &queueDelay
	s.persistContentModerationLog(ctx, cfg, log, task.inputHash, false, false)
	slog.Info("content_moderation.secondary_review_shadow",
		"request_id", task.input.RequestID,
		"user_id", task.input.UserID,
		"api_key_id", task.input.APIKeyID,
		"matched_keyword", task.intentKeyword,
		"label", result.Label,
		"score", result.Score,
		"would_block", secondaryReviewWouldBlock(cfg.SecondaryReview, result),
		"model_version", result.ModelVersion,
		"trace_id", result.TraceID,
		"latency_ms", latency,
		"queue_delay_ms", queueDelay)
}

func (s *ContentModerationService) enqueueSecondaryReviewLog(input ContentModerationCheckInput, cfg *ContentModerationConfig, content ContentModerationInput, hashText, keyword, action string, flagged bool, category string, score float64, scores map[string]float64, latency int, errorCode string, result *secondaryReviewResponse, fallback string, applySideEffects bool) {
	log := s.buildSecondaryReviewLog(input, cfg, content, keyword, action, flagged, category, score, scores, latency, errorCode, result, fallback)
	s.enqueueRecord(input, cfg, log, hashText, false, applySideEffects)
}

func (s *ContentModerationService) buildSecondaryReviewLog(input ContentModerationCheckInput, cfg *ContentModerationConfig, content ContentModerationInput, keyword, action string, flagged bool, category string, score float64, scores map[string]float64, latency int, errorCode string, result *secondaryReviewResponse, fallback string) *ContentModerationLog {
	log := s.buildLog(input, cfg, action, flagged, category, score, scores, content.ExcerptText(), &latency, nil, errorCode)
	log.MatchedKeyword = keyword
	log.ReviewMetadata = secondaryReviewMetadata(cfg.SecondaryReview.Mode, result, fallback)
	log.ThresholdSnapshot = map[string]float64{
		"intent_review": cfg.SecondaryReview.ReviewThreshold,
		"intent_block":  cfg.SecondaryReview.BlockThreshold,
	}
	return log
}

func secondaryReviewMetadata(mode string, result *secondaryReviewResponse, fallback string) map[string]string {
	if fallback == "" {
		fallback = "none"
	}
	metadata := map[string]string{
		"model_version": "",
		"trace_id":      "",
		"review_mode":   mode,
		"fallback":      fallback,
	}
	if result != nil {
		metadata["model_version"] = result.ModelVersion
		metadata["trace_id"] = result.TraceID
	}
	return metadata
}
