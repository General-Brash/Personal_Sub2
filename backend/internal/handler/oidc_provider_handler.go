package handler

import (
	"errors"
	"fmt"
	"html"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type OIDCProviderHandler struct {
	service *service.OIDCProviderService
	cfg     *config.Config
}

func NewOIDCProviderHandler(svc *service.OIDCProviderService, cfg *config.Config) *OIDCProviderHandler {
	return &OIDCProviderHandler{service: svc, cfg: cfg}
}

func (h *OIDCProviderHandler) PublicHost() string {
	if h == nil || h.cfg == nil {
		return ""
	}
	return h.cfg.OIDCProvider.PublicHost
}

func (h *OIDCProviderHandler) Discovery(c *gin.Context) {
	if !h.providerReady(c) {
		return
	}
	c.Header("Cache-Control", fmt.Sprintf("public, max-age=%d", h.cfg.OIDCProvider.JWKSCacheMaxAgeSeconds))
	c.JSON(http.StatusOK, h.service.Discovery())
}

func (h *OIDCProviderHandler) Authorize(c *gin.Context) {
	if !h.providerReady(c) {
		return
	}
	values := c.Request.URL.Query()
	if hasDuplicateOIDCParameters(values, []string{"client_id", "redirect_uri", "response_type", "response_mode", "scope", "state", "nonce", "code_challenge", "code_challenge_method", "prompt", "max_age", "display", "request", "request_uri", "claims"}) || hasUnsupportedAuthorizationParameters(values) {
		h.localError(c, service.ErrOIDCInvalidRequest)
		return
	}
	maxAge, err := parseMaxAge(c.Query("max_age"))
	if err != nil {
		h.localError(c, service.ErrOIDCInvalidRequest)
		return
	}
	result, err := h.service.BeginAuthorization(c.Request.Context(), service.OIDCAuthorizeInput{
		ClientID: c.Query("client_id"), RedirectURI: c.Query("redirect_uri"), ResponseType: c.Query("response_type"), ResponseMode: c.Query("response_mode"),
		Scope: c.Query("scope"), State: c.Query("state"), Nonce: c.Query("nonce"), CodeChallenge: c.Query("code_challenge"), CodeChallengeMethod: c.Query("code_challenge_method"),
		Prompt: c.Query("prompt"), MaxAgeSeconds: maxAge, Display: c.Query("display"),
	}, h.readCookie(c, h.cfg.OIDCProvider.Cookie.SessionName))
	if err != nil {
		h.localError(c, err)
		return
	}
	if result.ErrorCode != "" {
		h.redirectError(c, result)
		return
	}
	h.setCookie(c, h.cfg.OIDCProvider.Cookie.TransactionName, result.TransactionHandle, true, 300)
	if result.NeedsLogin {
		h.renderLogin(c, result.TransactionHandle)
		return
	}
	if result.NeedsConsent {
		h.renderConsent(c, result.TransactionHandle)
		return
	}
	h.redirectSuccess(c, result)
}

func (h *OIDCProviderHandler) LoginPage(c *gin.Context) {
	if !h.providerReady(c) {
		return
	}
	tx := c.Query("tx")
	if tx == "" {
		tx = h.readCookie(c, h.cfg.OIDCProvider.Cookie.TransactionName)
	}
	if tx == "" {
		h.localError(c, service.ErrOIDCInvalidRequest)
		return
	}
	h.renderLogin(c, tx)
}

func (h *OIDCProviderHandler) LoginSubmit(c *gin.Context) {
	if !h.providerReady(c) {
		return
	}
	if !h.checkCSRF(c, c.PostForm("tx"), "login") {
		h.localError(c, service.ErrOIDCCSRFFailed)
		return
	}
	tx := strings.TrimSpace(c.PostForm("tx"))
	email := strings.TrimSpace(c.PostForm("email"))
	attemptKey := servermiddleware.SecurityClientIP(c) + "|" + strings.ToLower(email)
	currentSession := h.readCookie(c, h.cfg.OIDCProvider.Cookie.SessionName)
	session, err := h.service.AuthenticateTransactionWithSession(c.Request.Context(), tx, email, c.PostForm("password"), c.PostForm("totp_code"), currentSession, attemptKey)
	if err != nil {
		h.renderLoginError(c, tx, err)
		return
	}
	h.setCookie(c, h.cfg.OIDCProvider.Cookie.SessionName, session, true, h.cfg.OIDCProvider.BrowserSessionAbsoluteTTLSeconds)
	result, err := h.service.ContinueAuthorization(c.Request.Context(), tx, session)
	if err != nil {
		h.localError(c, err)
		return
	}
	if result.ErrorCode != "" {
		h.redirectError(c, result)
		return
	}
	if result.NeedsConsent {
		h.renderConsent(c, tx)
		return
	}
	h.redirectSuccess(c, result)
}

func (h *OIDCProviderHandler) ConsentPage(c *gin.Context) {
	if !h.providerReady(c) {
		return
	}
	tx := c.Query("tx")
	if tx == "" {
		tx = h.readCookie(c, h.cfg.OIDCProvider.Cookie.TransactionName)
	}
	if tx == "" {
		h.localError(c, service.ErrOIDCInvalidRequest)
		return
	}
	h.renderConsent(c, tx)
}

func (h *OIDCProviderHandler) ConsentSubmit(c *gin.Context) {
	if !h.providerReady(c) {
		return
	}
	tx := strings.TrimSpace(c.PostForm("tx"))
	if !h.checkCSRF(c, tx, "consent") {
		h.localError(c, service.ErrOIDCCSRFFailed)
		return
	}
	approve := c.PostForm("decision") == "approve"
	result, err := h.service.ApproveConsent(c.Request.Context(), tx, h.readCookie(c, h.cfg.OIDCProvider.Cookie.SessionName), approve)
	if err != nil {
		h.localError(c, err)
		return
	}
	if result.ErrorCode != "" {
		h.redirectError(c, result)
		return
	}
	h.redirectSuccess(c, result)
}

func (h *OIDCProviderHandler) Token(c *gin.Context) {
	if !h.providerReady(c) {
		return
	}
	h.noStore(c)
	if !isOIDCFormRequest(c) {
		h.tokenError(c, http.StatusBadRequest, "invalid_request")
		return
	}
	clientID, secret, ok := oidcBasicClientCredentials(c)
	if !ok || clientID == "" || secret == "" {
		h.tokenError(c, http.StatusUnauthorized, "invalid_client")
		return
	}
	if err := c.Request.ParseForm(); err != nil {
		h.tokenError(c, http.StatusBadRequest, "invalid_request")
		return
	}
	if hasOIDCFormParameter(c, "client_id") || hasOIDCFormParameter(c, "client_secret") || len(c.Request.PostForm["grant_type"]) != 1 || hasDuplicateOIDCParameters(c.Request.Form, []string{"grant_type", "code", "redirect_uri", "code_verifier", "refresh_token", "scope"}) {
		h.tokenError(c, http.StatusBadRequest, "invalid_request")
		return
	}
	var result *service.OIDCTokenResponse
	var err error
	switch c.PostForm("grant_type") {
	case service.OIDCGrantAuthorizationCode:
		result, err = h.service.TokenCode(c.Request.Context(), clientID, secret, c.PostForm("code"), c.PostForm("redirect_uri"), c.PostForm("code_verifier"))
	case service.OIDCGrantRefreshToken:
		result, err = h.service.TokenRefresh(c.Request.Context(), clientID, secret, c.PostForm("refresh_token"), c.PostForm("scope"))
	default:
		h.tokenError(c, http.StatusBadRequest, "unsupported_grant_type")
		return
	}
	if err != nil {
		h.tokenError(c, statusForTokenError(err), tokenErrorCode(err))
		return
	}
	body := gin.H{"access_token": result.AccessToken, "token_type": "Bearer", "expires_in": result.ExpiresIn, "scope": result.Scope}
	if result.RefreshToken != "" {
		body["refresh_token"] = result.RefreshToken
	}
	if result.IDToken != "" {
		body["id_token"] = result.IDToken
	}
	c.JSON(http.StatusOK, body)
}

func (h *OIDCProviderHandler) UserInfo(c *gin.Context) {
	if !h.providerReady(c) {
		return
	}
	h.noStore(c)
	token, ok := bearerToken(c.GetHeader("Authorization"))
	if !ok {
		c.Header("WWW-Authenticate", `Bearer error="invalid_token"`)
		c.Status(http.StatusUnauthorized)
		return
	}
	claims, err := h.service.UserInfo(c.Request.Context(), token)
	if err != nil {
		status := statusForUserInfoError(err)
		if status == http.StatusUnauthorized {
			c.Header("WWW-Authenticate", `Bearer error="invalid_token"`)
		}
		c.Status(status)
		return
	}
	c.JSON(http.StatusOK, claims)
}

func (h *OIDCProviderHandler) JWKS(c *gin.Context) {
	if !h.providerReady(c) {
		return
	}
	keys, err := h.service.SigningJWKS(c.Request.Context())
	if err != nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	c.Header("Cache-Control", fmt.Sprintf("public, max-age=%d", h.cfg.OIDCProvider.JWKSCacheMaxAgeSeconds))
	c.JSON(http.StatusOK, gin.H{"keys": keys})
}

func (h *OIDCProviderHandler) Revoke(c *gin.Context) {
	if h.service == nil || !h.service.ProviderEnabled() {
		h.noStore(c)
		c.Status(http.StatusNotFound)
		return
	}
	h.noStore(c)
	if !isOIDCFormRequest(c) {
		h.tokenError(c, http.StatusBadRequest, "invalid_request")
		return
	}
	clientID, secret, ok := oidcBasicClientCredentials(c)
	if !ok {
		h.tokenError(c, http.StatusUnauthorized, "invalid_client")
		return
	}
	if err := c.Request.ParseForm(); err != nil {
		h.tokenError(c, http.StatusBadRequest, "invalid_request")
		return
	}
	if hasOIDCFormParameter(c, "client_id") || hasOIDCFormParameter(c, "client_secret") || hasDuplicateOIDCParameters(c.Request.Form, []string{"token", "token_type_hint"}) {
		h.tokenError(c, http.StatusBadRequest, "invalid_request")
		return
	}
	token := strings.TrimSpace(c.PostForm("token"))
	if token == "" {
		h.tokenError(c, http.StatusBadRequest, "invalid_request")
		return
	}
	if err := h.service.Revoke(c.Request.Context(), clientID, secret, token); err != nil {
		h.tokenError(c, statusForTokenError(err), tokenErrorCode(err))
		return
	}
	c.Status(http.StatusOK)
}

func (h *OIDCProviderHandler) providerReady(c *gin.Context) bool {
	if h.service == nil || !h.service.ProviderEnabled() {
		h.noStore(c)
		c.Status(http.StatusNotFound)
		return false
	}
	if err := h.service.RequireActiveSigningKey(c.Request.Context()); err != nil {
		h.noStore(c)
		if errors.Is(err, service.ErrOIDCProviderDisabled) {
			c.Status(http.StatusNotFound)
		} else {
			c.Status(http.StatusServiceUnavailable)
		}
		return false
	}
	return true
}

func (h *OIDCProviderHandler) readCookie(c *gin.Context, name string) string {
	if c == nil {
		return ""
	}
	value, err := c.Cookie(name)
	if err != nil {
		return ""
	}
	return value
}
func (h *OIDCProviderHandler) setCookie(c *gin.Context, name, value string, httpOnly bool, maxAge int) {
	if h != nil && h.cfg != nil {
		if name == h.cfg.OIDCProvider.Cookie.SessionName || name == h.cfg.OIDCProvider.Cookie.TransactionName {
			httpOnly = h.cfg.OIDCProvider.Cookie.HTTPOnly
		}
	}
	c.SetSameSite(oidcCookieSameSite(h.cfg))
	c.SetCookie(name, value, maxAge, "/", "", h.cfg.OIDCProvider.Cookie.Secure, httpOnly)
}
func (h *OIDCProviderHandler) clearCookie(c *gin.Context, name string, httpOnly bool) {
	h.setCookie(c, name, "", httpOnly, -1)
}
func (h *OIDCProviderHandler) clearFlowCookies(c *gin.Context) {
	if h == nil || h.cfg == nil {
		return
	}
	h.clearCookie(c, h.cfg.OIDCProvider.Cookie.TransactionName, true)
	h.clearCookie(c, "__Host-sub2_oidc_csrf", false)
}
func (h *OIDCProviderHandler) noStore(c *gin.Context) {
	c.Header("Cache-Control", "no-store, no-cache, max-age=0")
	c.Header("Pragma", "no-cache")
	c.Header("Expires", "0")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Referrer-Policy", "no-referrer")
}
func (h *OIDCProviderHandler) checkCSRF(c *gin.Context, tx, action string) bool {
	supplied := c.PostForm("csrf")
	cookie := h.readCookie(c, "__Host-sub2_oidc_csrf")
	return h.service.ValidCSRF(tx, action, supplied, cookie)
}
func (h *OIDCProviderHandler) renderLogin(c *gin.Context, tx string) {
	token := h.service.CSRFToken(tx, "login")
	h.setCookie(c, "__Host-sub2_oidc_csrf", token, false, 300)
	h.noStore(c)
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(fmt.Sprintf(`<html><body><h1>Sign in</h1><form method="post" action="/oauth/login"><input type="hidden" name="tx" value="%s"><input type="hidden" name="csrf" value="%s"><label>Email <input name="email" type="email" autocomplete="username"></label><label>Password <input name="password" type="password" autocomplete="current-password"></label><label>TOTP <input name="totp_code" inputmode="numeric" autocomplete="one-time-code"></label><button type="submit">Continue</button></form></body></html>`, html.EscapeString(tx), html.EscapeString(token))))
}
func (h *OIDCProviderHandler) renderLoginError(c *gin.Context, tx string, err error) {
	c.Header("X-OIDC-Login-Error", tokenErrorCode(err))
	h.renderLogin(c, tx)
}
func (h *OIDCProviderHandler) renderConsent(c *gin.Context, tx string) {
	token := h.service.CSRFToken(tx, "consent")
	h.setCookie(c, "__Host-sub2_oidc_csrf", token, false, 300)
	h.noStore(c)
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(fmt.Sprintf(`<html><body><h1>Authorize</h1><p>Approve the requested first-party scopes?</p><form method="post" action="/oauth/consent"><input type="hidden" name="tx" value="%s"><input type="hidden" name="csrf" value="%s"><button name="decision" value="approve">Approve</button><button name="decision" value="deny">Deny</button></form></body></html>`, html.EscapeString(tx), html.EscapeString(token))))
}
func (h *OIDCProviderHandler) redirectSuccess(c *gin.Context, result *service.OIDCAuthorizeResult) {
	h.noStore(c)
	h.clearFlowCookies(c)
	u, err := url.Parse(result.RedirectURI)
	if err != nil {
		h.localError(c, err)
		return
	}
	q := u.Query()
	q.Set("code", result.Code)
	q.Set("state", result.State)
	q.Set("iss", result.Issuer)
	u.RawQuery = q.Encode()
	c.Redirect(http.StatusFound, u.String())
}
func (h *OIDCProviderHandler) redirectError(c *gin.Context, result *service.OIDCAuthorizeResult) {
	h.noStore(c)
	h.clearFlowCookies(c)
	u, err := url.Parse(result.RedirectURI)
	if err != nil {
		h.localError(c, err)
		return
	}
	q := u.Query()
	q.Set("error", result.ErrorCode)
	if result.ErrorDescription != "" {
		q.Set("error_description", result.ErrorDescription)
	}
	if result.State != "" {
		q.Set("state", result.State)
	}
	q.Set("iss", result.Issuer)
	u.RawQuery = q.Encode()
	c.Redirect(http.StatusFound, u.String())
}
func (h *OIDCProviderHandler) localError(c *gin.Context, err error) {
	h.noStore(c)
	c.JSON(statusForLocalError(err), gin.H{"error": tokenErrorCode(err)})
}
func (h *OIDCProviderHandler) tokenError(c *gin.Context, status int, code string) {
	h.noStore(c)
	if status == http.StatusUnauthorized {
		c.Header("WWW-Authenticate", `Basic realm="oidc"`)
	}
	c.JSON(status, gin.H{"error": code})
}
func hasDuplicateOIDCParameters(values url.Values, names []string) bool {
	for _, name := range names {
		if len(values[name]) > 1 {
			return true
		}
	}
	return false
}

