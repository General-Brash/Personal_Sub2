package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const formActionTestOrigin = "https://rp.example.test"

// formActionDirectiveValues 按 ";" 拆指令、Fields 拆值，返回 form-action 指令的值列表。
// 用精确成员判断代替整串相等，避免受指令顺序与空白影响。
func formActionDirectiveValues(policy string) []string {
	for _, rawDirective := range strings.Split(policy, ";") {
		fields := strings.Fields(strings.TrimSpace(rawDirective))
		if len(fields) == 0 || fields[0] != "form-action" {
			continue
		}
		return fields[1:]
	}
	return nil
}

func formActionHasValue(policy, value string) bool {
	for _, v := range formActionDirectiveValues(policy) {
		if v == value {
			return true
		}
	}
	return false
}

func formActionCountValue(policy, value string) int {
	count := 0
	for _, v := range formActionDirectiveValues(policy) {
		if v == value {
			count++
		}
	}
	return count
}

// runFormActionHandler 按真实顺序执行：SecurityHeaders 中间件先设响应头，
// 路由 handler 内再调用 AllowFormActionOrigin 修改响应头。
func runFormActionHandler(t *testing.T, cfg config.CSPConfig, handler func(c *gin.Context)) *httptest.ResponseRecorder {
	t.Helper()
	router := gin.New()
	router.Use(SecurityHeaders(cfg, nil))
	router.GET("/oidc/authorize/confirm", func(c *gin.Context) {
		handler(c)
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/oidc/authorize/confirm", nil)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	return w
}

func TestAllowFormActionOriginAddsOriginToDefaultPolicy(t *testing.T) {
	w := runFormActionHandler(t, config.CSPConfig{Enabled: true, Policy: ""}, func(c *gin.Context) {
		AllowFormActionOrigin(c, formActionTestOrigin)
	})

	csp := w.Header().Get("Content-Security-Policy")
	require.NotEmpty(t, csp)
	t.Logf("final Content-Security-Policy header: %s", csp)

	values := formActionDirectiveValues(csp)
	require.NotEmpty(t, values, "form-action directive must exist, csp=%q", csp)
	assert.True(t, formActionHasValue(csp, "'self'"), "form-action must keep 'self', values=%v", values)
	assert.True(t, formActionHasValue(csp, formActionTestOrigin), "form-action must contain %s, values=%v", formActionTestOrigin, values)
}

func TestAllowFormActionOriginIsIdempotent(t *testing.T) {
	w := runFormActionHandler(t, config.CSPConfig{Enabled: true, Policy: ""}, func(c *gin.Context) {
		AllowFormActionOrigin(c, formActionTestOrigin)
		AllowFormActionOrigin(c, formActionTestOrigin)
	})

	csp := w.Header().Get("Content-Security-Policy")
	require.NotEmpty(t, csp)
	assert.Equal(t, 1, formActionCountValue(csp, formActionTestOrigin),
		"origin must appear exactly once in form-action, values=%v", formActionDirectiveValues(csp))
}

func TestAllowFormActionOriginNoOpsWhenCSPDisabled(t *testing.T) {
	w := runFormActionHandler(t, config.CSPConfig{Enabled: false}, func(c *gin.Context) {
		AllowFormActionOrigin(c, formActionTestOrigin)
	})

	assert.Empty(t, w.Header().Get("Content-Security-Policy"))
}

func TestAllowFormActionOriginPreservesNonce(t *testing.T) {
	var nonce string
	w := runFormActionHandler(t, config.CSPConfig{Enabled: true, Policy: ""}, func(c *gin.Context) {
		nonce = GetNonceFromContext(c)
		AllowFormActionOrigin(c, formActionTestOrigin)
	})

	require.NotEmpty(t, nonce, "middleware must set a nonce before handler runs")
	csp := w.Header().Get("Content-Security-Policy")
	assert.Contains(t, csp, "'nonce-"+nonce+"'", "nonce must survive form-action injection")
	assert.True(t, formActionHasValue(csp, formActionTestOrigin), "form-action must contain %s", formActionTestOrigin)
}

func TestAllowFormActionOriginCreatesDirectiveWhenMissing(t *testing.T) {
	w := runFormActionHandler(t, config.CSPConfig{Enabled: true, Policy: "default-src 'self'; script-src 'self'"}, func(c *gin.Context) {
		AllowFormActionOrigin(c, formActionTestOrigin)
	})

	csp := w.Header().Get("Content-Security-Policy")
	require.NotEmpty(t, csp)
	values := formActionDirectiveValues(csp)
	require.NotEmpty(t, values, "form-action directive must be created, csp=%q", csp)
	assert.True(t, formActionHasValue(csp, formActionTestOrigin), "form-action must contain %s, values=%v", formActionTestOrigin, values)
}

func TestAllowFormActionOriginRejectsOriginWithCSPDelimiters(t *testing.T) {
	for _, origin := range []string{"https://evil.example;script-src", "https://*.evil.example"} {
		w := runFormActionHandler(t, config.CSPConfig{Enabled: true, Policy: ""}, func(c *gin.Context) {
			AllowFormActionOrigin(c, origin)
		})

		csp := w.Header().Get("Content-Security-Policy")
		require.NotEmpty(t, csp)
		assert.NotContains(t, csp, "evil.example", "origin %q must be rejected, csp=%q", origin, csp)
		assert.Equal(t, []string{"'self'"}, formActionDirectiveValues(csp))
	}
}
