package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

const oidcAuditSecretPepper = "oidc-chat-interop-audit-secret-pepper-abcdefghijklmnopqrstuvwxyz"

func TestOIDCChatInteropAuditProviderDefaultsAndFrozenConfig(t *testing.T) {
	cfg, err := loadOIDCChatInteropConfig(t, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OIDCProvider.Enabled {
		t.Fatal("OIDC Provider must be disabled by default")
	}
	if cfg.OIDCProvider.Issuer != service.OIDCProviderIssuer {
		t.Fatalf("issuer = %q, want %q", cfg.OIDCProvider.Issuer, service.OIDCProviderIssuer)
	}
	if cfg.OIDCProvider.SigningAlg != service.OIDCSigningRS256 {
		t.Fatalf("signing algorithm = %q, want RS256", cfg.OIDCProvider.SigningAlg)
	}
	if !cfg.OIDCProvider.RequirePKCES256 || !cfg.OIDCProvider.AuthorizationResponseIssuer {
		t.Fatal("PKCE S256 and authorization response issuer must default to true")
	}
	if got := strings.Join(cfg.OIDCProvider.AllowedScopes, " "); got != "openid profile email roles offline_access" {
		t.Fatalf("allowed scopes = %q, want the frozen first-party scope set", got)
	}
	if cfg.OIDCProvider.Cookie.SessionName != "__Host-sub2_oidc_session" || cfg.OIDCProvider.Cookie.TransactionName != "__Host-sub2_oidc_tx" || !cfg.OIDCProvider.Cookie.Secure || !cfg.OIDCProvider.Cookie.HTTPOnly || cfg.OIDCProvider.Cookie.SameSite != "lax" {
		t.Fatalf("cookie contract = %#v, want Secure HttpOnly SameSite=Lax __Host cookies", cfg.OIDCProvider.Cookie)
	}

	base := `oidc_provider:
  enabled: true
  encryption_key: "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"
  secret_pepper: "` + oidcAuditSecretPepper + `"
`
	if _, err := loadOIDCChatInteropConfig(t, base); err != nil {
		t.Fatalf("valid enabled provider config rejected: %v", err)
	}
	invalid := map[string]string{
		"issuer":                     `issuer: "https://evil.example"`,
		"public host":                `public_host: "evil.example"`,
		"signing algorithm":          `signing_alg: "HS256"`,
		"key source":                 `signing_key_source: "memory"`,
		"PKCE":                       `require_pkce_s256: false`,
		"authorization response iss": `authorization_response_iss: false`,
		"scopes":                     `allowed_scopes: ["profile"]`,
		"session cookie": `cookie:
    session_name: "session"
    transaction_name: "__Host-sub2_oidc_tx"
    secure: true
    http_only: true
    same_site: "lax"`,
		"SameSite": `cookie:
    session_name: "__Host-sub2_oidc_session"
    transaction_name: "__Host-sub2_oidc_tx"
    secure: true
    http_only: true
    same_site: "strict"`,
	}
	for name, override := range invalid {
		t.Run(name, func(t *testing.T) {
			if _, err := loadOIDCChatInteropConfig(t, base+indentOIDCChatInteropYAML(override)); err == nil {
				t.Fatalf("invalid %s configuration was accepted", name)
			}
		})
	}
}

func TestOIDCChatInteropAuditDisabledProviderFailsClosedWithoutState(t *testing.T) {
	cfg := &config.Config{}
	svc := service.NewOIDCProviderService(nil, nil, nil, nil, cfg, nil, nil)
	if svc.ProviderEnabled() {
		t.Fatal("zero-value provider configuration must remain disabled")
	}
	if err := svc.RequireActiveSigningKey(context.Background()); !errors.Is(err, service.ErrOIDCProviderDisabled) {
		t.Fatalf("RequireActiveSigningKey error = %v, want provider-disabled", err)
	}
}

func TestOIDCChatInteropAuditBasicOnlyRejectsFormCredentials(t *testing.T) {
	formRequest := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader("grant_type=authorization_code&client_id=form-client&client_secret=form-secret"))
	formRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	formContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	formContext.Request = formRequest
	if !isOIDCFormRequest(formContext) {
		t.Fatal("form-encoded token request was not recognized")
	}
	if _, _, ok := oidcBasicClientCredentials(formContext); ok {
		t.Fatal("form credentials must not authenticate the token endpoint")
	}
	if err := formRequest.ParseForm(); err != nil {
		t.Fatal(err)
	}
	if !hasOIDCFormParameter(formContext, "client_id") || !hasOIDCFormParameter(formContext, "client_secret") {
		t.Fatal("form credential parameters were not visible to the rejection gate")
	}

	basicRequest := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader("grant_type=refresh_token"))
	basicRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	basicRequest.SetBasicAuth("first-party-client", "secret-value")
	basicContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	basicContext.Request = basicRequest
	clientID, secret, ok := oidcBasicClientCredentials(basicContext)
	if !ok || clientID != "first-party-client" || secret != "secret-value" {
		t.Fatalf("Basic credentials = %q/%q/%v, want the single Basic header", clientID, secret, ok)
	}
	if err := basicRequest.ParseForm(); err != nil {
		t.Fatal(err)
	}
	if hasOIDCFormParameter(basicContext, "client_id") || hasOIDCFormParameter(basicContext, "client_secret") {
		t.Fatal("Basic-only request unexpectedly contained form credentials")
	}

	duplicateBasic := httptest.NewRequest(http.MethodPost, "/oauth/token", nil)
	duplicateBasic.SetBasicAuth("client", "secret")
	duplicateBasic.Header.Add("Authorization", duplicateBasic.Header.Get("Authorization"))
	duplicateContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	duplicateContext.Request = duplicateBasic
	if _, _, ok := oidcBasicClientCredentials(duplicateContext); ok {
		t.Fatal("duplicate Authorization headers must fail closed")
	}
}