func hasUnsupportedAuthorizationParameters(values url.Values) bool {
	for _, name := range []string{"request", "request_uri", "claims"} {
		if _, ok := values[name]; ok {
			return true
		}
	}
	return false
}

func parseMaxAge(raw string) (*int64, error) {
	if raw == "" {
		return nil, nil
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v < 0 {
		return nil, service.ErrOIDCInvalidRequest
	}
	return &v, nil
}
func bearerToken(raw string) (string, bool) {
	parts := strings.Fields(raw)
	return func() (string, bool) {
		if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") || parts[1] == "" {
			return "", false
		}
		return parts[1], true
	}()
}
func statusForTokenError(err error) int {
	if errors.Is(err, service.ErrOIDCInvalidClient) {
		return http.StatusUnauthorized
	}
	if errors.Is(err, service.ErrOIDCProviderDisabled) || errors.Is(err, service.ErrOIDCKeyUnavailable) || errors.Is(err, service.ErrOIDCTemporarilyUnavailable) {
		return http.StatusServiceUnavailable
	}
	if errors.Is(err, service.ErrOIDCInvalidGrant) || errors.Is(err, service.ErrOIDCInvalidScope) || errors.Is(err, service.ErrOIDCUserInactive) || errors.Is(err, service.ErrOIDCReplayDetected) {
		return http.StatusBadRequest
	}
	if errors.Is(err, service.ErrOIDCServerError) {
		return http.StatusInternalServerError
	}
	return http.StatusInternalServerError
}

