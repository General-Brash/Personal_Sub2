package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const SettingKeyModelDefaultPricing = "model_default_pricing_overrides"
const maxDefaultPricingModels = 10000
const maxDefaultPricingBytes = 8 << 20
const defaultPricingRefreshInterval = 3 * time.Second

var (
	ErrDefaultPricingVersionConflict  = infraerrors.Conflict("MODEL_DEFAULT_PRICING_VERSION_CONFLICT", "Default pricing changed; reload and review your changes before saving again")
	ErrDefaultPricingInvalid          = infraerrors.BadRequest("MODEL_DEFAULT_PRICING_INVALID", "Invalid default pricing standard")
	ErrDefaultPricingResetUnavailable = infraerrors.Conflict("MODEL_DEFAULT_PRICING_RESET_UNAVAILABLE", "Cannot remove the only valid pricing standard; disable traffic before retiring this model")
)

// DefaultPricingFields is a sparse administrator layer. nil means inherit, not free.
// Money is USD/token for token fields, USD/request, USD/image, or USD/second.
// Advanced catalog policies are intentionally not editable through this contract.
type DefaultPricingFields struct {
	BillingMode       BillingMode `json:"billing_mode,omitempty"`
	InputPrice        *float64    `json:"input_price,omitempty"`
	OutputPrice       *float64    `json:"output_price,omitempty"`
	CacheWritePrice   *float64    `json:"cache_write_price,omitempty"`
	CacheWrite1hPrice *float64    `json:"cache_write_1h_price,omitempty"`
	CacheReadPrice    *float64    `json:"cache_read_price,omitempty"`
	ImageInputPrice   *float64    `json:"image_input_price,omitempty"`
	ImageOutputPrice  *float64    `json:"image_output_price,omitempty"`
	PerRequestPrice   *float64    `json:"per_request_price,omitempty"`
	ImagePrice1K      *float64    `json:"image_price_1k,omitempty"`
	ImagePrice2K      *float64    `json:"image_price_2k,omitempty"`
	ImagePrice4K      *float64    `json:"image_price_4k,omitempty"`
	VideoPrice480P    *float64    `json:"video_price_480p,omitempty"`
	VideoPrice720P    *float64    `json:"video_price_720p,omitempty"`
	VideoPrice1080P   *float64    `json:"video_price_1080p,omitempty"`
}

var defaultPricingMoneyFields = []string{
	"input_price", "output_price", "cache_write_price", "cache_write_1h_price", "cache_read_price", "image_input_price", "image_output_price",
	"per_request_price", "image_price_1k", "image_price_2k", "image_price_4k", "video_price_480p", "video_price_720p", "video_price_1080p",
}

func DefaultPricingEditableFields() []string {
	return append([]string{"billing_mode"}, defaultPricingMoneyFields...)
}

func (f *DefaultPricingFields) prices() map[string]*float64 {
	return map[string]*float64{
		"input_price": f.InputPrice, "output_price": f.OutputPrice, "cache_write_price": f.CacheWritePrice,
		"cache_write_1h_price": f.CacheWrite1hPrice, "cache_read_price": f.CacheReadPrice, "image_input_price": f.ImageInputPrice,
		"image_output_price": f.ImageOutputPrice, "per_request_price": f.PerRequestPrice,
		"image_price_1k": f.ImagePrice1K, "image_price_2k": f.ImagePrice2K, "image_price_4k": f.ImagePrice4K,
		"video_price_480p": f.VideoPrice480P, "video_price_720p": f.VideoPrice720P, "video_price_1080p": f.VideoPrice1080P,
	}
}

func NormalizeDefaultPricingModel(model string) (string, error) {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" || len(model) > 256 || !utf8.ValidString(model) {
		return "", ErrDefaultPricingInvalid.WithMetadata(map[string]string{"field": "model_id"})
	}
	for _, r := range model {
		if unicode.IsSpace(r) || unicode.IsControl(r) || strings.ContainsRune("*?", r) {
			return "", ErrDefaultPricingInvalid.WithMetadata(map[string]string{"field": "model_id"})
		}
	}
	return model, nil
}

// Only single-model patches are accepted. RawMessage preserves absent/null/value.
type DefaultPricingPatch map[string]json.RawMessage

type defaultPricingSnapshot struct {
	SchemaVersion int                             `json:"schema_version"`
	Revision      uint64                          `json:"revision"`
	Models        map[string]DefaultPricingFields `json:"models"`
}

