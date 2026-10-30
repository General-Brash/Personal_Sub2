package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const serverAutoRetryKey = "server_auto_retry_context"
const serverAutoRetryPreambleLimit = 64 << 10

type serverAutoRetryContextKey struct{}

// ServerAutoRetryTarget contains evidence, never inferred successful usage.
type ServerAutoRetryTarget struct {
	Reason                                       string
	RawHTTPStatus, SemanticStatus                int
	AccountID                                    int64
	UpstreamRequestID, UsageEvidence, StopReason string
	HasSemanticOutput                            bool
	RetryAfter                                   time.Duration
}

// A single logical request owns all dispatches, including Forward re-entry.
type ServerAutoRetryContext struct {
	Config                                                                        ServerAutoRetryConfig
	OriginalContext                                                               context.Context
	OriginalFingerprint                                                           string
	OriginalBody, CompliantBody                                                   []byte
	DispatchCount, RetryCount, SwitchCount                                        int
	Wait                                                                          time.Duration
	AzureRetries, EmptyRetries, SameAccountRetries                                int
	Governed, EmptyEligible, AttemptEligible, CrossAccountSafe, HasSemanticOutput bool
	UsageEvidence                                                                 string
	Last, Pending                                                                 *ServerAutoRetryTarget
	LastAccountID                                                                 int64
	Finalized                                                                     bool
	CurrentAccount                                                                *Account
	GuardStop                                                                     func() bool
	LastUpstreamRequestID                                                         string

	TerminalEventSeen bool
}

func ServerAutoRetryFromContext(c *gin.Context) *ServerAutoRetryContext {
	if c == nil {
		return nil
	}
	if value, ok := c.Get(serverAutoRetryKey); ok {
		if retry, ok := value.(*ServerAutoRetryContext); ok {
			return retry
		}
	}
	if c.Request != nil {
		retry, _ := c.Request.Context().Value(serverAutoRetryContextKey{}).(*ServerAutoRetryContext)
		return retry
	}
	return nil
}

func (s *OpenAIGatewayService) BeginServerAutoRetry(c *gin.Context, body []byte) *ServerAutoRetryContext {
	if existing := ServerAutoRetryFromContext(c); existing != nil {
		return existing
	}
	if c == nil || c.Request == nil {
		return nil
	}
	config := DefaultServerAutoRetryConfig()
	if s != nil && s.settingService != nil {
		config = s.settingService.GetServerAutoRetryConfig(c.Request.Context())
	}
	fingerprint := sha256.Sum256(body)
	retry := &ServerAutoRetryContext{Config: config, OriginalContext: c.Request.Context(),
		OriginalFingerprint: hex.EncodeToString(fingerprint[:]), OriginalBody: bytes.Clone(body), CompliantBody: bytes.Clone(body),
		CrossAccountSafe: serverAutoRetryReplaySafe(body), UsageEvidence: "unknown"}
	path := strings.TrimRight(c.Request.URL.Path, "/")
	retry.EmptyEligible = c.Request.Method == http.MethodPost &&
		(path == "/v1/responses" || path == "/openai/v1/responses" || path == "/responses" || path == "/backend-api/codex/responses") &&
		!isOpenAIResponsesCompactPath(c) && !isOpenAINativeCompactionV2(c) && !isOpenAICompatMessagesBridgeBody(body) && serverAutoRetrySafeTools(body)
	c.Set(serverAutoRetryKey, retry)
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), serverAutoRetryContextKey{}, retry))
	return retry
}

func serverAutoRetrySafeTools(body []byte) bool {
	if IsExplicitImageGenerationIntent("/v1/responses", gjson.GetBytes(body, "model").String(), body) {
		return false
	}
	for _, field := range []string{"task", "task_id", "context_management", "compaction"} {
		if gjson.GetBytes(body, field).Exists() {
			return false
		}
	}
	if gjson.GetBytes(body, "background").Bool() {
		return false
	}
	var safe func(gjson.Result) bool
	safe = func(tools gjson.Result) bool {
		for _, tool := range tools.Array() {
			switch tool.Get("type").String() {
			case "function", "custom":
			case "namespace":
				if !safe(tool.Get("tools")) {
					return false
				}
			default:
				return false
			}
		}
		return true
	}
	return safe(gjson.GetBytes(body, "tools"))
}

func serverAutoRetryReplaySafe(body []byte) bool {
	if gjson.GetBytes(body, "previous_response_id").Exists() {
		return false
	}
	safe := true
	var visit func(gjson.Result)
	visit = func(value gjson.Result) {
		if value.IsObject() {
			switch value.Get("type").String() {
			case "item_reference", "compaction", "function_call_output", "custom_tool_call_output":
				safe = false
			}
			if value.Get("encrypted_content").Exists() {
				safe = false
			}
		}
		if value.IsObject() || value.IsArray() {
			value.ForEach(func(_, child gjson.Result) bool { visit(child); return safe })
		}
	}
	visit(gjson.GetBytes(body, "input"))
	return safe
}