func statusForUserInfoError(err error) int {
	if errors.Is(err, service.ErrOIDCInvalidGrant) || errors.Is(err, service.ErrOIDCUserInactive) || errors.Is(err, service.ErrOIDCInvalidClient) {
		return http.StatusUnauthorized
	}
	if errors.Is(err, service.ErrOIDCProviderDisabled) {
		return http.StatusNotFound
	}
	return http.StatusInternalServerError
}

func statusForLocalError(err error) int {
	switch {
	case errors.Is(err, service.ErrOIDCProviderDisabled):
		return http.StatusNotFound
	case errors.Is(err, service.ErrOIDCKeyUnavailable), errors.Is(err, service.ErrOIDCTemporarilyUnavailable):
		return http.StatusServiceUnavailable
	case errors.Is(err, service.ErrOIDCServerError):
		return http.StatusInternalServerError
	case errors.Is(err, service.ErrOIDCInvalidClient), errors.Is(err, service.ErrOIDCInvalidGrant), errors.Is(err, service.ErrOIDCInvalidRequest), errors.Is(err, service.ErrOIDCInvalidScope), errors.Is(err, service.ErrOIDCUnauthorized), errors.Is(err, service.ErrOIDCUnsupportedResponseType), errors.Is(err, service.ErrOIDCInteractionRequired), errors.Is(err, service.ErrOIDCAccountSelectionRequired), errors.Is(err, service.ErrOIDCLoginRequired), errors.Is(err, service.ErrOIDCConsentRequired), errors.Is(err, service.ErrOIDCAccessDenied), errors.Is(err, service.ErrOIDCCSRFFailed):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

func tokenErrorCode(err error) string {
	switch {
	case errors.Is(err, service.ErrOIDCInvalidClient):
		return "invalid_client"
	case errors.Is(err, service.ErrOIDCInvalidGrant), errors.Is(err, service.ErrOIDCReplayDetected), errors.Is(err, service.ErrOIDCUserInactive):
		return "invalid_grant"
	case errors.Is(err, service.ErrOIDCInvalidScope):
		return "invalid_scope"
	case errors.Is(err, service.ErrOIDCUnauthorized):
		return "unauthorized_client"
	case errors.Is(err, service.ErrOIDCAccessDenied):
		return "access_denied"
	case errors.Is(err, service.ErrOIDCUnsupportedResponseType):
		return "unsupported_response_type"
	case errors.Is(err, service.ErrOIDCInteractionRequired):
		return "interaction_required"
	case errors.Is(err, service.ErrOIDCAccountSelectionRequired):
		return "account_selection_required"
	case errors.Is(err, service.ErrOIDCServerError):
		return "server_error"
	case errors.Is(err, service.ErrOIDCTemporarilyUnavailable):
		return "temporarily_unavailable"
	case errors.Is(err, service.ErrOIDCLoginRequired):
		return "login_required"
	case errors.Is(err, service.ErrOIDCConsentRequired):
		return "consent_required"
	case errors.Is(err, service.ErrOIDCProviderDisabled), errors.Is(err, service.ErrOIDCKeyUnavailable):
		return "temporarily_unavailable"
	default:
		return "server_error"
	}
}

func isOIDCFormRequest(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	return err == nil && strings.EqualFold(mediaType, "application/x-www-form-urlencoded")
}

func oidcBasicClientCredentials(c *gin.Context) (string, string, bool) {
	if c == nil || c.Request == nil || len(c.Request.Header.Values("Authorization")) != 1 {
		return "", "", false
	}
	clientID, secret, ok := c.Request.BasicAuth()
	if !ok || len(clientID) == 0 || len(clientID) > 128 || len(secret) == 0 || len(secret) > 512 {
		return "", "", false
	}
	return clientID, secret, true
}

func hasOIDCFormParameter(c *gin.Context, name string) bool {
	if c == nil || c.Request == nil {
		return false
	}
	_, ok := c.Request.Form[name]
	return ok
}

func oidcCookieSameSite(cfg *config.Config) http.SameSite {
	if cfg == nil {
		return http.SameSiteLaxMode
	}
	switch strings.ToLower(strings.TrimSpace(cfg.OIDCProvider.Cookie.SameSite)) {
	case "strict":
		return http.SameSiteStrictMode
	case "none":
		return http.SameSiteNoneMode
	default:
		return http.SameSiteLaxMode
	}
}
