package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"
)

func (r *usageBillingRepository) GetDynamicRatePolicy(ctx context.Context, groupID int64) (*service.DynamicRatePolicy, error) {
	return r.loadDynamicRatePolicy(ctx, groupID, false)
}
func (r *usageBillingRepository) GetDynamicRatePolicyDraft(ctx context.Context, groupID int64) (*service.DynamicRatePolicy, error) {
	return r.loadDynamicRatePolicy(ctx, groupID, true)
}
func (r *usageBillingRepository) loadDynamicRatePolicy(ctx context.Context, groupID int64, draft bool) (*service.DynamicRatePolicy, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("usage billing repository db is nil")
	}
	var policy service.DynamicRatePolicy
	var tiersJSON, modesJSON []byte
	var effectiveAt sql.NullTime
	err := r.db.QueryRowContext(ctx, `
		SELECT group_id, enabled, metric, timezone, reset_time, tiers, included_modes,
		       cache_token_policy, policy_version, effective_at
		FROM group_dynamic_rate_policies
		WHERE group_id = $1
	`, groupID).Scan(
		&policy.GroupID, &policy.Enabled, &policy.Metric, &policy.Timezone, &policy.ResetTime,
		&tiersJSON, &modesJSON, &policy.CacheTokenPolicy, &policy.PolicyVersion, &effectiveAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load dynamic rate policy: %w", err)
	}
	if effectiveAt.Valid {
		policy.EffectiveAt = effectiveAt.Time
	}
	currentPolicyVersion := policy.PolicyVersion
	policy.CurrentPolicyVersion = currentPolicyVersion
	if !draft && policy.EffectiveAt.After(time.Now()) {
		var historyJSON []byte
		err := r.db.QueryRowContext(ctx, `
			SELECT snapshot
			FROM group_dynamic_rate_policy_versions
			WHERE group_id = $1 AND effective_at <= NOW()
			ORDER BY policy_version DESC
			LIMIT 1
		`, groupID).Scan(&historyJSON)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("load effective dynamic rate policy version: %w", err)
		}
		if err := json.Unmarshal(historyJSON, &policy); err != nil {
			return nil, fmt.Errorf("decode dynamic rate policy history: %w", err)
		}
		policy.CurrentPolicyVersion = currentPolicyVersion
		policy.ExpectedPolicyVersion = nil
		return &policy, nil
	}
	if err := json.Unmarshal(tiersJSON, &policy.Tiers); err != nil {
		return nil, fmt.Errorf("decode dynamic rate tiers: %w", err)
	}
	if err := json.Unmarshal(modesJSON, &policy.IncludedModes); err != nil {
		return nil, fmt.Errorf("decode dynamic rate modes: %w", err)
	}
	return &policy, nil
}

