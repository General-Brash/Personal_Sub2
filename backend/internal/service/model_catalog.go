package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ModelCatalogState is the catalog-level existence state. It is deliberately
// separate from per-user eligibility and from live schedulability.
type ModelCatalogState string

const (
	ModelCatalogStateCatalogOnly            = "catalog_only"
	ModelCatalogStateEligible               = "eligible"
	ModelCatalogStateTemporarilyUnavailable = "temporarily_unavailable"
	ModelCatalogStateNotEntitled            = "not_entitled"
	ModelCatalogStateUnknown                = "unknown"
)

type ModelCatalogCapability string

const (
	ModelCatalogCapabilityText   ModelCatalogCapability = "text"
	ModelCatalogCapabilityImage  ModelCatalogCapability = "image"
	ModelCatalogCapabilityAudio  ModelCatalogCapability = "audio"
	ModelCatalogCapabilityVideo  ModelCatalogCapability = "video"
	ModelCatalogCapabilityEmbed  ModelCatalogCapability = "embedding"
	ModelCatalogCapabilityVision ModelCatalogCapability = "vision"
)

// ModelCatalogRoute describes one actual request route. Group is retained for
// pricing resolution and is never serialized by the HTTP DTO.
type ModelCatalogRoute struct {
	GroupID             int64  `json:"group_id"`
	GroupName           string `json:"group_name"`
	GroupPlatform       string `json:"group_platform"`
	RequestModelID      string `json:"request_model_id"`
	UpstreamModelID     string `json:"upstream_model_id,omitempty"`
	RouteKind           string `json:"route_kind"`
	Alias               bool   `json:"alias"`
	Exclusive           bool   `json:"exclusive"`
	Subscription        bool   `json:"subscription"`
	Schedulable         bool   `json:"schedulable"`
	AvailabilityKnown   bool   `json:"availability_known"`
	AvailabilityReason  string `json:"availability_reason,omitempty"`
	SourceVersion       string `json:"source_version,omitempty"`
	ChannelPricingModel string `json:"channel_pricing_model,omitempty"`
	Group               *Group `json:"-"`
}

type ModelCatalogItem struct {
	ModelID            string                   `json:"model_id"`
	DisplayName        string                   `json:"display_name"`
	Platform           string                   `json:"platform"`
	Capabilities       []ModelCatalogCapability `json:"capabilities,omitempty"`
	SupportedEndpoints []string                 `json:"supported_endpoints,omitempty"`
	ContextWindow      *int                     `json:"context_window,omitempty"`
	Routes             []ModelCatalogRoute      `json:"routes"`
	SourceVersions     map[string]string        `json:"source_versions,omitempty"`
}

type ModelCatalogSourceItem struct {
	ModelID            string
	DisplayName        string
	Platform           string
	Capabilities       []ModelCatalogCapability
	SupportedEndpoints []string
	ContextWindow      *int
	Routes             []ModelCatalogRoute
	SourceVersions     map[string]string
}

type ModelCatalogSource interface {
	ListModelCatalogSource(ctx context.Context) ([]ModelCatalogSourceItem, error)
}

// ModelCatalogService owns catalog normalization only. It does not decide
// billing, eligibility, or request admission.
type ModelCatalogService struct {
	source ModelCatalogSource
	now    func() time.Time
}

func NewModelCatalogService(source ModelCatalogSource) *ModelCatalogService {
	return &ModelCatalogService{source: source, now: time.Now}
}

// SetClock is useful for deterministic tests. It is intentionally not used by
// production wiring.
func (s *ModelCatalogService) SetClock(now func() time.Time) {
	if s != nil && now != nil {
		s.now = now
	}
}