func (r *ServerAutoRetryContext) Matches(c *gin.Context, account *Account) bool {
	if r == nil || account == nil {
		return false
	}
	scope := r.Config.Scope
	if scope.Platform != "" && scope.Platform != account.Platform {
		return false
	}
	containsID := func(ids []int64, id int64) bool {
		if len(ids) == 0 {
			return true
		}
		for _, allowed := range ids {
			if allowed == id {
				return true
			}
		}
		return false
	}
	if !containsID(scope.AccountIDs, account.ID) || !containsID(scope.GroupIDs, getOpenAIGroupIDFromContext(c)) {
		return false
	}
	model := gjson.GetBytes(r.OriginalBody, "model").String()
	if len(scope.Models) != 0 {
		for _, allowed := range scope.Models {
			if allowed == model {
				return true
			}
		}
		return false
	}
	return true
}

func (r *ServerAutoRetryContext) PrepareAttempt(s *OpenAIGatewayService, account *Account, body []byte, imageIntent bool) {
	if r == nil {
		return
	}
	r.CurrentAccount = account
	r.AttemptEligible = r.EmptyEligible && account != nil && account.Platform == PlatformOpenAI &&
		!shouldForwardOpenAIResponsesViaRawChatCompletions(account) && !account.IsAnthropicProtocol() &&
		!s.isAgentIdentityAccount(r.OriginalContext, account) && !imageIntent && serverAutoRetrySafeTools(body) &&
		(!account.IsOpenAIPassthroughEnabled() || account.Type == AccountTypeAPIKey)
}

func (r *ServerAutoRetryContext) BeforeDispatch(c *gin.Context, account *Account) error {
	if r == nil {
		return nil
	}
	if r.DispatchCount > 0 {
		if err := r.OriginalContext.Err(); err != nil {
			return err
		}
	}
	if r.Governed && (r.HasSemanticOutput || r.UsageEvidence == "nonzero") {
		return r.Stop(c, "semantic_output_or_positive_usage")
	}
	if r.Governed && r.RemainingWait() <= 0 {
		return r.Stop(c, "retry_wait_budget_exhausted")
	}
	if r.Governed && r.Last != nil && r.Last.Reason != "azure400" && !r.AttemptEligible {
		return r.Stop(c, "unsupported_replay_route")
	}
	if r.Governed && r.DispatchCount >= 1+r.Config.MaxAdditionalAttempts {
		return r.Stop(c, "dispatch_budget_exhausted")
	}
	if r.Governed && r.LastAccountID != 0 && r.LastAccountID != account.ID && !r.CrossAccountSafe {
		return r.Stop(c, "unsafe_cross_account_context")
	}
	r.DispatchCount++
	if r.Governed && r.LastAccountID != 0 && r.LastAccountID != account.ID {
		r.SwitchCount++
	}
	r.LastAccountID = account.ID
	if r.Governed {
		r.RetryCount++
	}
	if r.Pending != nil {
		if r.Pending.Reason == "azure400" {
			r.AzureRetries++
		} else {
			r.EmptyRetries++
		}
		r.Pending = nil
	}
	if r.Governed {
		r.log(c, "server_auto_retry.retry_attempt", "retry")
	}
	r.HasSemanticOutput, r.UsageEvidence, r.TerminalEventSeen = false, "unknown", false
	return nil
}

func (r *ServerAutoRetryContext) BodyForAttempt(body []byte) []byte {
	if r == nil || r.AzureRetries == 0 && (r.Pending == nil || r.Pending.Reason != "azure400") {
		return body
	}
	// Carry only the existing Azure helper's compliant input, preserving the
	// canonical model and the next account's ordinary request transformations.
	if input := gjson.GetBytes(r.CompliantBody, "input"); input.Exists() {
		if patched, err := sjson.SetRawBytes(body, "input", []byte(input.Raw)); err == nil {
			return patched
		}
	}
	return body
}

