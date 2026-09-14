package repository

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type entitlementRepository struct {
	sqlDB             *sql.DB
	previewSigningKey []byte
}

var _ service.EntitlementRepository = (*entitlementRepository)(nil)

func NewEntitlementRepository(sqlDB *sql.DB, cfg *config.Config) service.EntitlementRepository {
	r := &entitlementRepository{sqlDB: sqlDB}
	if cfg != nil && strings.TrimSpace(cfg.JWT.Secret) != "" {
		// Domain separation keeps preview credentials independent of JWT tokens.
		// The existing configured secret is shared across instances and restarts.
		mac := hmac.New(sha256.New, []byte(cfg.JWT.Secret))
		_, _ = mac.Write([]byte("personal-sub2:entitlement-preview:v1"))
		r.previewSigningKey = mac.Sum(nil)
	}
	return r
}

func (r *entitlementRepository) requireDB() (*sql.DB, error) {
	if r == nil || r.sqlDB == nil {
		return nil, errors.New("entitlement repository database is nil")
	}
	return r.sqlDB, nil
}

func (r *entitlementRepository) EntitlementTierDefinition(ctx context.Context, tier string) (string, bool, int64, error) {
	db, err := r.requireDB()
	if err != nil {
		return "", false, 0, err
	}
	var name string
	var enabled bool
	var version int64
	err = db.QueryRowContext(ctx, `SELECT display_name, enabled, version FROM entitlement_tiers WHERE tier = $1`, tier).Scan(&name, &enabled, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, 0, service.ErrEntitlementTierUnknown
	}
	if err != nil {
		return "", false, 0, fmt.Errorf("load entitlement tier: %w", err)
	}
	return name, enabled, version, nil
}

func (r *entitlementRepository) ResolveEntitlement(ctx context.Context, userID int64) (*service.EntitlementSnapshot, error) {
	db, err := r.requireDB()
	if err != nil {
		return nil, err
	}
	var exists int
	if err := db.QueryRowContext(ctx, `SELECT 1 FROM users WHERE id=$1 AND deleted_at IS NULL`, userID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrUserNotFound
		}
		return nil, fmt.Errorf("load entitlement user: %w", err)
	}
	return r.resolveEntitlement(ctx, db, userID, time.Now().UTC())
}