type DefaultPricingStatus struct {
	Version                string `json:"version"`
	LoadedVersion          string `json:"loaded_version"`
	RefreshError           string `json:"refresh_error,omitempty"`
	RefreshIntervalSeconds int    `json:"refresh_interval_seconds"`
}

type DefaultPricingChange struct {
	snapshot   *defaultPricingSnapshot // committed immutable candidate; not serialized/audited
	ModelID    string                  `json:"model_id"`
	Before     DefaultPricingFields    `json:"before"`
	After      DefaultPricingFields    `json:"after"`
	OldVersion string                  `json:"old_version"`
	NewVersion string                  `json:"new_version"`
}

type DefaultPricingOverrideItem struct {
	ModelID  string               `json:"model_id"`
	Override DefaultPricingFields `json:"override"`
}

type DefaultPricingOverrideList struct {
	DefaultPricingStatus
	Items    []DefaultPricingOverrideItem `json:"items"`
	Total    int                          `json:"total"`
	Page     int                          `json:"page"`
	PageSize int                          `json:"page_size"`
}

// The repository is the only dependency: baseline validation is a pure value
// passed by the shared system-pricing adapter, never a reverse BillingService dependency.
type DefaultModelPricingService struct {
	validationMu   sync.RWMutex
	systemBaseline func(string) DefaultPricingBaseline
	repo           SettingRepository
	snapshot       atomic.Pointer[defaultPricingSnapshot]
	statusMu       sync.Mutex
	refreshError   string
	cancel         context.CancelFunc
	done           chan struct{}
	stopOnce       sync.Once
}

func NewDefaultModelPricingService(repo SettingRepository) *DefaultModelPricingService {
	return &DefaultModelPricingService{repo: repo}
}

func (s *DefaultModelPricingService) Initialize(ctx context.Context) error {
	if err := s.Refresh(ctx); err != nil {
		return err
	}
	pollCtx, cancel := context.WithCancel(context.Background())
	s.cancel, s.done = cancel, make(chan struct{})
	go func() {
		defer close(s.done)
		ticker := time.NewTicker(defaultPricingRefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-pollCtx.Done():
				return
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(pollCtx, 2*time.Second)
				err := s.Refresh(ctx)
				cancel()
				if err != nil && pollCtx.Err() == nil {
					slog.Error("default pricing refresh failed; retaining loaded revision", "error", err)
				}
			}
		}
	}()
	return nil
}

func (s *DefaultModelPricingService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		if s.cancel != nil {
			s.cancel()
			<-s.done
		}
	})
}

func (s *DefaultModelPricingService) read(ctx context.Context) (*defaultPricingSnapshot, string, error) {
	if s == nil || s.repo == nil {
		return nil, "", ErrServiceUnavailable
	}
	values, err := s.repo.GetMultiple(ctx, []string{SettingKeyModelDefaultPricing})
	if err != nil {
		return nil, "", err
	}
	raw := values[SettingKeyModelDefaultPricing]
	snapshot, err := parseDefaultPricingSnapshot(raw)
	return snapshot, raw, err
}