func (r *ServerAutoRetryContext) Observe(payload []byte, eventType string) {
	if r == nil {
		return
	}
	evidence := serverAutoRetryUsageEvidence(payload)
	if evidence == "nonzero" || r.UsageEvidence != "nonzero" && evidence == "zero" {
		r.UsageEvidence = evidence
	}
	if eventType != "" && !openAIStreamEventIsPreamble(eventType) && !openAIStreamEventTypeIsTerminal(eventType) && eventType != "error" && eventType != "response.failed" && eventType != "[DONE]" {
		r.HasSemanticOutput = true
	}
	root := gjson.ParseBytes(payload)
	for _, path := range []string{"output", "response.output", "data.output", "data.response.output"} {
		if output := root.Get(path); output.IsArray() && len(output.Array()) > 0 || output.Exists() && output.Type != gjson.Null && !output.IsArray() {
			r.HasSemanticOutput = true
		}
	}
	for _, path := range []string{"output_text", "response.output_text", "refusal", "response.refusal"} {
		if v := root.Get(path); v.Exists() && v.Type != gjson.Null && v.String() != "" {
			r.HasSemanticOutput = true
		}
	}
	if r.HasSemanticOutput && r.GuardStop != nil {
		r.GuardStop()
		r.GuardStop = nil
	}
}

// Retries follow the original client's cancellation until semantic output;
// the initial detached request retains its existing usage-drain behavior.
func (r *ServerAutoRetryContext) GuardUpstream(ctx context.Context) (context.Context, func()) {
	if r == nil || r.DispatchCount == 0 || !r.AttemptEligible && !r.Governed {
		return ctx, func() {}
	}
	guarded, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(r.OriginalContext, cancel)
	r.GuardStop = stop
	return guarded, func() { stop(); cancel() }
}

func (r *ServerAutoRetryContext) NoteResponse(resp *http.Response) {
	if r == nil || resp == nil {
		return
	}
	r.LastUpstreamRequestID = resp.Header.Get("x-request-id")
	if r.Governed && r.Last != nil {
		r.Last.RawHTTPStatus = resp.StatusCode
		r.Last.UpstreamRequestID = r.LastUpstreamRequestID
		if resp.StatusCode >= 400 {
			r.Last.SemanticStatus = resp.StatusCode
		}
	}
}

func (r *ServerAutoRetryContext) NoteFailure(status int) {
	if r != nil && r.Governed && r.Last != nil {
		r.Last.SemanticStatus = status
		r.Last.UpstreamRequestID = r.LastUpstreamRequestID
	}
}

func serverAutoRetryUsageEvidence(payload []byte) string {
	if !gjson.ValidBytes(payload) {
		return "unknown"
	}
	root := gjson.ParseBytes(payload)
	zero := false
	for _, path := range []string{"usage", "response.usage", "data.usage", "data.response.usage"} {
		usage := root.Get(path)
		if !usage.IsObject() {
			continue
		}
		positive, malformed := false, false
		var visit func(gjson.Result)
		visit = func(value gjson.Result) {
			if value.Type == gjson.Number {
				if value.Float() > 0 {
					positive = true
				}
				if value.Float() < 0 {
					malformed = true
				}
				return
			}
			if value.IsObject() || value.IsArray() {
				value.ForEach(func(_, child gjson.Result) bool { visit(child); return true })
				return
			}
			malformed = true
		}
		visit(usage)
		if positive {
			return "nonzero"
		}
		in, out := usage.Get("input_tokens"), usage.Get("output_tokens")
		if !malformed && in.Type == gjson.Number && out.Type == gjson.Number && in.Float() == 0 && out.Float() == 0 {
			zero = true
		}
	}
	for _, path := range []string{"tool_usage", "response.tool_usage", "data.tool_usage", "data.response.tool_usage"} {
		positive := false
		var visit func(gjson.Result)
		visit = func(v gjson.Result) {
			if v.Type == gjson.Number && v.Float() > 0 {
				positive = true
			}
			if v.IsObject() || v.IsArray() {
				v.ForEach(func(_, c gjson.Result) bool { visit(c); return true })
			}
		}
		visit(root.Get(path))
		if positive {
			return "nonzero"
		}
	}
	if zero {
		return "zero"
	}
	return "unknown"
}

func serverAutoRetryRateLimited(status int, payload []byte) bool {
	if !gjson.ValidBytes(payload) {
		return status == http.StatusTooManyRequests
	}
	root := gjson.ParseBytes(payload)
	errorObject := root.Get("response.error")
	if !errorObject.IsObject() {
		errorObject = root.Get("error")
	}
	if !errorObject.IsObject() {
		return status == http.StatusTooManyRequests
	}
	message := strings.ToLower(strings.Join(strings.Fields(errorObject.Get("message").String()), " "))
	identifiers := strings.ToLower(errorObject.Get("code").String() + " " + errorObject.Get("type").String())
	combined := identifiers + " " + message
	for _, marker := range []string{"context_length", "context window", "context_window", "maximum context", "content_policy", "cyber_policy", "policy", "safety", "authentication", "unauthorized", "invalid_api_key", "permission", "forbidden", "insufficient_quota", "quota_exceeded", "billing_hard_limit", "credit balance", "quota has been exhausted", "exceeded your current quota"} {
		if strings.Contains(combined, marker) {
			return false
		}
	}
	if status == http.StatusTooManyRequests {
		return true
	}
	if status != http.StatusOK {
		return false
	}
	if strings.Contains(identifiers, "rate_limit") {
		return true
	}
	for _, phrase := range []string{"exceeded token rate limit", "exceeded the token rate limit", "exceeding token rate limit", "exceeding the token rate limit", "reached the token rate limit", "token rate limit exceeded", "token rate limit reached", "token rate limit has been exceeded", "token rate limit has been reached"} {
		if strings.Contains(message, phrase) {
			return true
		}
	}
	return false
}