func (r *entitlementRepository) resolveEntitlement(ctx context.Context, db entitlementQueryer, userID int64, now time.Time) (*service.EntitlementSnapshot, error) {
	snapshot := &service.EntitlementSnapshot{UserID: userID, Tier: service.EntitlementTierStandard, TierEnabled: true, Version: 1,
		DefaultRates: map[int64]float64{}, AllowedGroups: []int64{}, TierGroups: []int64{}, Sources: []service.EntitlementSource{},
		ManualGroups: []int64{}, SubscriptionGroups: []int64{}}

	var tier, source string
	var version int64
	var expiresAt sql.NullTime
	err := db.QueryRowContext(ctx, `
SELECT tier, source, version, expires_at
FROM user_entitlements
WHERE user_id = $1 AND (expires_at IS NULL OR expires_at > $2)`, userID, now).Scan(&tier, &source, &version, &expiresAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("load user entitlement: %w", err)
	}
	if err == nil {
		snapshot.Tier, snapshot.Version = tier, version
		if expiresAt.Valid {
			t := expiresAt.Time
			snapshot.Sources = append(snapshot.Sources, service.EntitlementSource{Source: source, Tier: tier, Version: version, ExpiresAt: &t, Explain: "user_entitlements"})
		} else {
			snapshot.Sources = append(snapshot.Sources, service.EntitlementSource{Source: source, Tier: tier, Version: version, Explain: "user_entitlements"})
		}
	}

	var grantTier, grantSource string
	var grantVersion int64
	var grantExpires sql.NullTime
	err = db.QueryRowContext(ctx, `
SELECT tier, source, version, expires_at
FROM user_entitlement_grants
WHERE user_id = $1 AND (expires_at IS NULL OR expires_at > $2)
ORDER BY CASE tier WHEN 'premium' THEN 0 ELSE 1 END, version DESC
LIMIT 1`, userID, now).Scan(&grantTier, &grantSource, &grantVersion, &grantExpires)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("load user entitlement grant: %w", err)
	}
	if err == nil {
		if rankEntitlementTier(grantTier) > rankEntitlementTier(snapshot.Tier) {
			snapshot.Tier, snapshot.Version = grantTier, grantVersion
		}
		source := grantSource
		if grantExpires.Valid {
			t := grantExpires.Time
			snapshot.Sources = append(snapshot.Sources, service.EntitlementSource{Source: source, Tier: grantTier, Version: grantVersion, ExpiresAt: &t, Explain: "user_entitlement_grants"})
		} else {
			snapshot.Sources = append(snapshot.Sources, service.EntitlementSource{Source: source, Tier: grantTier, Version: grantVersion, Explain: "user_entitlement_grants"})
		}
	}

	var enabled bool
	var tierVersion int64
	if err := db.QueryRowContext(ctx, `SELECT enabled, version FROM entitlement_tiers WHERE tier = $1`, snapshot.Tier).Scan(&enabled, &tierVersion); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			snapshot.Tier, snapshot.TierEnabled = service.EntitlementTierStandard, true
		} else {
			return nil, fmt.Errorf("load effective entitlement tier: %w", err)
		}
	} else {
		snapshot.TierEnabled = enabled
		snapshot.Version += tierVersion
		if !enabled {
			snapshot.Tier = service.EntitlementTierStandard
		}
	}

	snapshot.ManualGroups, err = queryInt64Column(ctx, db, `SELECT group_id FROM user_allowed_groups WHERE user_id = $1 ORDER BY group_id`, userID)
	if err != nil {
		return nil, fmt.Errorf("load manual entitlement groups: %w", err)
	}
	snapshot.SubscriptionGroups, err = queryInt64Column(ctx, db, `SELECT DISTINCT group_id FROM user_subscriptions WHERE user_id = $1 AND status = 'active' AND deleted_at IS NULL AND (expires_at IS NULL OR expires_at > $2) ORDER BY group_id`, userID, now)
	if err != nil {
		return nil, fmt.Errorf("load subscription entitlement groups: %w", err)
	}
	rows, err := db.QueryContext(ctx, `SELECT group_id, rate_multiplier FROM entitlement_tier_groups WHERE tier = $1 ORDER BY group_id`, snapshot.Tier)
	if err != nil {
		return nil, fmt.Errorf("load tier groups: %w", err)
	}
	for rows.Next() {
		var groupID int64
		var rate sql.NullFloat64
		if err := rows.Scan(&groupID, &rate); err != nil {
			return nil, joinRowsCloseError(rows, err)
		}
		snapshot.TierGroups = append(snapshot.TierGroups, groupID)
		if rate.Valid {
			value := rate.Float64
			snapshot.DefaultRates[groupID] = value
			snapshot.Sources = append(snapshot.Sources, service.EntitlementSource{Source: "tier_group", Tier: snapshot.Tier, GroupID: &groupID, Rate: &value, Version: snapshot.Version, Explain: "entitlement_tier_groups"})
		}
	}
	if err := joinRowsCloseError(rows, rows.Err()); err != nil {
		return nil, err
	}
	snapshot.AllowedGroups = mergeInt64Groups(snapshot.ManualGroups, snapshot.SubscriptionGroups, snapshot.TierGroups)
	return snapshot, nil
}