func (r *usageBillingRepository) GetDynamicRateUsage(ctx context.Context, userID, groupID int64, windowID string) (service.DynamicRateUsageCounters, error) {
	if r == nil || r.db == nil {
		return service.DynamicRateUsageCounters{}, errors.New("usage billing repository db is nil")
	}
	var tokens int64
	var wallet decimal.Decimal
	err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(normalized_tokens, 0), COALESCE(wallet_spent, 0)
		FROM user_group_usage_periods
		WHERE user_id = $1 AND group_id = $2 AND window_id = $3
	`, userID, groupID, windowID).Scan(&tokens, &wallet)
	if errors.Is(err, sql.ErrNoRows) {
		return service.DynamicRateUsageCounters{WalletSpent: decimal.Zero}, nil
	}
	if err != nil {
		return service.DynamicRateUsageCounters{}, fmt.Errorf("load dynamic rate usage: %w", err)
	}
	return service.DynamicRateUsageCounters{NormalizedTokens: tokens, WalletSpent: wallet}, nil
}

func (r *usageBillingRepository) UpsertDynamicRatePolicy(ctx context.Context, policy *service.DynamicRatePolicy) (*service.DynamicRatePolicy, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("usage billing repository db is nil")
	}
	if policy == nil {
		return nil, service.ErrDynamicRatePolicyInvalid
	}
	if err := service.ValidateDynamicRatePolicy(policy); err != nil {
		return nil, err
	}
	if policy.EffectiveAt.IsZero() {
		policy.EffectiveAt = time.Now().UTC()
	}
	tiersJSON, err := json.Marshal(policy.Tiers)
	if err != nil {
		return nil, fmt.Errorf("encode dynamic rate tiers: %w", err)
	}
	modesJSON, err := json.Marshal(policy.IncludedModes)
	if err != nil {
		return nil, fmt.Errorf("encode dynamic rate modes: %w", err)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var subscriptionType string
	var allowLive, allowImage, allowBatchImage bool
	err = tx.QueryRowContext(ctx, `
		SELECT subscription_type, allow_live, allow_image_generation, allow_batch_image_generation
		FROM groups
		WHERE id = $1
		FOR UPDATE
	`, policy.GroupID).Scan(&subscriptionType, &allowLive, &allowImage, &allowBatchImage)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: group %d does not exist", service.ErrDynamicRatePolicyInvalid, policy.GroupID)
	}
	if err != nil {
		return nil, fmt.Errorf("load dynamic rate group facts: %w", err)
	}
	var currentVersion int64
	err = tx.QueryRowContext(ctx, `SELECT policy_version FROM group_dynamic_rate_policies WHERE group_id = $1 FOR UPDATE`, policy.GroupID).Scan(&currentVersion)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("lock dynamic rate policy: %w", err)
	}
	if policy.Metric == service.DynamicRateMetricWalletSpend && strings.EqualFold(strings.TrimSpace(subscriptionType), service.SubscriptionTypeSubscription) {
		return nil, service.ErrDynamicRateSubscriptionWalletMode
	}
	for _, mode := range policy.IncludedModes {
		switch mode {
		case service.DynamicRateModeImage:
			if !allowImage {
				return nil, fmt.Errorf("%w: image mode is not enabled for group", service.ErrDynamicRateModeNotCovered)
			}
		case service.DynamicRateModeBatchImage:
			if !allowBatchImage {
				return nil, fmt.Errorf("%w: batch image mode is not enabled for group", service.ErrDynamicRateModeNotCovered)
			}
		case "live":
			if !allowLive {
				return nil, fmt.Errorf("%w: live mode is not enabled for group", service.ErrDynamicRateModeNotCovered)
			}
		}
	}
	if policy.ExpectedPolicyVersion != nil && *policy.ExpectedPolicyVersion != currentVersion {
		return nil, fmt.Errorf("%w: expected %d, current %d", service.ErrDynamicRatePolicyVersionConflict, *policy.ExpectedPolicyVersion, currentVersion)
	}
	if policy.Enabled && (allowLive || (allowImage && !dynamicPolicyHasMode(policy, service.DynamicRateModeImage)) || (allowBatchImage && !dynamicPolicyHasMode(policy, service.DynamicRateModeBatchImage))) {
		return nil, fmt.Errorf("%w: disable live/image/batch modes before enabling a text-only dynamic policy", service.ErrDynamicRateModeNotCovered)
	}
	if policy.Enabled && policy.ExpectedPolicyVersion == nil {
		return nil, fmt.Errorf("%w: expected_policy_version is required", service.ErrDynamicRatePolicyVersionConflict)
	}
	// Switch only at a complete boundary of the previously active clock. A
	// policy edit is not an opportunity to reset the current usage bucket.
	activePolicy, activeErr := r.GetDynamicRatePolicy(ctx, policy.GroupID)
	if activeErr != nil {
		return nil, activeErr
	}
	boundaryPolicy := policy
	if activePolicy != nil {
		boundaryPolicy = activePolicy
	}
	_, _, nextBoundary, boundaryErr := service.DynamicRateWindow(boundaryPolicy, time.Now().UTC())
	if boundaryErr != nil {
		return nil, boundaryErr
	}
	if policy.EffectiveAt.Before(nextBoundary) {
		policy.EffectiveAt = nextBoundary
	}
	policy.PolicyVersion = currentVersion + 1
	if policy.PolicyVersion < 1 {
		policy.PolicyVersion = 1
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO group_dynamic_rate_policies
			(group_id, enabled, metric, timezone, reset_time, tiers, included_modes, cache_token_policy, policy_version, effective_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb,$8,$9,$10,NOW())
		ON CONFLICT (group_id) DO UPDATE SET
			enabled = EXCLUDED.enabled,
			metric = EXCLUDED.metric,
			timezone = EXCLUDED.timezone,
			reset_time = EXCLUDED.reset_time,
			tiers = EXCLUDED.tiers,
			included_modes = EXCLUDED.included_modes,
			cache_token_policy = EXCLUDED.cache_token_policy,
			policy_version = EXCLUDED.policy_version,
			effective_at = EXCLUDED.effective_at,
			updated_at = NOW()
	`, policy.GroupID, policy.Enabled, policy.Metric, policy.Timezone, policy.ResetTime, tiersJSON, modesJSON, policy.CacheTokenPolicy, policy.PolicyVersion, policy.EffectiveAt); err != nil {
		return nil, fmt.Errorf("upsert dynamic rate policy: %w", err)
	}
	policy.CurrentPolicyVersion = policy.PolicyVersion
	policy.ExpectedPolicyVersion = nil
	historyJSON, err := json.Marshal(policy)
	if err != nil {
		return nil, fmt.Errorf("encode dynamic rate policy history: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO group_dynamic_rate_policy_versions (group_id, policy_version, snapshot, effective_at, created_at)
		VALUES ($1,$2,$3::jsonb,$4,NOW())
		ON CONFLICT (group_id, policy_version) DO NOTHING
	`, policy.GroupID, policy.PolicyVersion, historyJSON, policy.EffectiveAt); err != nil {
		return nil, fmt.Errorf("write dynamic rate policy history: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return policy, nil
}

func normalizedDynamicRateTokenDelta(cmd *service.UsageBillingCommand) int64 {
	if cmd == nil {
		return 0
	}
	// Input/output/cache-write/cache-read are the normalized disjoint buckets
	// produced by the protocol billing layers. Do not add raw upstream fields or
	// ImageInputTokens here: those may overlap or use a different unit.
	total := int64(0)
	for _, value := range []int{cmd.InputTokens, cmd.OutputTokens, cmd.CacheCreationTokens, cmd.CacheReadTokens} {
		if value < 0 || int64(value) > math.MaxInt64-total {
			return -1
		}
		total += int64(value)
	}
	return total
}

func incrementDynamicRateUsage(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand, actualWalletDelta decimal.Decimal) error {
	if cmd == nil || cmd.DynamicRateSnapshot == nil {
		return nil
	}
	snapshot := cmd.DynamicRateSnapshot
	if cmd.UsageLog == nil || cmd.UsageLog.GroupID == nil || *cmd.UsageLog.GroupID <= 0 || snapshot.UserID != cmd.UserID || snapshot.GroupID != *cmd.UsageLog.GroupID {
		return service.ErrDynamicRateSnapshotMismatch
	}
	if snapshot.WindowID == "" || snapshot.Metric == "" {
		return service.ErrDynamicRatePolicyInvalid
	}
	if err := service.ValidateDynamicRateSnapshotForRequest(snapshot, cmd.UserID, snapshot.GroupID, snapshot.Mode, snapshot.Metric); err != nil {
		return err
	}
	// A text policy may never be used to discount a media request. Media modes
	// are not adapted yet and are rejected before upstream; this is a defensive
	// ledger check for a future caller regression.
	if strings.EqualFold(strings.TrimSpace(snapshot.Mode), service.DynamicRateModeText) {
		if cmd.UsageLog.ImageCount > 0 || cmd.UsageLog.VideoCount > 0 || strings.TrimSpace(cmd.MediaType) != "" {
			return service.ErrDynamicRateModeNotCovered
		}
	}
	var tokenDelta int64
	walletDelta := decimal.Zero
	switch snapshot.Metric {
	case service.DynamicRateMetricTokensM:
		tokenDelta = normalizedDynamicRateTokenDelta(cmd)
		if tokenDelta < 0 || (tokenDelta == 0 && cmd.BalanceCost > 0) {
			return service.ErrDynamicRateTokenUsageUnavailable
		}
	case service.DynamicRateMetricWalletSpend:
		if cmd.SubscriptionCost > 0 {
			return service.ErrDynamicRateSubscriptionWalletMode
		}
		walletDelta = actualWalletDelta
	default:
		return service.ErrDynamicRatePolicyInvalid
	}
	snapshotJSON, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE usage_billing_dedup
		SET dynamic_rate_snapshot = $3::jsonb,
		    dynamic_rate_window_id = $4,
		    dynamic_rate_tokens_delta = $5,
		    dynamic_rate_wallet_delta = $6
		WHERE request_id = $1 AND api_key_id = $2
	`, cmd.RequestID, cmd.APIKeyID, snapshotJSON, snapshot.WindowID, tokenDelta, walletDelta.String()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO user_group_usage_periods
			(user_id, group_id, window_id, normalized_tokens, wallet_spent, version, updated_at)
		VALUES ($1,$2,$3,$4,$5,1,NOW())
		ON CONFLICT (user_id, group_id, window_id) DO UPDATE SET
			normalized_tokens = user_group_usage_periods.normalized_tokens + EXCLUDED.normalized_tokens,
			wallet_spent = user_group_usage_periods.wallet_spent + EXCLUDED.wallet_spent,
			version = user_group_usage_periods.version + 1,
			updated_at = NOW()
	`, cmd.UserID, snapshot.GroupID, snapshot.WindowID, tokenDelta, walletDelta.String()); err != nil {
		return err
	}
	return nil
}

func dynamicPolicyHasMode(policy *service.DynamicRatePolicy, mode string) bool {
	for _, included := range policy.IncludedModes {
		if included == mode {
			return true
		}
	}
	return false
}

func (r *usageBillingRepository) GetDynamicRatePolicyAt(ctx context.Context, groupID int64, at time.Time) (*service.DynamicRatePolicy, error) {
	var raw []byte
	err := r.db.QueryRowContext(ctx, `SELECT snapshot FROM group_dynamic_rate_policy_versions WHERE group_id=$1 AND effective_at<=$2 ORDER BY policy_version DESC LIMIT 1`, groupID, at).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var policy service.DynamicRatePolicy
	if err := json.Unmarshal(raw, &policy); err != nil {
		return nil, err
	}
	policy.ExpectedPolicyVersion = nil
	return &policy, nil
}
