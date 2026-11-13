package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

const (
	opsCleanupJobName = "ops_cleanup"

	opsCleanupLeaderLockKeyDefault = "ops:cleanup:leader"
	opsCleanupLeaderLockTTLDefault = 30 * time.Minute
)

var opsCleanupCronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

var opsCleanupReleaseScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`)

// OpsCleanupService periodically deletes old ops data to prevent unbounded DB growth.
//
// - Scheduling: 5-field cron spec (minute hour dom month dow).
// - Multi-instance: best-effort Redis leader lock so only one node runs cleanup.
// - Safety: deletes in batches to avoid long transactions.
//
// 附带：在 runCleanupOnce 末尾调用 ChannelMonitorService.RunDailyMaintenance，
// 统一共享 cron schedule + leader lock + heartbeat，避免再引一套调度。
type OpsCleanupService struct {
	opsRepo           OpsRepository
	db                *sql.DB
	redisClient       *redis.Client
	cfg               *config.Config
	channelMonitorSvc *ChannelMonitorService
	settingRepo       SettingRepository
	usageCleanup      *UsageCleanupService

	instanceID string

	// mu 守护 cron 实例切换 + effective 配置切换。
	// 这里不再用 startOnce/stopOnce，是因为 Reload 需要"停旧 cron 重启新 cron"，
	// 而 Once 一旦触发就无法再次执行；改为 started/stopped 布尔配合 mu。
	mu               sync.Mutex
	cron             *cron.Cron
	cronSchedule     string
	started          bool
	stopped          bool
	effective        config.OpsCleanupConfig
	effectiveTargets map[string]OpsRetentionPolicy

	warnNoRedisOnce sync.Once
}

func NewOpsCleanupService(
	opsRepo OpsRepository,
	db *sql.DB,
	redisClient *redis.Client,
	cfg *config.Config,
	channelMonitorSvc *ChannelMonitorService,
	settingRepo SettingRepository,
	usageCleanup ...*UsageCleanupService,
) *OpsCleanupService {
	svc := &OpsCleanupService{
		opsRepo:           opsRepo,
		db:                db,
		redisClient:       redisClient,
		cfg:               cfg,
		channelMonitorSvc: channelMonitorSvc,
		settingRepo:       settingRepo,
		instanceID:        uuid.NewString(),
	}
	if len(usageCleanup) > 0 {
		svc.usageCleanup = usageCleanup[0]
	}
	return svc
}

// Start 首次启动 cron 调度。Enabled / Schedule 由 effective 配置决定（settings 优先 cfg）。
// 重复调用幂等。
func (s *OpsCleanupService) Start() {
	if s == nil {
		return
	}
	if s.opsRepo == nil || s.db == nil {
		logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] not started (missing deps)")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started || s.stopped {
		return
	}
	s.started = true
	if err := s.applyScheduleLocked(context.Background()); err != nil {
		logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] not started: %v", err)
	}
}

// Stop 关闭 cron。幂等。
func (s *OpsCleanupService) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	stopped := s.stopCronLocked()
	s.mu.Unlock()
	// A finishing cron job may need mu. Wait only after releasing it.
	if stopped != nil {
		select {
		case <-stopped.Done():
		case <-time.After(opsCleanupCronStopTimeout):
			logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] cron stop timed out")
		}
	}
}

// stopCronLocked detaches the cron without waiting for jobs that may need mu.
func (s *OpsCleanupService) stopCronLocked() context.Context {
	if s.cron == nil {
		return nil
	}
	stopped := s.cron.Stop()
	s.cron = nil
	s.cronSchedule = ""
	return stopped
}

// applyScheduleLocked 重新计算 effective 配置并按其 schedule 重建 cron。调用方持锁。
// 若 effective.Enabled=false（用户在 UI 关闭清理），停旧 cron 后直接返回，不创建新 cron。
func (s *OpsCleanupService) applyScheduleLocked(ctx context.Context) error {
	previousEffective := s.effective
	previousTargets := s.effectiveTargets
	previousCron := s.cron
	if err := s.computeEffectiveLocked(ctx); err != nil {
		return err
	}

	if !s.effective.Enabled {
		s.stopCronLocked()
		logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] cron disabled by settings")
		return nil
	}

	schedule := strings.TrimSpace(s.effective.Schedule)
	if schedule == "" {
		schedule = opsCleanupDefaultSchedule
	}
	if s.cron != nil && s.cronSchedule == schedule {
		return nil
	}

	loc := time.Local
	if s.cfg != nil && strings.TrimSpace(s.cfg.Timezone) != "" {
		if parsed, err := time.LoadLocation(strings.TrimSpace(s.cfg.Timezone)); err == nil && parsed != nil {
			loc = parsed
		}
	}

	c := cron.New(cron.WithParser(opsCleanupCronParser), cron.WithLocation(loc))
	if _, err := c.AddFunc(schedule, func() { s.runScheduled() }); err != nil {
		s.effective = previousEffective
		s.effectiveTargets = previousTargets
		s.cron = previousCron
		return fmt.Errorf("invalid schedule %q: %w", schedule, err)
	}
	s.stopCronLocked()
	c.Start()
	s.cron = c
	s.cronSchedule = schedule
	logger.LegacyPrintf("service.ops_cleanup",
		"[OpsCleanup] scheduled (schedule=%q tz=%s retention_days=err:%d/min:%d/hour:%d)",
		schedule, loc.String(),
		s.effective.ErrorLogRetentionDays,
		s.effective.MinuteMetricsRetentionDays,
		s.effective.HourlyMetricsRetentionDays,
	)
	return nil
}

// Reload 重新读取 ops_advanced_settings.data_retention 并按新配置重建 cron。
// 适用于 admin 在 UI 修改清理设置后立即生效（schedule / enabled 改动需要 Reload；
// retention 改动 runScheduled 顶部也会刷新，下一次触发即生效）。
// 若 service 还未 Start 或已 Stop，Reload 不做任何事。
func (s *OpsCleanupService) Reload(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started || s.stopped {
		return nil
	}
	return s.applyScheduleLocked(ctx)
}

// computeEffectiveLocked 计算"生效配置"并写入 s.effective。调用方持锁。
//
// 优先级：UI 写入的 settings.ops_advanced_settings.data_retention（权威）覆盖 cfg.Ops.Cleanup 的副本。
//   - Enabled：settings 直接覆盖
//   - Schedule：settings 非空时覆盖，否则保留 cfg
//   - *RetentionDays：settings >=0 时覆盖（0=禁用），<0 沿用 cfg
//
// Missing settings use cfg; read/parse errors preserve the previous state for retry.
func (s *OpsCleanupService) computeEffectiveLocked(ctx context.Context) (err error) {
	base := config.OpsCleanupConfig{}
	if s.cfg != nil {
		base = s.cfg.Ops.Cleanup
	}
	targets := defaultOpsRetentionPolicies(base.ErrorLogRetentionDays, base.MinuteMetricsRetentionDays, base.HourlyMetricsRetentionDays)
	defer func() {
		if err == nil {
			s.effective = base
			s.effectiveTargets = targets
		}
	}()

	if s.settingRepo == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	values, err := s.settingRepo.GetMultiple(ctx, []string{SettingKeyOpsAdvancedSettings, SettingKeyAuditLogRetentionDays})
	if err != nil {
		return fmt.Errorf("read cleanup settings: %w", err)
	}
	auditDays := parseAuditLogRetentionDays(values[SettingKeyAuditLogRetentionDays])
	targets["audit_logs"] = OpsRetentionPolicy{Enabled: auditDays > 0, RetentionDays: auditDays}
	raw := strings.TrimSpace(values[SettingKeyOpsAdvancedSettings])
	if raw == "" {
		return nil
	}
	adv := *defaultOpsAdvancedSettings()
	if err := json.Unmarshal([]byte(raw), &adv); err != nil {
		return fmt.Errorf("parse cleanup settings: %w", err)
	}
	// 缓存原始（可能为负）retention 值：normalizeOpsAdvancedSettings 会把负数 clamp 成默认值，
	// 覆盖判断需用原始值才能保留“<0 沿用 cfg”的语义。
	rawErr := adv.DataRetention.ErrorLogRetentionDays
	rawMin := adv.DataRetention.MinuteMetricsRetentionDays
	rawHour := adv.DataRetention.HourlyMetricsRetentionDays
	normalizeOpsAdvancedSettings(&adv)
	dr := adv.DataRetention
	base.Enabled = dr.CleanupEnabled
	if sched := strings.TrimSpace(dr.CleanupSchedule); sched != "" {
		base.Schedule = sched
	}
	if rawErr >= 0 {
		base.ErrorLogRetentionDays = rawErr
	}
	if rawMin >= 0 {
		base.MinuteMetricsRetentionDays = rawMin
	}
	if rawHour >= 0 {
		base.HourlyMetricsRetentionDays = rawHour
	}
	targets = make(map[string]OpsRetentionPolicy, len(dr.Targets)+1)
	for key, policy := range dr.Targets {
		targets[key] = policy
	}
	// Keep the existing independent retention_days value (0 means forever),
	// but run it under the center's switch, cron, and leader lock.
	targets["audit_logs"] = OpsRetentionPolicy{Enabled: auditDays > 0, RetentionDays: auditDays}
	return nil
}

func (s *OpsCleanupService) snapshotEffectiveTargets() map[string]OpsRetentionPolicy {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]OpsRetentionPolicy, len(s.effectiveTargets))
	for key, policy := range s.effectiveTargets {
		out[key] = policy
	}
	return out
}

// refreshEffectiveBeforeRun updates retention before a run. A stale schedule
// is skipped until the runtime refresh reloads the cron.
func (s *OpsCleanupService) refreshEffectiveBeforeRun(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.computeEffectiveLocked(ctx)
}

func (s *OpsCleanupService) runScheduled() {
	if s == nil || s.db == nil || s.opsRepo == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), opsCleanupRunTimeout)
	defer cancel()

	// 让 retention 改动当次生效（schedule/enabled 改动需要 Reload）。
	if err := s.refreshEffectiveBeforeRun(ctx); err != nil {
		logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] settings refresh failed: %v", err)
		return
	}
	s.mu.Lock()
	enabled := s.effective.Enabled && !s.stopped
	if s.cronSchedule != "" && s.cronSchedule != strings.TrimSpace(s.effective.Schedule) {
		enabled = false // Do not execute a stale cron while waiting for Reload.
	}
	s.mu.Unlock()
	if !enabled {
		return
	}

	release, ok := s.tryAcquireLeaderLock(ctx)
	if !ok {
		return
	}
	if release != nil {
		defer release()
	}

	startedAt := time.Now().UTC()
	runAt := startedAt

	counts, err := s.runCleanupOnce(ctx)
	status, errText := "succeeded", ""
	if err != nil {
		status = "failed"
		errText = truncateString(err.Error(), 500)
	}
	auditCtx, auditCancel := context.WithTimeout(context.WithoutCancel(ctx), dataCleanupAuditTimeout)
	defer auditCancel()
	_, _ = s.db.ExecContext(auditCtx, `INSERT INTO data_cleanup_audits(auth_method,category,cleanup_mode,filters,deleted_rows,status,error_message,started_at,finished_at)
	 VALUES('scheduler','scheduled','range',jsonb_build_object('result',$1::text),$2,$3,$4,$5,NOW())`, counts.String(), counts.Total(), status, errText, startedAt)
	if err != nil {
		s.recordHeartbeatError(runAt, time.Since(startedAt), counts, err)
		logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] cleanup failed: %v; %s", err, counts.String())
		return
	}
	s.recordHeartbeatSuccess(runAt, time.Since(startedAt), counts)
	logger.L().Info("[OpsCleanup] cleanup complete",
		zap.String("component", "service.ops_cleanup"),
		zap.String("deleted_counts", counts.String()),
	)
}

func (s *OpsCleanupService) runCleanupOnce(ctx context.Context) (opsCleanupDeletedCounts, error) {
	out := opsCleanupDeletedCounts{}
	if s == nil || s.db == nil || s.cfg == nil {
		return out, nil
	}

	policies := s.snapshotEffectiveTargets()
	now := time.Now().UTC()

	targets := []opsCleanupTarget{
		{policies["ops_error_logs"].RetentionDays, "ops_error_logs", "created_at", false, &out.errorLogs},
		{policies["ops_ingress_reject_aggregates"].RetentionDays, "ops_ingress_reject_aggregates", "bucket_start", false, &out.ingressRejects},
		{policies["ops_alert_events"].RetentionDays, "ops_alert_events", "created_at", false, &out.alertEvents},
		{policies["ops_system_log_cleanup_audits"].RetentionDays, "ops_system_log_cleanup_audits", "created_at", false, &out.logAudits},
		{policies["ops_system_metrics"].RetentionDays, "ops_system_metrics", "created_at", false, &out.systemMetrics},
		{policies["ops_metrics_hourly"].RetentionDays, "ops_metrics_hourly", "bucket_start", false, &out.hourlyPreagg},
		{policies["ops_metrics_daily"].RetentionDays, "ops_metrics_daily", "bucket_date", true, &out.dailyPreagg},
	}

	for _, t := range targets {
		if policy := policies[t.table]; !policy.Enabled || policy.RetentionDays <= 0 {
			continue
		}
		cutoff, truncate, ok := opsCleanupPlan(now, t.retentionDays)
		if !ok {
			continue
		}
		n, err := opsCleanupRunOne(ctx, s.db, truncate, cutoff, t.table, t.timeCol, t.castDate, opsCleanupBatchSize)
		*t.counter = n
		if err != nil {
			return out, err
		}
	}

	if policy := policies["usage_logs"]; policy.Enabled && policy.RetentionDays > 0 && s.usageCleanup != nil {
		deleted, err := s.usageCleanup.RunRetention(ctx, UsageCompactionEnd(now))
		out.usageLogs = deleted
		if err != nil {
			return out, err
		}
	}
	for _, target := range []struct {
		category string
		counter  *int64
	}{
		{"server_auto_retry", &out.serverAutoRetry},
		{"http_access", &out.httpAccess},
		{"audit_statistics", &out.auditStatistics},
	} {
		category := target.category
		policy := policies[category]
		if !policy.Enabled || policy.RetentionDays <= 0 {
			continue
		}
		n, err := cleanupCategoryRetention(ctx, s.db, category, now.AddDate(0, 0, -policy.RetentionDays))
		*target.counter = n
		if err != nil {
			return out, err
		}
	}
	if policy := policies["audit_logs"]; policy.Enabled && policy.RetentionDays > 0 {
		n, err := deleteOldRowsByID(ctx, s.db, "audit_logs", "created_at", now.AddDate(0, 0, -policy.RetentionDays), opsCleanupBatchSize, false)
		out.auditLogs = n
		if err != nil {
			return out, err
		}
	}
	if policy := policies["ops_system_logs"]; policy.Enabled && policy.RetentionDays > 0 {
		n, err := cleanupRuntimeRetention(ctx, s.db, policies, now)
		out.systemLogs = n
		if err != nil {
			return out, err
		}
	}

	// Channel monitor 每日维护（聚合昨日明细 + 软删过期明细/聚合）。
	// 失败只记日志，不影响 ops 清理的成功状态（与 ops 各步骤风格一致）；
	// 维护本身已经把每步错误打到 slog，heartbeat result 不再分项记录。
	if s.channelMonitorSvc != nil {
		if err := s.channelMonitorSvc.RunDailyMaintenance(ctx); err != nil {
			logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] channel monitor maintenance failed: %v", err)
		}
	}

	return out, nil
}

func (s *OpsCleanupService) tryAcquireLeaderLock(ctx context.Context) (func(), bool) {
	if s == nil {
		return nil, false
	}
	// In simple run mode, assume single instance.
	if s.cfg != nil && s.cfg.RunMode == config.RunModeSimple {
		return nil, true
	}

	key := opsCleanupLeaderLockKeyDefault
	ttl := opsCleanupLeaderLockTTLDefault

	// Prefer Redis leader lock when available, but avoid stampeding the DB when Redis is flaky by
	// falling back to a DB advisory lock.
	if s.redisClient != nil {
		ok, err := s.redisClient.SetNX(ctx, key, s.instanceID, ttl).Result()
		if err == nil {
			if !ok {
				return nil, false
			}
			return func() {
				_, _ = opsCleanupReleaseScript.Run(ctx, s.redisClient, []string{key}, s.instanceID).Result()
			}, true
		}
		// Redis error: fall back to DB advisory lock.
		s.warnNoRedisOnce.Do(func() {
			logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] leader lock SetNX failed; falling back to DB advisory lock: %v", err)
		})
	} else {
		s.warnNoRedisOnce.Do(func() {
			logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] redis not configured; using DB advisory lock")
		})
	}

	release, ok := tryAcquireDBAdvisoryLock(ctx, s.db, hashAdvisoryLockID(key))
	if !ok {
		return nil, false
	}
	return release, true
}

func (s *OpsCleanupService) recordHeartbeatSuccess(runAt time.Time, duration time.Duration, counts opsCleanupDeletedCounts) {
	if s == nil || s.opsRepo == nil {
		return
	}
	now := time.Now().UTC()
	durMs := duration.Milliseconds()
	result := truncateString(counts.String(), 2048)
	ctx, cancel := context.WithTimeout(context.Background(), opsCleanupHeartbeatTimeout)
	defer cancel()
	_ = s.opsRepo.UpsertJobHeartbeat(ctx, &OpsUpsertJobHeartbeatInput{
		JobName:        opsCleanupJobName,
		LastRunAt:      &runAt,
		LastSuccessAt:  &now,
		LastDurationMs: &durMs,
		LastResult:     &result,
	})
}

func (s *OpsCleanupService) recordHeartbeatError(runAt time.Time, duration time.Duration, counts opsCleanupDeletedCounts, err error) {
	if s == nil || s.opsRepo == nil || err == nil {
		return
	}
	now := time.Now().UTC()
	durMs := duration.Milliseconds()
	msg := truncateString(err.Error(), 2048)
	result := truncateString(counts.String(), 2048)
	ctx, cancel := context.WithTimeout(context.Background(), opsCleanupHeartbeatTimeout)
	defer cancel()
	_ = s.opsRepo.UpsertJobHeartbeat(ctx, &OpsUpsertJobHeartbeatInput{
		JobName:        opsCleanupJobName,
		LastRunAt:      &runAt,
		LastErrorAt:    &now,
		LastError:      &msg,
		LastResult:     &result,
		LastDurationMs: &durMs,
	})
}