func (r *entitlementRepository) PreviewEntitlementChange(ctx context.Context, userIDs []int64, tier string) (*service.EntitlementChangePreview, error) {
	db, err := r.requireDB()
	if err != nil {
		return nil, err
	}
	tier = service.NormalizeEntitlementWriteTier(tier)
	if tier == "" {
		return nil, service.ErrEntitlementTierUnknown
	}
	ids, err := normalizeEntitlementMutationIDs(userIDs)
	if err != nil {
		return nil, err
	}
	actor, err := currentEntitlementPreviewActor(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("begin entitlement preview: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UTC()
	state, err := loadEntitlementPreviewState(ctx, tx, ids, now)
	if err != nil {
		return nil, err
	}
	policy, err := entitlementPreviewTargetPolicy(state, tier)
	if err != nil {
		return nil, err
	}
	preview := &service.EntitlementChangePreview{Tier: tier, UserIDs: ids, AffectedUserIDs: []int64{}, AlreadyAtTier: []int64{},
		GrantedGroupIDs: []int64{}, RevokedGroupIDs: []int64{}, PreservedManual: true, PreservedSubscriptions: true,
		PolicyEnabled: policy.Enabled, PolicyVersion: policy.Version, ExpiresAt: time.Unix(now.Unix()+int64(entitlementPreviewLifetime/time.Second), 0).UTC()}
	granted, revoked := map[int64]struct{}{}, map[int64]struct{}{}
	claims := entitlementPreviewClaims{Format: entitlementPreviewFormat, Actor: actor, Tier: tier,
		Targets: make([]entitlementPreviewTarget, 0, len(ids)), IssuedAt: now.Unix(), ExpiresAt: preview.ExpiresAt.Unix()}
	for i, userID := range ids {
		userState := state.Users[i]
		claims.Targets = append(claims.Targets, userState.entitlementPreviewTarget)
		needed, err := entitlementAssignmentNeeded(userState.Assigned, tier)
		if err != nil {
			return nil, err
		}
		if !needed {
			preview.AlreadyAtTier = append(preview.AlreadyAtTier, userID)
			continue
		}
		preview.AffectedUserIDs = append(preview.AffectedUserIDs, userID)
		snapshot, err := r.resolveEntitlement(ctx, tx, userID, now)
		if err != nil {
			return nil, err
		}
		projectedTier, err := projectedEntitlementTier(state, userState.Grants, tier)
		if err != nil {
			return nil, err
		}
		newTierGroups, err := r.tierGroups(ctx, tx, projectedTier)
		if err != nil {
			return nil, err
		}
		oldSet, newSet := map[int64]struct{}{}, map[int64]struct{}{}
		for _, id := range snapshot.AllowedGroups {
			oldSet[id] = struct{}{}
		}
		for _, id := range mergeInt64Groups(snapshot.ManualGroups, snapshot.SubscriptionGroups, newTierGroups) {
			newSet[id] = struct{}{}
		}
		for id := range newSet {
			if _, ok := oldSet[id]; !ok {
				granted[id] = struct{}{}
			}
		}
		for id := range oldSet {
			if _, ok := newSet[id]; !ok {
				revoked[id] = struct{}{}
			}
		}
	}
	preview.GrantedGroupIDs, preview.RevokedGroupIDs = mapKeys(granted), mapKeys(revoked)
	claims.StateHash, err = entitlementPreviewStateHash(state)
	if err != nil {
		return nil, err
	}
	preview.PreviewToken, err = r.signEntitlementPreview(claims)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("finish entitlement preview: %w", err)
	}
	return preview, nil
}

func (r *entitlementRepository) ApplyEntitlementChange(ctx context.Context, userIDs []int64, tier string, actorUserID int64, reason, requestID, previewToken string) (*service.EntitlementChangeResult, error) {
	db, err := r.requireDB()
	if err != nil {
		return nil, err
	}
	tier = service.NormalizeEntitlementWriteTier(tier)
	if tier == "" {
		return nil, service.ErrEntitlementTierUnknown
	}
	reason, requestID = strings.TrimSpace(reason), strings.TrimSpace(requestID)
	if reason == "" {
		return nil, service.ErrEntitlementReasonRequired
	}
	if requestID == "" || len(requestID) > 128 {
		return nil, service.ErrEntitlementRequestIDRequired
	}
	ids, err := normalizeEntitlementMutationIDs(userIDs)
	if err != nil {
		return nil, err
	}
	actor, err := currentEntitlementPreviewActor(ctx)
	if err != nil || actor.UserID != actorUserID {
		return nil, service.ErrAdminPermissionDenied
	}
	ctx = service.ContextWithAdminMutationActorID(ctx, actorUserID)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin entitlement change: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := lockAdminMutationScope(ctx, tx); err != nil {
		return nil, err
	}
	lockIDs := append(append([]int64{}, ids...), actorUserID)
	lockIDs, err = normalizeEntitlementMutationIDsForLocks(lockIDs)
	if err != nil {
		return nil, err
	}
	lockedUsers, err := lockAdminMutationUsers(ctx, tx, lockIDs)
	if err != nil {
		return nil, err
	}
	required := make([]AdminMutationPermission, 0, len(ids))
	for _, id := range ids {
		required = append(required, AdminMutationPermission{Permission: "users.entitlement.manage", Scope: userMutationScope(id)})
	}
	authorized, err := AuthorizeAdminMutationTx(ctx, tx, actorUserID, AdminMutationAuthorizationOptions{Permissions: required})
	if err != nil {
		return nil, err
	}
	if authorized == nil {
		return nil, service.ErrAdminPermissionDenied
	}
	fingerprint := entitlementChangeFingerprint(ids, tier, reason, previewToken)
	lockKey := fmt.Sprintf("entitlement-change:%d:%s", actorUserID, requestID)
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, lockKey); err != nil {
		return nil, err
	}
	var recordedFingerprint string
	var recordedResult []byte
	replayErr := tx.QueryRowContext(ctx, `SELECT fingerprint,result FROM entitlement_change_requests WHERE actor_user_id=$1 AND request_id=$2`, actorUserID, requestID).Scan(&recordedFingerprint, &recordedResult)
	if replayErr == nil {
		if recordedFingerprint != fingerprint {
			return nil, service.ErrEntitlementIdempotencyConflict
		}
		var result service.EntitlementChangeResult
		if err := json.Unmarshal(recordedResult, &result); err != nil {
			return nil, err
		}
		result.Idempotent = true
		return &result, tx.Commit()
	}
	if !errors.Is(replayErr, sql.ErrNoRows) {
		return nil, replayErr
	}

	// Completed same-key/same-input results above are replayed before any preview,
	// expiry or target-state check. New writes can never take that shortcut.
	claims, err := r.verifyEntitlementPreview(previewToken, actor, ids, tier, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if err := validateEntitlementPreviewTargets(lockedUsers, claims.Targets, authorized); err != nil {
		return nil, err
	}
	if err := lockEntitlementPreviewState(ctx, tx, ids); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	state, err := loadEntitlementPreviewState(ctx, tx, ids, now)
	if errors.Is(err, service.ErrUserNotFound) {
		return nil, service.ErrEntitlementPreviewConflict
	}
	if err != nil {
		return nil, err
	}
	stateHash, err := entitlementPreviewStateHash(state)
	if err != nil {
		return nil, err
	}
	if stateHash != claims.StateHash {
		return nil, service.ErrEntitlementPreviewConflict
	}
	if _, err := r.verifyEntitlementPreview(previewToken, actor, ids, tier, time.Now().UTC()); err != nil {
		return nil, err
	}
	policy, err := entitlementPreviewTargetPolicy(state, tier)
	if err != nil {
		return nil, err
	}
	if !policy.Enabled {
		return nil, service.ErrEntitlementDisabled
	}
	result := &service.EntitlementChangeResult{Tier: tier, Requested: len(ids), Version: policy.Version}

	for _, userID := range ids {
		var oldTier string
		var oldVersion int64
		var expires sql.NullTime
		err := tx.QueryRowContext(ctx, `SELECT tier, version, expires_at FROM user_entitlements WHERE user_id = $1 FOR UPDATE`, userID).Scan(&oldTier, &oldVersion, &expires)
		if errors.Is(err, sql.ErrNoRows) {
			oldTier, oldVersion = service.EntitlementTierStandard, 0
		} else if err != nil {
			return nil, fmt.Errorf("lock user entitlement: %w", err)
		}
		if oldTier == tier && (!expires.Valid || expires.Time.After(now)) {
			result.Unchanged++
			continue
		}
		newVersion := oldVersion + 1
		if newVersion < 1 {
			newVersion = 1
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO user_entitlements (user_id, tier, source, version, updated_by, updated_at)
VALUES ($1, $2, 'tier_grant', $3, NULLIF($4, 0), NOW())
ON CONFLICT (user_id) DO UPDATE SET tier = EXCLUDED.tier, source = EXCLUDED.source, version = EXCLUDED.version, expires_at = NULL, updated_by = EXCLUDED.updated_by, updated_at = NOW()`,
			userID, tier, newVersion, actorUserID); err != nil {
			return nil, fmt.Errorf("apply user entitlement: %w", err)
		}
		detail, _ := json.Marshal(map[string]any{
			"reason": reason, "request_id": requestID,
			"preserved_manual_groups": true, "preserved_subscriptions": true,
			"previous_tier": oldTier, "tier": tier,
		})
		if _, err := tx.ExecContext(ctx, `
INSERT INTO entitlement_audit_logs (actor_user_id, target_user_id, tier, action, old_version, new_version, detail)
VALUES (NULLIF($1, 0), $2, $3, 'tier_change', NULLIF($4, 0), $5, $6::jsonb)`, actorUserID, userID, tier, oldVersion, newVersion, string(detail)); err != nil {
			return nil, fmt.Errorf("write entitlement audit: %w", err)
		}
		result.Changed++
		if newVersion > result.Version {
			result.Version = newVersion
		}
	}
	serialized, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO entitlement_change_requests(actor_user_id,request_id,fingerprint,result) VALUES($1,$2,$3,$4::jsonb)`, actorUserID, requestID, fingerprint, string(serialized)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit entitlement change: %w", err)
	}
	return result, nil
}

func (r *entitlementRepository) ListEntitlementTierPolicies(ctx context.Context) ([]service.EntitlementTierPolicy, error) {
	db, err := r.requireDB()
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT tier, display_name, enabled, version FROM entitlement_tiers ORDER BY CASE tier WHEN 'standard' THEN 0 ELSE 1 END`)
	if err != nil {
		return nil, fmt.Errorf("list entitlement tiers: %w", err)
	}
	tiers := make([]service.EntitlementTierPolicy, 0, 2)
	for rows.Next() {
		var item service.EntitlementTierPolicy
		if err := rows.Scan(&item.Tier, &item.DisplayName, &item.Enabled, &item.Version); err != nil {
			return nil, joinRowsCloseError(rows, err)
		}
		item.Groups = []service.EntitlementTierGroupPolicy{}
		tiers = append(tiers, item)
	}
	if err := joinRowsCloseError(rows, rows.Err()); err != nil {
		return nil, err
	}
	for i := range tiers {
		groups, err := r.tierGroupPolicies(ctx, db, tiers[i].Tier)
		if err != nil {
			return nil, err
		}
		tiers[i].Groups = groups
	}
	return tiers, nil
}

func (r *entitlementRepository) UpdateEntitlementTierPolicy(ctx context.Context, input service.UpdateEntitlementTierPolicyInput, actorUserID int64) (*service.EntitlementTierPolicy, error) {
	input.Tier = service.NormalizeEntitlementWriteTier(input.Tier)
	if input.Tier == "" {
		return nil, service.ErrEntitlementTierUnknown
	}
	if strings.TrimSpace(input.Reason) == "" {
		return nil, service.ErrEntitlementReasonRequired
	}
	if strings.TrimSpace(input.RequestID) == "" || len(input.RequestID) > 116 {
		return nil, service.ErrEntitlementRequestIDRequired
	}
	if input.ExpectedVersion == nil || *input.ExpectedVersion < 1 {
		return nil, service.ErrEntitlementPolicyInvalid
	}
	if actorUserID <= 0 {
		return nil, service.ErrAdminPermissionDenied
	}
	db, err := r.requireDB()
	if err != nil {
		return nil, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin entitlement tier update: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Take the actor FK lock before policy locks, matching assignment's
	// user-before-policy order and avoiding a cycle on the idempotency row FK.
	var actorID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id=$1 AND deleted_at IS NULL FOR KEY SHARE`, actorUserID).Scan(&actorID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrAdminPermissionDenied
		}
		return nil, err
	}
	groups, err := normalizeTierGroups(input.Groups)
	if err != nil {
		return nil, err
	}
	input.Groups = groups
	rawInput, _ := json.Marshal(input)
	sum := sha256.Sum256(rawInput)
	fingerprint := hex.EncodeToString(sum[:])
	requestID := "tier-policy:" + input.RequestID
	lockKey := fmt.Sprintf("entitlement-change:%d:%s", actorUserID, requestID)
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, lockKey); err != nil {
		return nil, err
	}
	var recorded string
	var body []byte
	replayErr := tx.QueryRowContext(ctx, `SELECT fingerprint,result FROM entitlement_change_requests WHERE actor_user_id=$1 AND request_id=$2`, actorUserID, requestID).Scan(&recorded, &body)
	if replayErr == nil {
		if recorded != fingerprint {
			return nil, service.ErrEntitlementIdempotencyConflict
		}
		var result service.EntitlementTierPolicy
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, err
		}
		return &result, tx.Commit()
	}
	if !errors.Is(replayErr, sql.ErrNoRows) {
		return nil, replayErr
	}

	var currentName string
	var currentEnabled bool
	var currentVersion int64
	if err := tx.QueryRowContext(ctx, `SELECT display_name, enabled, version FROM entitlement_tiers WHERE tier = $1 FOR UPDATE`, input.Tier).Scan(&currentName, &currentEnabled, &currentVersion); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrEntitlementTierUnknown
		}
		return nil, fmt.Errorf("lock entitlement tier policy: %w", err)
	}

	groups, err = normalizeTierGroups(input.Groups)
	if err != nil {
		return nil, err
	}
	if *input.ExpectedVersion != currentVersion {
		return nil, service.ErrEntitlementVersionConflict
	}
	if len(groups) > 0 {
		ids := make([]int64, 0, len(groups))
		for _, group := range groups {
			ids = append(ids, group.GroupID)
		}
		rows, err := tx.QueryContext(ctx, `SELECT id FROM groups WHERE id=ANY($1) AND deleted_at IS NULL ORDER BY id FOR KEY SHARE`, pq.Array(ids))
		if err != nil {
			return nil, err
		}
		found := 0
		for rows.Next() {
			found++
		}
		if err := joinRowsCloseError(rows, rows.Err()); err != nil {
			return nil, err
		}
		if found != len(ids) {
			return nil, service.ErrEntitlementPolicyInvalid
		}
	}
	newVersion := currentVersion + 1
	if _, err := tx.ExecContext(ctx, `UPDATE entitlement_tiers SET display_name = $2, enabled = $3, version = $4, updated_at = NOW() WHERE tier = $1`, input.Tier, input.DisplayName, input.Enabled, newVersion); err != nil {
		return nil, fmt.Errorf("update entitlement tier policy: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM entitlement_tier_groups WHERE tier = $1`, input.Tier); err != nil {
		return nil, fmt.Errorf("clear entitlement tier groups: %w", err)
	}
	for _, group := range groups {
		source := strings.TrimSpace(group.Source)
		if source == "" {
			source = "tier"
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO entitlement_tier_groups (tier, group_id, rate_multiplier, source) VALUES ($1, $2, $3, $4)`, input.Tier, group.GroupID, group.RateMultiplier, source); err != nil {
			return nil, fmt.Errorf("insert entitlement tier group: %w", err)
		}
	}
	detail, _ := json.Marshal(map[string]any{
		"reason": input.Reason, "request_id": input.RequestID, "tier": input.Tier,
		"before": map[string]any{"display_name": currentName, "enabled": currentEnabled, "version": currentVersion},
		"after":  map[string]any{"display_name": input.DisplayName, "enabled": input.Enabled, "version": newVersion, "groups": groups},
	})
	if _, err := tx.ExecContext(ctx, `
INSERT INTO entitlement_audit_logs (actor_user_id, target_user_id, tier, action, old_version, new_version, detail)
VALUES (NULLIF($1, 0), NULLIF($2, 0), $3, 'tier_config_change', $4, $5, $6::jsonb)`, actorUserID, actorUserID, input.Tier, currentVersion, newVersion, string(detail)); err != nil {
		return nil, fmt.Errorf("write entitlement tier audit: %w", err)
	}
	result := &service.EntitlementTierPolicy{Tier: input.Tier, DisplayName: input.DisplayName, Enabled: input.Enabled, Version: newVersion, Groups: groups}
	serialized, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO entitlement_change_requests(actor_user_id,request_id,fingerprint,result) VALUES($1,$2,$3,$4::jsonb)`, actorUserID, requestID, fingerprint, string(serialized)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit entitlement tier update: %w", err)
	}
	return &service.EntitlementTierPolicy{Tier: input.Tier, DisplayName: input.DisplayName, Enabled: input.Enabled, Version: newVersion, Groups: groups}, nil
}

func normalizeTierGroups(groups []service.EntitlementTierGroupPolicy) ([]service.EntitlementTierGroupPolicy, error) {
	out := make([]service.EntitlementTierGroupPolicy, 0, len(groups))
	seen := make(map[int64]struct{}, len(groups))
	for _, group := range groups {
		if group.GroupID <= 0 {
			return nil, fmt.Errorf("%w: group id must be positive", service.ErrEntitlementPolicyInvalid)
		}
		if group.RateMultiplier != nil && (math.IsNaN(*group.RateMultiplier) || math.IsInf(*group.RateMultiplier, 0) || *group.RateMultiplier < 0 || *group.RateMultiplier > 1000) {
			return nil, fmt.Errorf("%w: multiplier must be finite and between 0 and 1000", service.ErrEntitlementPolicyInvalid)
		}
		if _, ok := seen[group.GroupID]; ok {
			return nil, fmt.Errorf("%w: duplicate group id %d", service.ErrEntitlementPolicyInvalid, group.GroupID)
		}
		seen[group.GroupID] = struct{}{}
		out = append(out, group)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].GroupID < out[j].GroupID })
	return out, nil
}

