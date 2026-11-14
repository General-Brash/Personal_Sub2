package middleware

import (
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/gatewaytiming"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const gatewayTimingStartTimeKey = "gateway_timing_logger_started_at"

// BeginGatewayTiming activates timing from application Logger entry, excluding
// client and reverse-proxy waiting before arrival. Without Logger, it starts now.
func BeginGatewayTiming(c *gin.Context) {
	if c == nil || c.Request == nil || gatewaytiming.FromContext(c.Request.Context()) != nil {
		return
	}
	startTime, ok := c.Get(gatewayTimingStartTimeKey)
	startedAt, valid := startTime.(time.Time)
	if !ok || !valid {
		startedAt = time.Now()
	}
	collector := gatewaytiming.New(startedAt)
	c.Request = c.Request.WithContext(gatewaytiming.WithCollector(c.Request.Context(), collector))
}

// Logger 请求日志中间件
func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 开始时间
		startTime := time.Now()
		c.Set(gatewayTimingStartTimeKey, startTime)
		if c.Request != nil && c.Request.Method == http.MethodPost {
			switch c.FullPath() {
			case "/v1/responses", "/v1/responses/*subpath",
				"/responses", "/responses/*subpath",
				"/backend-api/codex/responses", "/backend-api/codex/responses/*subpath":
				// Start before composite routing reads the inbound body. Responses
				// handled by another gateway may have only ingress timing and no
				// OpenAI HTTP attempt.
				BeginGatewayTiming(c)
			}
		}

		// 请求路径
		path := c.Request.URL.Path

		// 处理请求
		c.Next()

		// 跳过健康检查等高频探针路径的日志
		if path == "/health" || path == "/setup/status" {
			return
		}

		endTime := time.Now()
		latency := endTime.Sub(startTime)

		method := c.Request.Method
		statusCode := c.Writer.Status()
		clientIP := ip.GetClientIP(c)
		protocol := c.Request.Proto
		accountID, hasAccountID := c.Request.Context().Value(ctxkey.AccountID).(int64)
		platform, _ := c.Request.Context().Value(ctxkey.Platform).(string)
		model, _ := c.Request.Context().Value(ctxkey.Model).(string)
		reason, rejected := GetIngressRejectReason(c)
		if rejected {
			recordIngressReject(c, reason)
			allowed, droppedSummary := globalIngressRejectAccessSampler.allow(endTime)
			if droppedSummary > 0 {
				logger.FromContext(c.Request.Context()).Info("ingress rejection access logs dropped",
					zap.String("component", "http.access"),
					zap.Uint64("dropped_count", droppedSummary),
					zap.Bool(logger.OpsSystemLogSkipField, true),
				)
			}
			if !allowed {
				return
			}
		}

		fields := []zap.Field{
			zap.String("component", "http.access"),
			zap.Int("status_code", statusCode),
			zap.Int64("latency_ms", latency.Milliseconds()),
			zap.String("client_ip", clientIP),
			zap.String("protocol", protocol),
			zap.String("method", method),
			zap.String("path", path),
		}
		if rejected {
			fields = append(fields,
				zap.String("ingress_reject_reason", string(reason)),
				zap.Bool(logger.OpsSystemLogSkipField, true),
			)
		}
		if hasAccountID && accountID > 0 {
			fields = append(fields, zap.Int64("account_id", accountID))
		}
		if platform != "" {
			fields = append(fields, zap.String("platform", platform))
		}
		if model != "" {
			fields = append(fields, zap.String("model", model))
		}

		l := logger.FromContext(c.Request.Context()).With(fields...)
		completionFields := []zap.Field{zap.Time("completed_at", endTime)}
		if gatewaytiming.FromContext(c.Request.Context()) != nil {
			completionFields = append(completionFields, zap.Any("gateway_timing", gatewaytiming.Snapshot(c.Request.Context(), endTime)))
		}
		l.Info("http request completed", completionFields...)

		if len(c.Errors) > 0 {
			l.Warn("http request contains gin errors", zap.String("errors", c.Errors.String()))
		}
	}
}
