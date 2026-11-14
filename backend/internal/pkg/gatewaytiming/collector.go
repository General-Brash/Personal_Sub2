// Package gatewaytiming collects bounded, request-scoped gateway diagnostics.
// It retains only timestamps, numeric values, and whitelisted labels, not the
// request, its context or body, an account object, or transport error text.
package gatewaytiming

import (
	"context"
	"strings"
	"sync"
	"time"
)

const (
	maxPhases     = 32
	maxMilestones = 32
	maxAttempts   = 32
	maxLabelBytes = 64
)

type contextKey struct{}

type phaseSample struct {
	TotalMS float64 `json:"total_ms"`
	Count   uint64  `json:"count"`
}

type requestBodySample struct {
	RawBytes     *int64 `json:"raw_bytes,omitempty"`
	DecodedBytes *int64 `json:"decoded_bytes,omitempty"`
	Encoding     string `json:"encoding"`
	Complete     bool   `json:"complete"`
	Observations uint64 `json:"observations"`
}

type truncationSample struct {
	Phases                  uint64 `json:"phases"`
	Milestones              uint64 `json:"milestones"`
	HTTPAttempts            uint64 `json:"http_attempts"`
	RejectedPhaseLabels     uint64 `json:"rejected_phase_labels"`
	RejectedMilestoneLabels uint64 `json:"rejected_milestone_labels"`
}

type collectorSnapshot struct {
	SchemaVersion        int                    `json:"schema_version"`
	TotalMS              *float64               `json:"total_ms,omitempty"`
	FirstVisibleOffsetMS *float64               `json:"first_visible_offset_ms,omitempty"`
	FirstVisibleAttempt  *uint64                `json:"first_visible_attempt,omitempty"`
	AfterFirstVisibleMS  *float64               `json:"after_first_visible_ms,omitempty"`
	Phases               map[string]phaseSample `json:"phases,omitempty"`
	Milestones           map[string]float64     `json:"milestones,omitempty"`
	RequestBody          *requestBodySample     `json:"request_body,omitempty"`
	HTTPAttemptCount     uint64                 `json:"http_attempt_count"`
	HTTPAttempts         []httpAttemptSnapshot  `json:"http_attempts,omitempty"`
	Truncated            truncationSample       `json:"truncated"`
}

// Collector is safe for concurrent use, including callbacks after Do returns.
type Collector struct {
	startedAt time.Time
	mu        sync.Mutex

	phases              map[string]phaseSample
	milestones          map[string]time.Time
	requestBody         *requestBodySample
	firstVisibleAt      time.Time
	firstVisibleAttempt uint64
	attemptCount        uint64
	attempts            []*httpAttempt
	latestAttempt       *httpAttempt
	truncated           truncationSample
}

// New starts a collector at startedAt; a zero time uses the current time.
func New(startedAt time.Time) *Collector {
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	return &Collector{
		startedAt:  startedAt,
		phases:     make(map[string]phaseSample),
		milestones: make(map[string]time.Time),
	}
}

// WithCollector attaches c without changing cancellation or deadlines.
func WithCollector(ctx context.Context, c *Collector) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, contextKey{}, c)
}

// FromContext returns nil when collection is not active.
func FromContext(ctx context.Context) *Collector {
	if ctx == nil {
		return nil
	}
	c, _ := ctx.Value(contextKey{}).(*Collector)
	return c
}

// Observe accumulates elapsed time and a count on its first completion. Totals
// are sums, not interval unions: nested or concurrent phases may overlap.
func Observe(ctx context.Context, phase string) func() {
	c := FromContext(ctx)
	if c == nil {
		return noop
	}
	phase = safeLabel(phase)
	if phase == "" {
		c.mu.Lock()
		c.truncated.RejectedPhaseLabels++
		c.mu.Unlock()
		return noop
	}
	startedAt := time.Now()
	var once sync.Once
	return func() {
		once.Do(func() {
			elapsed := durationMS(startedAt, time.Now())
			if elapsed == nil {
				return
			}
			c.mu.Lock()
			defer c.mu.Unlock()
			sample, exists := c.phases[phase]
			if !exists && len(c.phases) >= maxPhases {
				c.truncated.Phases++
				return
			}
			sample.TotalMS += *elapsed
			sample.Count++
			c.phases[phase] = sample
		})
	}
}