func (s *OpenAIGatewayService) serverAutoRetryTarget(c *gin.Context, account *Account, resp *http.Response, payload []byte, reason string, compliant []byte) *UpstreamFailoverError {
	r := ServerAutoRetryFromContext(c)
	if r == nil || reason != "azure400" && !r.AttemptEligible {
		return nil
	}
	status := resp.StatusCode
	if reason == "" {
		if !serverAutoRetryRateLimited(status, payload) {
			return nil
		}
		reason = "rate_limit"
	}
	semantic := http.StatusBadGateway
	if reason == "rate_limit" {
		semantic = http.StatusTooManyRequests
	}
	if reason == "azure400" {
		semantic = http.StatusBadRequest
	}
	target := &ServerAutoRetryTarget{Reason: reason, RawHTTPStatus: status, SemanticStatus: semantic, AccountID: account.ID,
		UpstreamRequestID: resp.Header.Get("x-request-id"), UsageEvidence: r.UsageEvidence, HasSemanticOutput: r.HasSemanticOutput,
		RetryAfter: serverAutoRetryAfter(resp.Header, time.Now())}
	r.Governed, r.Last = true, target
	if compliant != nil {
		r.CompliantBody = bytes.Clone(compliant)
	}
	if !r.Config.Enabled {
		target.StopReason = "disabled"
	} else if !r.Matches(c, account) {
		target.StopReason = "scope_mismatch"
	}
	rule, used := r.Config.EmptyResponse, r.EmptyRetries
	if reason == "azure400" {
		rule, used = r.Config.Azure400, r.AzureRetries
		if used >= 1 {
			target.StopReason = "azure_once_exhausted"
		}
	}
	if target.StopReason == "" && (!rule.Enabled || rule.MaxRetries == 0) {
		target.StopReason = "object_disabled"
	}
	if target.StopReason == "" && used >= rule.MaxRetries {
		target.StopReason = "reason_budget_exhausted"
	}
	if target.StopReason == "" && r.DispatchCount >= 1+r.Config.MaxAdditionalAttempts {
		target.StopReason = "dispatch_budget_exhausted"
	}
	if target.HasSemanticOutput {
		target.StopReason = "semantic_output_observed"
	}
	if target.UsageEvidence == "nonzero" {
		target.StopReason = "positive_usage_observed"
	}
	if GetOpsCyberPolicy(c) != nil {
		target.StopReason = "safety_policy"
	}
	if svc := getBoundErrorPassthroughService(c); svc != nil {
		ruleBody := payload
		if status == http.StatusOK {
			ruleBody = openAIStreamFailedEventPassthroughBody(payload, extractOpenAISSEErrorMessage(payload))
		}
		if svc.MatchRule(account.Platform, semantic, ruleBody) != nil {
			target.StopReason = "admin_passthrough"
		}
	}
	body, _ := json.Marshal(gin.H{"error": gin.H{"type": "upstream_error", "message": "Upstream Responses request failed", "code": reason}})
	if len(payload) > 0 && (reason == "rate_limit" || reason == "azure400") {
		body = openAIStreamFailedEventPassthroughBody(payload, extractOpenAISSEErrorMessage(payload))
	}
	err := &UpstreamFailoverError{StatusCode: semantic, ResponseBody: body, ResponseHeaders: resp.Header.Clone(), RequestScopedTransient: true,
		SafeToFailoverAfterWrite: !r.HasSemanticOutput, ServerAutoRetry: target}
	if target.StopReason != "" {
		err.NextAccountAction = NextAccountStop
	}
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
		ProxyID: opsUpstreamProxyID(account), ProxyName: opsUpstreamProxyName(account), Passthrough: c.GetBool("openai_passthrough"),
		UpstreamStatusCode: status, UpstreamRequestID: target.UpstreamRequestID, Kind: "failover", Reason: reason, Message: reason})
	return err
}

