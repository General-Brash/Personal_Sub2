package usagestats

import "time"

// HistoryMetadata describes the loss of sub-day precision after compaction.
// History bounds are half-open and cover only the included archived buckets.
type HistoryMetadata struct {
	IncludesHistory    bool       `json:"includes_history"`
	Approximate        bool       `json:"approximate"`
	HistoryGranularity string     `json:"history_granularity,omitempty"`
	HistoryRangePolicy string     `json:"history_range_policy,omitempty"`
	HistoryTimezone    string     `json:"history_timezone,omitempty"`
	HistoryStart       *time.Time `json:"history_start,omitempty"`
	HistoryEnd         *time.Time `json:"history_end,omitempty"`
}

func (m *HistoryMetadata) SetHistoryPolicy() {
	if m.IncludesHistory {
		m.Approximate = true
		m.HistoryGranularity = "day"
		m.HistoryRangePolicy = "whole_intersecting_days"
	}
}

// EffectiveGranularity is shared by query and response paths. A daily history
// bucket can never be presented as an hourly request series.
func EffectiveGranularity(requested string, includesHistory bool) string {
	switch requested {
	case "hour":
		if !includesHistory {
			return "hour"
		}
	case "week", "month":
		return requested
	}
	return "day"
}