func (s *ModelCatalogService) List(ctx context.Context) ([]ModelCatalogItem, error) {
	if s == nil || s.source == nil {
		return nil, fmt.Errorf("model catalog source is not configured")
	}
	raw, err := s.source.ListModelCatalogSource(ctx)
	if err != nil {
		return nil, err
	}
	merged := make(map[string]*ModelCatalogItem, len(raw))
	for _, item := range raw {
		modelID := strings.TrimSpace(item.ModelID)
		if modelID == "" {
			continue
		}
		platform := strings.ToLower(strings.TrimSpace(item.Platform))
		if platform == "" {
			platform = "unknown"
		}
		key := strings.ToLower(modelID) + "\x00" + platform
		current := merged[key]
		if current == nil {
			current = &ModelCatalogItem{
				ModelID:            modelID,
				DisplayName:        strings.TrimSpace(item.DisplayName),
				Platform:           platform,
				Capabilities:       append([]ModelCatalogCapability(nil), item.Capabilities...),
				SupportedEndpoints: append([]string(nil), item.SupportedEndpoints...),
				ContextWindow:      cloneCatalogInt(item.ContextWindow),
				SourceVersions:     cloneCatalogStringMap(item.SourceVersions),
			}
			if current.DisplayName == "" {
				current.DisplayName = modelID
			}
			merged[key] = current
		}
		current.Routes = mergeCatalogRoutes(current.Routes, item.Routes)
		current.Capabilities = mergeCatalogCapabilities(current.Capabilities, item.Capabilities)
		current.SupportedEndpoints = mergeCatalogStrings(current.SupportedEndpoints, item.SupportedEndpoints)
		current.SourceVersions = mergeCatalogStringMaps(current.SourceVersions, item.SourceVersions)
		if current.ContextWindow == nil && item.ContextWindow != nil {
			current.ContextWindow = cloneCatalogInt(item.ContextWindow)
		}
	}

	out := make([]ModelCatalogItem, 0, len(merged))
	for _, item := range merged {
		sort.SliceStable(item.Routes, func(i, j int) bool {
			if item.Routes[i].GroupID != item.Routes[j].GroupID {
				return item.Routes[i].GroupID < item.Routes[j].GroupID
			}
			if item.Routes[i].RequestModelID != item.Routes[j].RequestModelID {
				return item.Routes[i].RequestModelID < item.Routes[j].RequestModelID
			}
			return item.Routes[i].RouteKind < item.Routes[j].RouteKind
		})
		out = append(out, *item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ModelID != out[j].ModelID {
			return out[i].ModelID < out[j].ModelID
		}
		return out[i].Platform < out[j].Platform
	})
	return out, nil
}

func mergeCatalogRoutes(dst, src []ModelCatalogRoute) []ModelCatalogRoute {
	seen := make(map[string]struct{}, len(dst)+len(src))
	out := append([]ModelCatalogRoute(nil), dst...)
	for _, route := range dst {
		seen[catalogRouteKey(route)] = struct{}{}
	}
	for _, route := range src {
		key := catalogRouteKey(route)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		route.GroupName = strings.TrimSpace(route.GroupName)
		route.GroupPlatform = strings.ToLower(strings.TrimSpace(route.GroupPlatform))
		route.RequestModelID = strings.TrimSpace(route.RequestModelID)
		route.UpstreamModelID = strings.TrimSpace(route.UpstreamModelID)
		route.RouteKind = strings.TrimSpace(route.RouteKind)
		if route.RouteKind == "" {
			route.RouteKind = "direct"
		}
		out = append(out, route)
	}
	return out
}

func catalogRouteKey(route ModelCatalogRoute) string {
	return fmt.Sprintf("%d|%s|%s|%s", route.GroupID, strings.ToLower(route.RequestModelID), strings.ToLower(route.RouteKind), strings.ToLower(route.UpstreamModelID))
}

func mergeCatalogCapabilities(dst, src []ModelCatalogCapability) []ModelCatalogCapability {
	seen := make(map[ModelCatalogCapability]struct{}, len(dst)+len(src))
	out := append([]ModelCatalogCapability(nil), dst...)
	for _, value := range dst {
		seen[value] = struct{}{}
	}
	for _, value := range src {
		value = ModelCatalogCapability(strings.TrimSpace(string(value)))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func mergeCatalogStrings(dst, src []string) []string {
	seen := make(map[string]struct{}, len(dst)+len(src))
	out := append([]string(nil), dst...)
	for _, value := range dst {
		seen[strings.ToLower(value)] = struct{}{}
	}
	for _, value := range src {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func mergeCatalogStringMaps(dst, src map[string]string) map[string]string {
	out := cloneCatalogStringMap(dst)
	if out == nil {
		out = map[string]string{}
	}
	for key, value := range src {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			continue
		}
		out[key] = value
	}
	return out
}

func cloneCatalogStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneCatalogInt(in *int) *int {
	if in == nil {
		return nil
	}
	value := *in
	return &value
}