func (r *ServerAutoRetryContext) PreferOther(target *ServerAutoRetryTarget) bool {
	if r == nil || target == nil || !r.CrossAccountSafe || target.Reason == "azure400" {
		return false
	}
	if target.Reason == "rate_limit" {
		return r.Config.RateLimitRetryStrategy == "prefer_other_account"
	}
	switch r.Config.EmptyRetryStrategy {
	case "prefer_other_account":
		return true
	case "same_then_switch":
		return r.SameAccountRetries >= r.Config.SameAccountRetries
	default:
		return false
	}
}

func (r *ServerAutoRetryContext) Plan(c *gin.Context, target *ServerAutoRetryTarget, switched bool) (time.Duration, error) {
	if target.StopReason != "" {
		return 0, r.Stop(c, target.StopReason)
	}
	if err := r.OriginalContext.Err(); err != nil {
		return 0, err
	}
	if r.DispatchCount >= 1+r.Config.MaxAdditionalAttempts {
		return 0, r.Stop(c, "dispatch_budget_exhausted")
	}
	if switched && !r.CrossAccountSafe {
		return 0, r.Stop(c, "unsafe_cross_account_context")
	}
	if !switched && target.Reason != "azure400" && target.Reason != "rate_limit" && r.Config.EmptyRetryStrategy == "same_then_switch" && r.SameAccountRetries >= r.Config.SameAccountRetries {
		return 0, r.Stop(c, "no_eligible_independent_account")
	}
	wait := time.Duration(0)
	if target.Reason != "azure400" && (!switched || target.Reason != "rate_limit") {
		base := r.Config.EmptyBackoffBaseMS
		if target.Reason == "rate_limit" {
			base = r.Config.RateLimitBackoffBaseMS
		}
		local := math.Min(float64(r.Config.BackoffCapMS), float64(base)*math.Pow(2, float64(r.EmptyRetries)))
		wait = time.Duration(local) * time.Millisecond
		if target.Reason == "rate_limit" && r.Config.RespectRetryAfter && target.RetryAfter > wait {
			wait = target.RetryAfter
		}
		if r.Config.JitterMS > 0 {
			jitter := time.Duration(rand.Intn(r.Config.JitterMS+1)) * time.Millisecond
			if jitter > time.Duration(math.MaxInt64)-wait {
				wait = time.Duration(math.MaxInt64)
			} else {
				wait += jitter
			}
		}
	}
	if wait > r.RemainingWait() {
		return 0, r.Stop(c, "retry_wait_budget_exhausted")
	}
	if !switched && target.Reason != "azure400" && target.Reason != "rate_limit" {
		r.SameAccountRetries++
	}
	r.Pending = target
	return wait, nil
}

func (r *ServerAutoRetryContext) RemainingWait() time.Duration {
	if r == nil {
		return 0
	}
	remaining := time.Duration(r.Config.MaxTotalRetryWaitMS)*time.Millisecond - r.Wait
	if remaining < 0 {
		return 0
	}
	return remaining
}

func (r *ServerAutoRetryContext) Sleep(c *gin.Context, delay time.Duration) error {
	if delay <= 0 {
		return r.OriginalContext.Err()
	}
	if delay > r.RemainingWait() {
		return r.Stop(c, "retry_wait_budget_exhausted")
	}
	start := time.Now()
	timer := time.NewTimer(delay)
	defer timer.Stop()
	defer func() { r.Wait += time.Since(start) }()
	select {
	case <-r.OriginalContext.Done():
		return r.OriginalContext.Err()
	case <-timer.C:
		return nil
	}
}

// Bound only retry scheduling and slot waits, never normal generation.
func (r *ServerAutoRetryContext) SelectionContext(ctx context.Context) (context.Context, func()) {
	if r == nil || !r.Governed {
		return ctx, func() {}
	}
	start := time.Now()
	bounded, cancel := context.WithTimeout(ctx, r.RemainingWait())
	return bounded, func() { cancel(); r.Wait += time.Since(start) }
}

func (r *ServerAutoRetryContext) Stop(c *gin.Context, reason string) *UpstreamFailoverError {
	if r.Last == nil {
		r.Last = &ServerAutoRetryTarget{Reason: "retry_budget", SemanticStatus: http.StatusBadGateway, UsageEvidence: r.UsageEvidence}
	}
	r.Last.StopReason = reason
	body, _ := json.Marshal(gin.H{"error": gin.H{"type": "upstream_error", "message": "Server auto retry stopped", "code": r.Last.Reason}})
	return &UpstreamFailoverError{StatusCode: r.Last.SemanticStatus, ResponseBody: body, NextAccountAction: NextAccountStop,
		SafeToFailoverAfterWrite: !r.HasSemanticOutput, ServerAutoRetry: r.Last}
}

