package handler

import (
	"bytes"
	"embed"
	"html/template"
	"net/http"

	"github.com/gin-gonic/gin"
)

//go:embed oidc_templates/*.html
var oidcTemplateFS embed.FS

var oidcTemplates = template.Must(template.ParseFS(oidcTemplateFS, "oidc_templates/*.html"))

// oidcLoginView 驱动两步式登录页：Step 为 "email" 时只收邮箱，
// 为 "credentials" 时收密码，并按 ShowTOTP 决定是否显示两步验证码框。
type oidcLoginView struct {
	Tx       string
	CSRF     string
	Step     string
	Email    string
	ShowTOTP bool
	HasError bool
}

type oidcScopeItem struct {
	Key   string
	Label string
}

type oidcConsentViewData struct {
	Tx          string
	CSRF        string
	ClientName  string
	ClientOwner string
	Scopes      []oidcScopeItem
	UserEmail   string
	UserName    string
}

// oidcScopeLabels 把 OIDC scope 映射为面向用户的中文说明。
var oidcScopeLabels = map[string]string{
	"openid":         "确认你的登录身份",
	"profile":        "获取你的基本资料（用户名等）",
	"email":          "获取你的邮箱地址",
	"roles":          "获取你的角色权限",
	"offline_access": "在你离线时保持授权（长期访问）",
}

func oidcScopeItems(scopes []string) []oidcScopeItem {
	items := make([]oidcScopeItem, 0, len(scopes))
	for _, s := range scopes {
		label := oidcScopeLabels[s]
		if label == "" {
			label = s
		}
		items = append(items, oidcScopeItem{Key: s, Label: label})
	}
	return items
}

func (h *OIDCProviderHandler) renderTemplate(c *gin.Context, name string, data any) {
	var buf bytes.Buffer
	if err := oidcTemplates.ExecuteTemplate(&buf, name, data); err != nil {
		h.localError(c, err)
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", buf.Bytes())
}