func (r *entitlementRepository) tierGroupPolicies(ctx context.Context, db entitlementQueryer, tier string) (policies []service.EntitlementTierGroupPolicy, err error) {
	rows, err := db.QueryContext(ctx, `SELECT group_id, rate_multiplier, source FROM entitlement_tier_groups WHERE tier = $1 ORDER BY group_id`, tier)
	if err != nil {
		return nil, fmt.Errorf("list entitlement tier groups: %w", err)
	}
	defer func() {
		err = joinRowsCloseError(rows, err)
	}()
	return scanTierGroupPolicies(rows)
}

func scanTierGroupPolicies(rows *sql.Rows) ([]service.EntitlementTierGroupPolicy, error) {
	out := []service.EntitlementTierGroupPolicy{}
	for rows.Next() {
		var group service.EntitlementTierGroupPolicy
		var rate sql.NullFloat64
		if err := rows.Scan(&group.GroupID, &rate, &group.Source); err != nil {
			return nil, err
		}
		if rate.Valid {
			value := rate.Float64
			group.RateMultiplier = &value
		}
		out = append(out, group)
	}
	return out, rows.Err()
}

func (r *entitlementRepository) tierGroups(ctx context.Context, db entitlementQueryer, tier string) ([]int64, error) {
	return queryInt64Column(ctx, db, `SELECT group_id FROM entitlement_tier_groups WHERE tier = $1 ORDER BY group_id`, tier)
}