func (r *ServerAutoRetryContext) Final(c *gin.Context, success bool, stop string) {
	if r == nil || !r.Governed || r.Finalized {
		return
	}
	r.Finalized = true
	outcome := "failed"
	if success {
		outcome = "success"
		if r.Last != nil {
			r.Last.SemanticStatus = http.StatusOK
			r.Last.RawHTTPStatus = http.StatusOK
			r.Last.StopReason = ""
			r.Last.UpstreamRequestID = r.LastUpstreamRequestID
		}
	}
	if stop != "" && r.Last != nil {
		r.Last.StopReason = stop
	}
	c.Set("server_auto_retry_final", r.fields(c, outcome))
	r.log(c, "server_auto_retry.final", outcome)
}

func (r *ServerAutoRetryContext) fields(c *gin.Context, outcome string) map[string]any {
	target := r.Last
	if target == nil {
		target = &ServerAutoRetryTarget{}
	}
	requestID, _ := r.OriginalContext.Value(ctxkey.RequestID).(string)
	return map[string]any{"request_id": requestID, "reason": target.Reason, "raw_http_status": target.RawHTTPStatus,
		"semantic_status": target.SemanticStatus, "account_id": r.LastAccountID, "dispatch_count": r.DispatchCount,
		"retry_count": r.RetryCount, "switch_count": r.SwitchCount, "wait_ms": r.Wait.Milliseconds(),
		"response_committed": c != nil && c.Writer != nil && c.Writer.Written(), "has_semantic_output": r.HasSemanticOutput,
		"usage_evidence": r.UsageEvidence, "stop_reason": target.StopReason, "outcome": outcome, "upstream_request_id": target.UpstreamRequestID}
}

func (r *ServerAutoRetryContext) log(c *gin.Context, message, outcome string) {
	fields := r.fields(c, outcome)
	args := make([]any, 0, 2*len(fields))
	for key, value := range fields {
		args = append(args, key, value)
	}
	slog.InfoContext(r.OriginalContext, message, args...)
}

func serverAutoRetryAfter(headers http.Header, now time.Time) time.Duration {
	wait := time.Duration(0)
	for _, value := range headers.Values("Retry-After") {
		value = strings.TrimSpace(value)
		seconds, err := strconv.ParseFloat(value, 64)
		if errors.Is(err, strconv.ErrRange) && math.IsInf(seconds, 1) {
			wait = time.Duration(math.MaxInt64)
		} else if err == nil && seconds >= 0 && !math.IsInf(seconds, 0) && !math.IsNaN(seconds) {
			if seconds >= float64(math.MaxInt64)/float64(time.Second) {
				wait = time.Duration(math.MaxInt64)
			} else {
				retryAfter := time.Duration(seconds * float64(time.Second))
				if retryAfter > wait {
					wait = retryAfter
				}
			}
		} else if at, err := http.ParseTime(value); err == nil && at.After(now) {
			retryAfter := at.Sub(now)
			if retryAfter > wait {
				wait = retryAfter
			}
		}
	}
	for _, value := range headers.Values("retry-after-ms") {
		ms, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if errors.Is(err, strconv.ErrRange) && math.IsInf(ms, 1) {
			wait = time.Duration(math.MaxInt64)
		} else if err == nil && ms >= 0 && !math.IsInf(ms, 0) && !math.IsNaN(ms) {
			if ms >= float64(math.MaxInt64)/float64(time.Millisecond) {
				wait = time.Duration(math.MaxInt64)
			} else {
				retryAfter := time.Duration(ms * float64(time.Millisecond))
				if retryAfter > wait {
					wait = retryAfter
				}
			}
		}
	}
	return wait
}

func ServerAutoRetryIndependentAccount(first, next *Account) bool {
	if first == nil || next == nil || first.ID == next.ID || next.Platform != PlatformOpenAI || shouldForwardOpenAIResponsesViaRawChatCompletions(next) {
		return false
	}
	a, _ := url.Parse(first.GetOpenAIBaseURL())
	b, _ := url.Parse(next.GetOpenAIBaseURL())
	if a != nil && b != nil && strings.EqualFold(a.Hostname(), b.Hostname()) && strings.Contains(strings.ToLower(a.Hostname()), ".openai.azure.") {
		return false
	}
	firstOwner, nextOwner := first.ID, next.ID
	if first.ParentAccountID != nil {
		firstOwner = *first.ParentAccountID
	}
	if next.ParentAccountID != nil {
		nextOwner = *next.ParentAccountID
	}
	if firstOwner == nextOwner {
		return false
	}
	if first.Type == AccountTypeOAuth && next.Type == AccountTypeOAuth {
		return first.GetCredential("access_token") != next.GetCredential("access_token") && first.GetCredential("refresh_token") != next.GetCredential("refresh_token")
	}
	return first.GetOpenAIBaseURL() != next.GetOpenAIBaseURL() || first.GetOpenAIProtocolAPIKey() != next.GetOpenAIProtocolAPIKey()
}

