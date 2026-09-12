package service

import (
	"context"
	"strings"
)

func DynamicRateModeForGatewayResult(result *ForwardResult) string {
	if result == nil {
		return DynamicRateModeText
	}
	if result.ImageCount > 0 {
		return DynamicRateModeImage
	}
	if result.AudioUsage != nil {
		return DynamicRateModeAudio
	}
	return DynamicRateModeText
}

func DynamicRateModeForOpenAIResult(result *OpenAIForwardResult) string {
	if result == nil {
		return DynamicRateModeText
	}
	if result.WebSearchCalls > 0 {
		return DynamicRateModeText
	}
	if result.VideoCount > 0 {
		return DynamicRateModeVideo
	}
	if result.AudioUsage != nil {
		return DynamicRateModeAudio
	}
	if result.ImageCount > 0 {
		return DynamicRateModeImage
	}
	return DynamicRateModeText
}

func (s *GatewayService) resolveDynamicRateForBilling(ctx context.Context, userID, groupID int64, mode string) (*DynamicRatePricingSnapshot, error) {
	if s == nil {
		return nil, nil
	}
	return dynamicRateSnapshotForBilling(ctx, s.dynamicRateResolver, userID, groupID, mode, "")
}

func (s *OpenAIGatewayService) resolveDynamicRateForBilling(ctx context.Context, userID, groupID int64, mode string) (*DynamicRatePricingSnapshot, error) {
	if s == nil {
		return nil, nil
	}
	return dynamicRateSnapshotForBilling(ctx, s.dynamicRateResolver, userID, groupID, mode, "")
}

func applyDynamicRateMultipliers(snapshot *DynamicRatePricingSnapshot, staticFactor, peakFactor, imageBase, videoBase, searchBase float64) (text, image, video, search float64) {
	text = staticFactor * peakFactor
	image = imageBase
	video = videoBase
	search = searchBase
	if snapshot == nil {
		return
	}
	// A snapshot is mode-scoped. Never leak a text policy into image/video or
	// vice versa: the mode admission gate is fail-closed, and this switch makes
	// a regression at the call site non-billable instead of silently discounting.
	switch normalizeDynamicRateMode(snapshot.Mode) {
	case DynamicRateModeText:
		text = snapshot.FinalFactor
		// Search surcharge is a text-mode surcharge and shares the text factor.
		search = snapshot.StaticFactor * snapshot.DynamicFactor
	case DynamicRateModeImage:
		text = snapshot.FinalFactor
		if snapshot.ImageBaseFactor != nil {
			imageBase = *snapshot.ImageBaseFactor
		}
		image = imageBase * snapshot.DynamicFactor
		search = searchBase * snapshot.DynamicFactor
	case DynamicRateModeVideo:
		video = videoBase * snapshot.DynamicFactor
	}
	return
}

func normalizeDynamicRateMode(mode string) string {
	return strings.ToLower(strings.TrimSpace(mode))
}