func queryInt64Column(ctx context.Context, db entitlementQueryer, query string, args ...any) (values []int64, err error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = joinRowsCloseError(rows, err)
	}()
	out := []int64{}
	for rows.Next() {
		var value int64
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, rows.Err()
}

func mergeInt64Groups(groups ...[]int64) []int64 {
	seen := map[int64]struct{}{}
	out := []int64{}
	for _, list := range groups {
		for _, id := range list {
			if id <= 0 {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	return out
}

func rankEntitlementTier(tier string) int {
	if tier == service.EntitlementTierPremium {
		return 2
	}
	return 1
}

func mapKeys(in map[int64]struct{}) []int64 {
	out := make([]int64, 0, len(in))
	for key := range in {
		out = append(out, key)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func entitlementChangeFingerprint(ids []int64, tier, reason string, previewToken ...string) string {
	ordered := append([]int64(nil), ids...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	guardHash := ""
	if len(previewToken) > 0 && previewToken[0] != "" {
		sum := sha256.Sum256([]byte(previewToken[0]))
		guardHash = hex.EncodeToString(sum[:])
	}
	// Omit an absent guard to retain exact replay of completed pre-P3.5
	// requests. A new request without a guard is still rejected after lookup.
	raw, _ := json.Marshal(struct {
		Users            []int64
		Tier, Reason     string
		PreviewTokenHash string `json:",omitempty"`
	}{ordered, tier, reason, guardHash})
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func (r *entitlementRepository) ResolveEntitlementTierNames(ctx context.Context, ids []int64) (names map[int64]string, err error) {
	result := map[int64]string{}
	if len(ids) == 0 {
		return result, nil
	}
	rows, err := r.sqlDB.QueryContext(ctx, `SELECT u.id,CASE WHEN EXISTS (
      SELECT 1 FROM (SELECT user_id,tier,expires_at FROM user_entitlements UNION ALL SELECT user_id,tier,expires_at FROM user_entitlement_grants) e
      JOIN entitlement_tiers t ON t.tier=e.tier AND t.enabled=TRUE
      WHERE e.user_id=u.id AND e.tier='premium' AND (e.expires_at IS NULL OR e.expires_at>NOW())
    ) THEN 'premium' ELSE 'standard' END FROM users u WHERE u.id=ANY($1)`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer func() {
		err = joinRowsCloseError(rows, err)
	}()
	for rows.Next() {
		var id int64
		var tier string
		if err := rows.Scan(&id, &tier); err != nil {
			return nil, err
		}
		result[id] = tier
	}
	return result, rows.Err()
}