func (s *OpenAIGatewayService) ServerAutoRetrySameAccountSelection(selection *AccountSelectionResult, remaining time.Duration) *AccountSelectionResult {
	if selection == nil {
		return nil
	}
	next := *selection
	next.Acquired, next.ReleaseFunc = false, nil
	if next.WaitPlan != nil {
		plan := *next.WaitPlan
		plan.Timeout = min(plan.Timeout, remaining)
		next.WaitPlan = &plan
	} else {
		cfg := s.schedulingConfig()
		next.WaitPlan = &AccountWaitPlan{AccountID: next.Account.ID, MaxConcurrency: next.Account.Concurrency, Timeout: min(remaining, cfg.StickySessionWaitTimeout), MaxWaiting: cfg.StickySessionMaxWaiting}
	}
	return &next
}

func (s *OpenAIGatewayService) serverAutoRetryHTTPError(c *gin.Context, account *Account, resp *http.Response, body, payload []byte, upstreamURL *url.URL) (*UpstreamFailoverError, error) {
	r := ServerAutoRetryFromContext(c)
	if r == nil {
		return nil, nil
	}
	r.Observe(payload, "")
	compliant, changed, err := normalizeOpenAIResponsesAzureResourceRetryBody(resp.StatusCode, body, payload, upstreamURL)
	if err != nil {
		return nil, err
	}
	if !changed && r.AzureRetries > 0 {
		_, changed, err = normalizeOpenAIResponsesAzureResourceRetryBody(resp.StatusCode, r.OriginalBody, payload, upstreamURL)
		if err != nil {
			return nil, err
		}
	}
	if changed {
		return s.serverAutoRetryTarget(c, account, resp, payload, "azure400", compliant), nil
	}
	return s.serverAutoRetryTarget(c, account, resp, payload, "", nil), nil
}

func (s *OpenAIGatewayService) serverAutoRetryStreamEvent(c *gin.Context, account *Account, resp *http.Response, payload []byte, eventType string) *UpstreamFailoverError {
	r := ServerAutoRetryFromContext(c)
	if r == nil || !r.AttemptEligible {
		return nil
	}
	if openAIStreamEventTypeIsTerminal(eventType) {
		r.TerminalEventSeen = true
	}
	if eventType == "" && len(bytes.TrimSpace(payload)) != 0 && string(bytes.TrimSpace(payload)) != "[DONE]" {
		r.HasSemanticOutput = true
	}
	r.Observe(payload, eventType)
	if string(bytes.TrimSpace(payload)) == "[DONE]" && !r.TerminalEventSeen && !r.HasSemanticOutput {
		return s.serverAutoRetryTarget(c, account, resp, nil, "no_output_terminal_missing", nil)
	}
	if hit, _, _ := detectOpenAICyberPolicy(payload); hit {
		usage, _ := extractOpenAIUsageFromJSONBytes(payload)
		markOpenAICyberPolicyEvent(c, payload, resp.StatusCode, &usage)
		r.HasSemanticOutput = true
		if r.GuardStop != nil {
			r.GuardStop()
			r.GuardStop = nil
		}
		return nil
	}
	if eventType == "error" || eventType == "response.failed" {
		if target := s.serverAutoRetryTarget(c, account, resp, payload, "", nil); target != nil {
			return target
		}
		if !openAIStreamFailedEventShouldFailover(payload, extractOpenAISSEErrorMessage(payload)) {
			// Non-retryable errors are emitted, so later empty terminals must not replay them.
			r.HasSemanticOutput = true
			if r.GuardStop != nil {
				r.GuardStop()
				r.GuardStop = nil
			}
		}
		return nil
	}
	if eventType == "response.completed" || eventType == "response.done" {
		if gjson.GetBytes(payload, "response").IsObject() && serverAutoRetryUsageEvidence(payload) == "zero" && r.UsageEvidence == "zero" && !r.HasSemanticOutput &&
			!serverAutoRetryHasError(payload) {
			return s.serverAutoRetryTarget(c, account, resp, payload, "zero_usage_empty", nil)
		}
		if !r.HasSemanticOutput && serverAutoRetryUsageEvidence(payload) == "unknown" && !serverAutoRetryHasError(payload) {
			err := s.serverAutoRetryTarget(c, account, resp, payload, "empty_usage_unknown", nil)
			err.ServerAutoRetry.StopReason = "usage_unknown"
			err.NextAccountAction = NextAccountStop
			return err
		}
	}
	return nil
}