func parseDefaultPricingSnapshot(raw string) (*defaultPricingSnapshot, error) {
	next := &defaultPricingSnapshot{SchemaVersion: 1, Models: map[string]DefaultPricingFields{}}
	if raw == "" {
		return next, nil
	}
	if len(raw) > maxDefaultPricingBytes {
		return nil, fmt.Errorf("default pricing snapshot too large")
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(next); err != nil {
		return nil, fmt.Errorf("decode default pricing: %w", err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("invalid trailing default pricing data")
	}
	if next.SchemaVersion != 1 || next.Revision == 0 || next.Models == nil || len(next.Models) > maxDefaultPricingModels {
		return nil, fmt.Errorf("invalid default pricing snapshot schema, revision or record count")
	}
	for model, fields := range next.Models {
		normalized, err := NormalizeDefaultPricingModel(model)
		if err != nil || normalized != model {
			return nil, fmt.Errorf("invalid default pricing model key %q", model)
		}
		if err := validateDefaultPricingFields(fields); err != nil {
			return nil, fmt.Errorf("invalid default pricing %q: %w", model, err)
		}
	}
	return next, nil
}

func (s *DefaultModelPricingService) publish(next *defaultPricingSnapshot) {
	for {
		previous := s.snapshot.Load()
		if previous != nil && previous.Revision >= next.Revision {
			return
		}
		if s.snapshot.CompareAndSwap(previous, next) {
			return
		}
	}
}

func (s *DefaultModelPricingService) Refresh(ctx context.Context) error {
	next, _, err := s.read(ctx)
	s.validationMu.RLock()
	defer s.validationMu.RUnlock()
	if err == nil && s.systemBaseline != nil {
		err = validateDefaultPricingSnapshot(ctx, next, s.systemBaseline)
	}
	s.statusMu.Lock()
	if err != nil {
		s.refreshError = "Unable to refresh persisted default pricing; the last valid revision remains loaded"
	} else {
		s.refreshError = ""
	}
	s.statusMu.Unlock()
	if err != nil {
		return err
	}
	current := s.snapshot.Load()
	if current != nil && next.Revision < current.Revision {
		s.statusMu.Lock()
		s.refreshError = "Persisted revision regressed; restart all instances after an intentional database restore"
		s.statusMu.Unlock()
		return fmt.Errorf("persisted default pricing revision regressed from %d to %d; restart after database restore", current.Revision, next.Revision)
	}
	s.publish(next)
	return nil
}

func cloneDefaultPricingFields(f DefaultPricingFields) DefaultPricingFields {
	// The wire type is small and fixed. Deep-copy pointers before exposing a snapshot.
	raw, _ := json.Marshal(f)
	var copy DefaultPricingFields
	_ = json.Unmarshal(raw, &copy)
	return copy
}

func (s *DefaultModelPricingService) Lookup(model string) (DefaultPricingFields, string, string, bool) {
	if s == nil {
		return DefaultPricingFields{}, "", "", false
	}
	snapshot := s.snapshot.Load()
	if snapshot == nil {
		return DefaultPricingFields{}, "", "", false
	}
	key := strings.ToLower(strings.TrimSpace(model))
	fields, ok := snapshot.Models[key]
	if !ok {
		// Exact administrator ID always wins over the deliberately narrow billing alias.
		key = adminDefaultPricingAliasKey(key)
		fields, ok = snapshot.Models[key]
	}
	return cloneDefaultPricingFields(fields), key, strconv.FormatUint(snapshot.Revision, 10), ok
}

func (s *DefaultModelPricingService) status(revision uint64) DefaultPricingStatus {
	out := DefaultPricingStatus{Version: strconv.FormatUint(revision, 10), RefreshIntervalSeconds: int(defaultPricingRefreshInterval / time.Second)}
	if current := s.snapshot.Load(); current != nil {
		out.LoadedVersion = strconv.FormatUint(current.Revision, 10)
	}
	s.statusMu.Lock()
	out.RefreshError = s.refreshError
	s.statusMu.Unlock()
	return out
}

func (s *DefaultModelPricingService) Read(ctx context.Context, model string) (DefaultPricingFields, bool, DefaultPricingStatus, error) {
	key, err := NormalizeDefaultPricingModel(model)
	if err != nil {
		return DefaultPricingFields{}, false, DefaultPricingStatus{}, err
	}
	persisted, _, err := s.read(ctx)
	if err != nil {
		return DefaultPricingFields{}, false, s.status(0), err
	}
	fields, exists := persisted.Models[key]
	return cloneDefaultPricingFields(fields), exists, s.status(persisted.Revision), nil
}

func (s *DefaultModelPricingService) List(ctx context.Context, query string, page, size int) (*DefaultPricingOverrideList, error) {
	persisted, _, err := s.read(ctx)
	if err != nil {
		return nil, err
	}
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	query = strings.ToLower(strings.TrimSpace(query))
	keys := make([]string, 0, len(persisted.Models))
	for key := range persisted.Models {
		if strings.Contains(key, query) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	result := &DefaultPricingOverrideList{DefaultPricingStatus: s.status(persisted.Revision), Items: []DefaultPricingOverrideItem{}, Total: len(keys), Page: page, PageSize: size}
	start := min((page-1)*size, len(keys))
	end := min(start+size, len(keys))
	for _, key := range keys[start:end] {
		result.Items = append(result.Items, DefaultPricingOverrideItem{key, cloneDefaultPricingFields(persisted.Models[key])})
	}
	return result, nil
}

func mergeDefaultPricingPatch(current DefaultPricingFields, patch DefaultPricingPatch) (DefaultPricingFields, error) {
	if len(patch) == 0 {
		return current, ErrDefaultPricingInvalid
	}
	raw, _ := json.Marshal(current)
	fields := map[string]json.RawMessage{}
	_ = json.Unmarshal(raw, &fields)
	allowed := map[string]bool{}
	for _, name := range DefaultPricingEditableFields() {
		allowed[name] = true
	}
	for name, value := range patch {
		if !allowed[name] {
			return current, ErrDefaultPricingInvalid.WithMetadata(map[string]string{"field": name})
		}
		if strings.TrimSpace(string(value)) == "null" {
			delete(fields, name)
		} else {
			fields[name] = value
		}
	}
	raw, err := json.Marshal(fields)
	if err != nil {
		return current, ErrDefaultPricingInvalid
	}
	var next DefaultPricingFields
	if err := json.Unmarshal(raw, &next); err != nil {
		return current, ErrDefaultPricingInvalid
	}
	if err := validateDefaultPricingFields(next); err != nil {
		return current, err
	}
	return next, nil
}

func validateDefaultPricingFields(fields DefaultPricingFields) error {
	switch fields.BillingMode {
	case "", BillingModeToken, BillingModePerRequest, BillingModeImage, BillingModeVideo:
	default:
		return ErrDefaultPricingInvalid.WithMetadata(map[string]string{"field": "billing_mode"})
	}
	for name, price := range fields.prices() {
		if price != nil {
			if err := validateBillingPrice(name, *price); err != nil {
				return ErrDefaultPricingInvalid.WithMetadata(map[string]string{"field": name}).WithCause(err)
			}
		}
	}
	return nil
}

func mergeDefaultPricingFields(base, override DefaultPricingFields) DefaultPricingFields {
	raw, _ := json.Marshal(override)
	patch := DefaultPricingPatch{}
	_ = json.Unmarshal(raw, &patch)
	if len(patch) == 0 {
		return cloneDefaultPricingFields(base)
	}
	merged, _ := mergeDefaultPricingPatch(base, patch)
	return merged
}

func validateDefaultPricingStandard(fields DefaultPricingFields) error {
	if err := validateDefaultPricingFields(fields); err != nil {
		return err
	}
	present := func(values ...*float64) bool {
		for _, value := range values {
			if value == nil {
				return false
			}
		}
		return true
	}
	valid := false
	switch fields.BillingMode {
	case BillingModeToken:
		valid = present(fields.InputPrice, fields.OutputPrice)
	case BillingModePerRequest:
		valid = present(fields.PerRequestPrice)
	case BillingModeImage:
		valid = present(fields.ImagePrice1K, fields.ImagePrice2K, fields.ImagePrice4K)
	case BillingModeVideo:
		valid = present(fields.VideoPrice480P, fields.VideoPrice720P, fields.VideoPrice1080P)
	}
	if !valid {
		return ErrDefaultPricingInvalid.WithMetadata(map[string]string{"field": "required_prices"})
	}
	return nil
}

// Save validates against the same system baseline used by billing. It never writes
// an entire client-supplied table and never publishes until the DB CAS commits.
func (s *DefaultModelPricingService) Save(ctx context.Context, model, version string, patch DefaultPricingPatch, base DefaultPricingFields, reset bool) (*DefaultPricingChange, error) {
	key, err := NormalizeDefaultPricingModel(model)
	if err != nil {
		return nil, err
	}
	current, raw, err := s.read(ctx)
	if err != nil {
		return nil, err
	}
	if version == "" || version != strconv.FormatUint(current.Revision, 10) {
		return nil, ErrDefaultPricingVersionConflict
	}
	if loaded := s.snapshot.Load(); loaded != nil && current.Revision < loaded.Revision {
		return nil, ErrServiceUnavailable
	}
	previous := current.Models[key]
	next, err := mergeDefaultPricingPatch(previous, patch)
	if err != nil {
		return nil, err
	}
	effective := mergeDefaultPricingFields(base, next)
	if err := validateDefaultPricingStandard(effective); err != nil {
		removing := reset
		for _, value := range patch {
			removing = removing || strings.TrimSpace(string(value)) == "null"
		}
		if removing {
			return nil, ErrDefaultPricingResetUnavailable
		}
		return nil, err
	}
	// Reject mismatched units; changing mode requires explicitly clearing old-mode fields.
	for name, price := range next.prices() {
		if price == nil {
			continue
		}
		mode := BillingModeToken
		if name == "per_request_price" {
			mode = BillingModePerRequest
		}
		if strings.HasPrefix(name, "image_price_") {
			mode = BillingModeImage
		}
		if strings.HasPrefix(name, "video_price_") {
			mode = BillingModeVideo
		}
		if effective.BillingMode != mode {
			return nil, ErrDefaultPricingInvalid.WithMetadata(map[string]string{"field": name, "reason": "wrong_billing_unit"})
		}
	}
	if current.Revision == ^uint64(0) {
		return nil, ErrServiceUnavailable
	}
	candidate := &defaultPricingSnapshot{SchemaVersion: 1, Revision: current.Revision + 1, Models: make(map[string]DefaultPricingFields, len(current.Models)+1)}
	for model, fields := range current.Models {
		candidate.Models[model] = fields
	}
	encoded, _ := json.Marshal(next)
	if string(encoded) == "{}" {
		delete(candidate.Models, key)
	} else {
		candidate.Models[key] = next
	}
	if len(candidate.Models) > maxDefaultPricingModels {
		return nil, ErrDefaultPricingInvalid
	}
	payload, err := json.Marshal(candidate)
	if err != nil || len(payload) > maxDefaultPricingBytes {
		return nil, ErrDefaultPricingInvalid
	}
	repo, ok := s.repo.(AtomicSettingPolicyRepository)
	if !ok {
		return nil, ErrServiceUnavailable
	}
	applied, err := repo.CompareAndSetMultiple(ctx, map[string]string{SettingKeyModelDefaultPricing: raw}, map[string]string{SettingKeyModelDefaultPricing: string(payload)})
	if err != nil {
		return nil, err
	}
	if !applied {
		return nil, ErrDefaultPricingVersionConflict
	}
	s.publish(candidate)
	return &DefaultPricingChange{snapshot: candidate, ModelID: key, Before: cloneDefaultPricingFields(previous), After: cloneDefaultPricingFields(next), OldVersion: version, NewVersion: strconv.FormatUint(candidate.Revision, 10)}, nil
}

// Register only the system-only baseline function, never a complete setting or
// billing service. Registration and refresh publication are serialized at startup.
func (s *DefaultModelPricingService) setBaselineValidator(baseline func(string) DefaultPricingBaseline) error {
	s.validationMu.Lock()
	defer s.validationMu.Unlock()
	if current := s.snapshot.Load(); current != nil {
		if err := validateDefaultPricingSnapshot(context.Background(), current, baseline); err != nil {
			return err
		}
	}
	s.systemBaseline = baseline
	return nil
}

func validateDefaultPricingSnapshot(ctx context.Context, snapshot *defaultPricingSnapshot, baseline func(string) DefaultPricingBaseline) error {
	for model, fields := range snapshot.Models {
		if err := ctx.Err(); err != nil {
			return err
		}
		effective := fields
		// Complete new standards do not need fuzzy catalog scans on every poll.
		if validateDefaultPricingStandard(effective) != nil {
			base := baseline(model)
			if base.MatchType == "family" {
				base.Fields.InputPrice = nil
				base.Fields.OutputPrice = nil
			}
			effective = mergeDefaultPricingFields(base.Fields, fields)
		}
		if err := validateDefaultPricingStandard(effective); err != nil {
			return fmt.Errorf("incomplete persisted default pricing for %s: %w", model, err)
		}
		for name, price := range fields.prices() {
			if price == nil {
				continue
			}
			mode := BillingModeToken
			if name == "per_request_price" {
				mode = BillingModePerRequest
			}
			if strings.HasPrefix(name, "image_price_") {
				mode = BillingModeImage
			}
			if strings.HasPrefix(name, "video_price_") {
				mode = BillingModeVideo
			}
			if effective.BillingMode != mode {
				return fmt.Errorf("invalid persisted default pricing unit for %s: %s", model, name)
			}
		}
	}
	return nil
}

// Keep exact requested overrides ahead of aliases. Only explicitly known video
// aliases are canonicalized; future native models must not inherit another
// generation's administrator price merely because they share a prefix.
func adminDefaultPricingAliasKey(model string) string {
	if alias := defaultPricingBaseModelKey(model); alias != model {
		return alias
	}
	native := model
	for _, prefix := range []string{"xai/", "x-ai/", "grok/"} {
		if strings.HasPrefix(native, prefix) {
			native = strings.TrimPrefix(native, prefix)
			break
		}
	}
	switch native {
	case "grok-imagine-video", "grok-imagine-video-preview", "grok-video", "grok-video-latest",
		"grok-imagine-video-1.5", "grok-imagine-video-1.5-preview", "grok-video-1.5":
		if key := CanonicalGrokImagineVideoPriceFamily(native); key != "" {
			return key
		}
	}
	return model
}