// Mark keeps only the first occurrence of a whitelisted milestone.
func Mark(ctx context.Context, milestone string) {
	c := FromContext(ctx)
	if c == nil {
		return
	}
	milestone = safeLabel(milestone)
	if milestone == "first_visible" {
		MarkFirstVisible(ctx)
		return
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if milestone == "" {
		c.truncated.RejectedMilestoneLabels++
		return
	}
	if _, exists := c.milestones[milestone]; exists {
		return
	}
	if len(c.milestones) >= maxMilestones {
		c.truncated.Milestones++
		return
	}
	c.milestones[milestone] = now
}

// RecordRequestBody stores the latest size observation, never body contents.
// rawBytes counts inbound Body reads, not ContentLength; negative sizes are
// unknown. Repeated calls replace the observation rather than double-count it.
func RecordRequestBody(ctx context.Context, rawBytes, decodedBytes int64, encoding string, complete bool) {
	c := FromContext(ctx)
	if c == nil {
		return
	}
	sample := &requestBodySample{
		RawBytes:     knownSize(rawBytes),
		DecodedBytes: knownSize(decodedBytes),
		Encoding:     safeEncoding(encoding),
		Complete:     complete,
		Observations: 1,
	}
	c.mu.Lock()
	if c.requestBody != nil {
		sample.Observations += c.requestBody.Observations
	}
	c.requestBody = sample
	c.mu.Unlock()
}

// MarkFirstVisible records the request's first semantic output and the first
// such output on the latest attempt. It does not change any FirstTokenMs field.
func MarkFirstVisible(ctx context.Context) {
	c := FromContext(ctx)
	if c == nil {
		return
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	a := c.latestAttempt
	if c.firstVisibleAt.IsZero() {
		c.firstVisibleAt = now
		if len(c.milestones) < maxMilestones {
			c.milestones["first_visible"] = now
		} else {
			c.truncated.Milestones++
		}
		if a != nil {
			c.firstVisibleAttempt = a.number
		}
	}
	if a != nil {
		a.mu.Lock()
		if a.firstVisibleAt.IsZero() {
			a.firstVisibleAt = now
		}
		a.mu.Unlock()
	}
}

// Snapshot copies all state into independently owned JSON-serializable values.
// Missing or out-of-order time intervals are omitted, never clamped to zero.
func Snapshot(ctx context.Context, endedAt time.Time) any {
	c := FromContext(ctx)
	if c == nil {
		return nil
	}
	if endedAt.IsZero() {
		endedAt = time.Now()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	snapshot := collectorSnapshot{
		SchemaVersion:        1,
		TotalMS:              durationMS(c.startedAt, endedAt),
		FirstVisibleOffsetMS: durationMS(c.startedAt, c.firstVisibleAt),
		Phases:               make(map[string]phaseSample, len(c.phases)),
		Milestones:           make(map[string]float64, len(c.milestones)),
		HTTPAttemptCount:     c.attemptCount,
		HTTPAttempts:         make([]httpAttemptSnapshot, 0, len(c.attempts)),
		Truncated:            c.truncated,
	}
	// Includes remaining generation, stream forwarding and cleanup, not just
	// network download. Missing or out-of-order boundaries remain unknown.
	snapshot.AfterFirstVisibleMS = durationMS(c.firstVisibleAt, endedAt)
	if c.firstVisibleAttempt != 0 {
		number := c.firstVisibleAttempt
		snapshot.FirstVisibleAttempt = &number
	}
	for label, phase := range c.phases {
		snapshot.Phases[label] = phase
	}
	for label, at := range c.milestones {
		if offset := durationMS(c.startedAt, at); offset != nil {
			snapshot.Milestones[label] = *offset
		}
	}
	if c.requestBody != nil {
		body := *c.requestBody
		body.RawBytes = copyInt64(body.RawBytes)
		body.DecodedBytes = copyInt64(body.DecodedBytes)
		snapshot.RequestBody = &body
	}
	for _, a := range c.attempts {
		snapshot.HTTPAttempts = append(snapshot.HTTPAttempts, a.snapshot())
	}
	return snapshot
}

func noop() {}

func durationMS(startedAt, endedAt time.Time) *float64 {
	if startedAt.IsZero() || endedAt.IsZero() || endedAt.Before(startedAt) {
		return nil
	}
	value := float64(endedAt.Sub(startedAt)) / float64(time.Millisecond)
	return &value
}

func knownSize(value int64) *int64 {
	if value < 0 {
		return nil
	}
	return &value
}

func copyInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

// Encoding reports the observed whitelisted token, not decoder acceptance.
func safeEncoding(encoding string) string {
	if len(encoding) > maxLabelBytes {
		return "other"
	}
	encoding = strings.ToLower(strings.TrimSpace(encoding))
	switch encoding {
	case "", "identity":
		return "identity"
	case "gzip", "x-gzip", "deflate", "zstd", "x-zstd", "br", "brotli", "compress", "x-compress":
		return strings.Clone(encoding)
	default:
		return "other"
	}
}

// Labels are a closed vocabulary so accidental user text cannot enter logs.
func safeLabel(label string) string {
	if len(label) > maxLabelBytes {
		return ""
	}
	label = strings.ToLower(strings.TrimSpace(label))
	switch label {
	case "handler_entered", "body_read_done", "first_forward_started", "first_visible",
		"body_read", "body_decode",
		"body_normalize", "body_reread", "body_redecode", "preflight",
		"user_slot_acquire", "account_slot_acquire", "billing_check", "account_selection",
		"forward", "retry_backoff":
		return strings.Clone(label)
	default:
		return ""
	}
}