// Unary responses already use bounded body reads. Inspect their real SSE
// frames, not arbitrary business text, before the existing JSON conversion.
func (s *OpenAIGatewayService) serverAutoRetryUnary(c *gin.Context, resp *http.Response, body []byte) *UpstreamFailoverError {
	r := ServerAutoRetryFromContext(c)
	if r == nil || !r.AttemptEligible {
		return nil
	}
	account := r.CurrentAccount
	if len(bytes.TrimSpace(body)) == 0 {
		return s.serverAutoRetryTarget(c, account, resp, nil, "no_output_terminal_missing", nil)
	}
	if bodyHasSSEFraming(body) || isEventStreamResponse(resp.Header) {
		var target *UpstreamFailoverError
		terminal := false
		forEachOpenAISSEDataPayload(string(body), func(payload []byte) {
			eventType := strings.TrimSpace(gjson.GetBytes(payload, "type").String())
			if openAIStreamEventTypeIsTerminal(eventType) {
				terminal = true
			}
			r.Observe(payload, eventType)
			if target == nil {
				target = s.serverAutoRetryStreamEvent(c, account, resp, payload, eventType)
			}
		})
		// Evidence from later frames can veto an earlier error frame's replay.
		if target != nil {
			target.ServerAutoRetry.HasSemanticOutput = r.HasSemanticOutput
			target.ServerAutoRetry.UsageEvidence = r.UsageEvidence
			if r.HasSemanticOutput || r.UsageEvidence == "nonzero" {
				target.ServerAutoRetry.StopReason = "semantic_output_or_positive_usage"
				target.NextAccountAction = NextAccountStop
			}
			return target
		}
		if !terminal && !r.HasSemanticOutput {
			return s.serverAutoRetryTarget(c, account, resp, nil, "no_output_terminal_missing", nil)
		}
		return nil
	}
	r.Observe(body, "")
	if target := s.serverAutoRetryTarget(c, account, resp, body, "", nil); target != nil {
		return target
	}
	status := gjson.GetBytes(body, "status").String()
	if status == "" {
		for _, path := range []string{"response.status", "data.status", "data.response.status"} {
			if v := gjson.GetBytes(body, path); v.Type == gjson.String {
				status = v.String()
				break
			}
		}
	}
	if status == "completed" && !r.HasSemanticOutput && serverAutoRetryUsageEvidence(body) == "zero" && r.UsageEvidence == "zero" && !serverAutoRetryHasError(body) {
		return s.serverAutoRetryTarget(c, account, resp, body, "zero_usage_empty", nil)
	}
	if status == "completed" && !r.HasSemanticOutput && serverAutoRetryUsageEvidence(body) == "unknown" && !serverAutoRetryHasError(body) {
		err := s.serverAutoRetryTarget(c, account, resp, body, "empty_usage_unknown", nil)
		err.ServerAutoRetry.StopReason = "usage_unknown"
		err.NextAccountAction = NextAccountStop
		return err
	}
	return nil
}

func serverAutoRetryHasError(payload []byte) bool {
	for _, path := range []string{"error", "response.error"} {
		if v := gjson.GetBytes(payload, path); v.Exists() && v.Type != gjson.Null {
			return true
		}
	}
	return false
}

func (s *OpenAIGatewayService) RefreshServerAutoRetrySelection(c *gin.Context, selection *AccountSelectionResult, groupID *int64, model, platform string, compact bool, capability OpenAIEndpointCapability) bool {
	if selection == nil || selection.Account == nil {
		return false
	}
	ctx := ContextWithSelectionProfitGate(c.Request.Context(), selection)
	previous := selection.Account
	account := s.recheckSelectedOpenAIAccountFromDB(ctx, previous, groupID, platform, model, compact, capability)
	if account == nil || !s.openAIAccountMatchesSchedulingGroup(account, groupID) || !account.IsModelSupported(model) {
		return false
	}
	if s.needsUpstreamChannelRestrictionCheck(ctx, groupID) && groupID != nil && s.isUpstreamModelRestrictedByChannel(ctx, *groupID, account, model, compact) {
		return false
	}
	if account.GetOpenAIBaseURL() != previous.GetOpenAIBaseURL() || account.GetOpenAIProtocolAPIKey() != previous.GetOpenAIProtocolAPIKey() {
		return false
	}
	selection.Account = account
	if selection.WaitPlan == nil && !selection.Acquired {
		cfg := s.schedulingConfig()
		selection.WaitPlan = &AccountWaitPlan{AccountID: account.ID, MaxConcurrency: account.Concurrency, Timeout: cfg.StickySessionWaitTimeout, MaxWaiting: cfg.StickySessionMaxWaiting}
	}
	return true
}
