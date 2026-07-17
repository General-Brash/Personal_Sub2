package repository

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
)

const availableCreditCacheScale = 8

var fixedAvailableCreditCacheValuePattern = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)\.[0-9]{8}$`)

func formatAvailableCreditCacheValue(value float64) (string, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "", fmt.Errorf("available credit cache value must be finite")
	}
	raw := strconv.FormatFloat(value, 'f', availableCreditCacheScale, 64)
	if !fixedAvailableCreditCacheValuePattern.MatchString(raw) {
		return "", fmt.Errorf("invalid available credit cache value %q", raw)
	}
	return raw, nil
}

func parseAvailableCreditCacheValue(raw string) (float64, error) {
	if !fixedAvailableCreditCacheValuePattern.MatchString(raw) {
		return 0, fmt.Errorf("invalid fixed available credit cache value %q", raw)
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("parse available credit cache value %q: %w", raw, err)
	}
	return value, nil
}