func TestOIDCChatInteropAuditNoStoreAndCrossSiteHeaders(t *testing.T) {
	h := &OIDCProviderHandler{}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	h.noStore(ctx)
	assertOIDCNoStoreHeaders(t, recorder)

	router := gin.New()
	router.Use(servermiddleware.SecurityHeaders(config.CSPConfig{}, nil))
	router.POST("/oauth/token", func(c *gin.Context) {
		h.noStore(c)
		c.Status(http.StatusOK)
	})
	request := httptest.NewRequest(http.MethodPost, "/oauth/token", nil)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("token test route status = %d, want 200", recorder.Code)
	}
	assertOIDCNoStoreHeaders(t, recorder)
	if got := recorder.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options = %q, want DENY", got)
	}
	if got := recorder.Header().Get("Referrer-Policy"); got == "" {
		t.Fatal("cross-site Referrer-Policy header is missing")
	}
}

func TestOIDCChatInteropAuditProviderHostBinding(t *testing.T) {
	router := gin.New()
	router.Use(servermiddleware.RequireOIDCPublicHost("auth.taffy.edu.kg"))
	router.GET("/oauth/token", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	good := httptest.NewRequest(http.MethodGet, "https://auth.taffy.edu.kg/oauth/token", nil)
	goodRecorder := httptest.NewRecorder()
	router.ServeHTTP(goodRecorder, good)
	if goodRecorder.Code != http.StatusNoContent {
		t.Fatalf("fixed issuer host status = %d, want 204", goodRecorder.Code)
	}

	bad := httptest.NewRequest(http.MethodGet, "https://evil.example/oauth/token", nil)
	badRecorder := httptest.NewRecorder()
	router.ServeHTTP(badRecorder, bad)
	if badRecorder.Code != http.StatusNotFound {
		t.Fatalf("wrong host status = %d, want 404", badRecorder.Code)
	}
	if !strings.Contains(badRecorder.Header().Get("Cache-Control"), "no-store") {
		t.Fatalf("wrong host response Cache-Control = %q, want no-store", badRecorder.Header().Get("Cache-Control"))
	}
}

func indentOIDCChatInteropYAML(contents string) string {
	lines := strings.Split(strings.TrimSuffix(contents, "\n"), "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != "" {
			lines[i] = "  " + line
		}
	}
	return strings.Join(lines, "\n") + "\n"
}
func loadOIDCChatInteropConfig(t *testing.T, contents string) (*config.Config, error) {
	t.Helper()
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("CONFIG_FILE", "")
	t.Setenv("DATA_DIR", "")
	t.Setenv("JWT_SECRET", strings.Repeat("x", 32))
	t.Setenv("OIDC_PROVIDER_SECRET_PEPPER", oidcAuditSecretPepper)
	if contents != "" {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			return nil, err
		}
		t.Setenv("CONFIG_FILE", path)
	}
	return config.Load()
}

func assertOIDCNoStoreHeaders(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()
	if !strings.Contains(recorder.Header().Get("Cache-Control"), "no-store") {
		t.Fatalf("Cache-Control = %q, want no-store", recorder.Header().Get("Cache-Control"))
	}
	if recorder.Header().Get("Pragma") != "no-cache" || recorder.Header().Get("Expires") != "0" {
		t.Fatalf("cache headers = Pragma=%q Expires=%q, want no-cache/0", recorder.Header().Get("Pragma"), recorder.Header().Get("Expires"))
	}
	if recorder.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", recorder.Header().Get("X-Content-Type-Options"))
	}
}
