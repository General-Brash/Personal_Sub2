package admin

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
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

type oidcProviderStatusResponse struct {
	Enabled         bool                         `json:"enabled"`
	Status          string                       `json:"status"`
	Issuer          string                       `json:"issuer"`
	Endpoints       oidcProviderEndpointResponse `json:"endpoints"`
	ActiveKey       *oidcSigningKeyResponse      `json:"active_key,omitempty"`
	RetiringKeys    []oidcSigningKeyResponse     `json:"retiring_keys,omitempty"`
	SupportedScopes []string                     `json:"supported_scopes"`
	SupportedClaims []string                     `json:"supported_claims"`
	Security        oidcProviderSecurityResponse `json:"security"`
	Warning         string                       `json:"warning,omitempty"`
}

type oidcProviderEndpointResponse struct {
	Discovery     string `json:"discovery"`
	Authorization string `json:"authorization"`
	Token         string `json:"token"`
	UserInfo      string `json:"userinfo"`
	JWKS          string `json:"jwks"`
	Revocation    string `json:"revocation"`
}

type oidcProviderSecurityResponse struct {
	DefaultDisabled      bool   `json:"default_disabled"`
	SecretOneTimeDisplay bool   `json:"secret_one_time_display"`
	PrivateKeyHidden     bool   `json:"private_key_hidden"`
	CSRFRequired         bool   `json:"csrf_required"`
	StepUpRequired       bool   `json:"step_up_required"`
	AdminSession         string `json:"admin_session"`
}

type oidcSigningKeyResponse struct {
	KID         string     `json:"kid"`
	Alg         string     `json:"alg"`
	Fingerprint string     `json:"fingerprint"`
	Status      string     `json:"status"`
	NotBefore   time.Time  `json:"not_before"`
	NotAfter    time.Time  `json:"not_after"`
	CreatedAt   time.Time  `json:"created_at"`
	ActivatedAt *time.Time `json:"activated_at,omitempty"`
	RetiredAt   *time.Time `json:"retired_at,omitempty"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
}

type oidcClientSecretResponse struct {
	ID          int64      `json:"id"`
	Fingerprint string     `json:"fingerprint"`
	Status      string     `json:"status"`
	NotBefore   time.Time  `json:"not_before"`
	ExpiresAt   time.Time  `json:"expires_at"`
	CreatedAt   time.Time  `json:"created_at"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
}

type oidcClientResponse struct {
	ID                 int64                      `json:"id"`
	ClientID           string                     `json:"client_id"`
	Name               string                     `json:"name"`
	Owner              string                     `json:"owner"`
	ClientType         string                     `json:"client_type"`
	Enabled            bool                       `json:"enabled"`
	TrustedSkipConsent bool                       `json:"trusted_skip_consent"`
	RedirectURIs       []string                   `json:"redirect_uris"`
	AllowedScopes      []string                   `json:"allowed_scopes"`
	Secrets            []oidcClientSecretResponse `json:"secrets,omitempty"`
	CreatedAt          time.Time                  `json:"created_at"`
	UpdatedAt          time.Time                  `json:"updated_at"`
	DisabledAt         *time.Time                 `json:"disabled_at,omitempty"`
	DisabledReason     string                     `json:"disabled_reason,omitempty"`
	PolicyVersion      int64                      `json:"policy_version"`
	Version            int64                      `json:"version"`
}

type oidcClientCreateResponse struct {
	Client       oidcClientResponse            `json:"client"`
	ClientSecret string                        `json:"client_secret"`
	Secret       oidcClientSecretIssueMetadata `json:"secret"`
}

type oidcClientSecretIssueResponse struct {
	ClientSecret string                        `json:"client_secret"`
	Secret       oidcClientSecretIssueMetadata `json:"secret"`
}

type oidcClientSecretIssueMetadata struct {
	ID          int64     `json:"id"`
	Fingerprint string    `json:"fingerprint"`
	NotBefore   time.Time `json:"not_before"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type oidcConsentResponse struct {
	ID            int64      `json:"id"`
	UserID        int64      `json:"user_id"`
	ClientID      string     `json:"client_id"`
	ClientName    string     `json:"client_name,omitempty"`
	Scopes        []string   `json:"scopes"`
	Source        string     `json:"source"`
	Status        string     `json:"status"`
	PolicyVersion int64      `json:"policy_version"`
	ApprovedAt    *time.Time `json:"approved_at,omitempty"`
	RevokedAt     *time.Time `json:"revoked_at,omitempty"`
	RevokedReason string     `json:"revoked_reason,omitempty"`
}

type oidcAuditEventResponse struct {
	ID                int64     `json:"id"`
	CreatedAt         time.Time `json:"created_at"`
	Action            string    `json:"action"`
	Result            string    `json:"result"`
	ActorUserID       *int64    `json:"actor_user_id,omitempty"`
	ClientID          *string   `json:"client_id,omitempty"`
	SecretID          *string   `json:"secret_id,omitempty"`
	SecretFingerprint *string   `json:"secret_fingerprint,omitempty"`
	KID               *string   `json:"kid,omitempty"`
	UserID            *int64    `json:"user_id,omitempty"`
	FamilyID          *string   `json:"family_id,omitempty"`
	Scope             []string  `json:"scope,omitempty"`
	OldStatus         *string   `json:"old_status,omitempty"`
	NewStatus         *string   `json:"new_status,omitempty"`
	Reason            *string   `json:"reason,omitempty"`
	RequestID         *string   `json:"request_id,omitempty"`
}

type oidcAdminActionRequest struct {
	Reason    string `json:"reason"`
	RequestID string `json:"request_id"`
}

type oidcClientCreateRequest struct {
	Name               string   `json:"name"`
	Owner              string   `json:"owner"`
	RedirectURIs       []string `json:"redirect_uris"`
	AllowedScopes      []string `json:"allowed_scopes"`
	TrustedSkipConsent bool     `json:"trusted_skip_consent"`
	Reason             string   `json:"reason"`
	RequestID          string   `json:"request_id"`
}

type oidcClientUpdateRequest struct {
	Name               string   `json:"name"`
	Owner              string   `json:"owner"`
	RedirectURIs       []string `json:"redirect_uris"`
	AllowedScopes      []string `json:"allowed_scopes"`
	TrustedSkipConsent bool     `json:"trusted_skip_consent"`
	Reason             string   `json:"reason"`
	RequestID          string   `json:"request_id"`
}

func (h *OIDCProviderHandler) AdminCSRFToken(c *gin.Context) string {
	if h == nil || h.cfg == nil || c == nil || c.GetString("auth_method") != service.AuditAuthMethodJWT {
		return ""
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		return ""
	}
	sessionID := c.GetString(string(middleware.ContextKeySessionID))
	tokenVersion, ok := middleware.GetAuthTokenVersionFromContext(c)
	if !ok || sessionID == "" {
		return ""
	}
	return service.OIDCAdminCSRFToken(h.cfg.OIDCProvider.SecretPepper, subject.UserID, sessionID, tokenVersion)
}

func (h *OIDCProviderHandler) Status(c *gin.Context) {
	// The synchronizer token is deliberately non-HttpOnly so the authorized
	// same-origin admin client can echo it in X-CSRF-Token. It is issued only
	// when the current request has a bound JWT administrator session.
	if token := h.AdminCSRFToken(c); token != "" {
		http.SetCookie(c.Writer, &http.Cookie{
			Name:     "__Host-sub2_oidc_admin_csrf",
			Value:    token,
			MaxAge:   300,
			Path:     "/",
			Secure:   true,
			HttpOnly: false,
			SameSite: http.SameSiteLaxMode,
		})
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	response := oidcProviderStatusResponse{
		Enabled: h.service.ProviderEnabled(),
		Status:  "disabled",
		Issuer:  service.OIDCProviderIssuer,
		Endpoints: oidcProviderEndpointResponse{
			Discovery:     service.OIDCProviderIssuer + "/.well-known/openid-configuration",
			Authorization: service.OIDCProviderIssuer + "/oauth/authorize",
			Token:         service.OIDCProviderIssuer + "/oauth/token",
			UserInfo:      service.OIDCProviderIssuer + "/oauth/userinfo",
			JWKS:          service.OIDCProviderIssuer + "/oauth/jwks",
			Revocation:    service.OIDCProviderIssuer + "/oauth/revoke",
		},
		SupportedScopes: append([]string{}, h.cfg.OIDCProvider.AllowedScopes...),
		SupportedClaims: []string{},
		Security: oidcProviderSecurityResponse{
			DefaultDisabled:      true,
			SecretOneTimeDisplay: true,
			PrivateKeyHidden:     true,
			CSRFRequired:         true,
			StepUpRequired:       true,
			AdminSession:         "bearer_compatibility",
		},
	}
	metadata := h.service.Discovery()
	if claims, ok := metadata["claims_supported"].([]string); ok {
		response.SupportedClaims = append([]string(nil), claims...)
	}
	if response.Enabled {
		response.Status = "enabled"
		keys, err := h.service.AdminKeys(c.Request.Context())
		if err != nil {
			response.Status = "degraded"
			response.Warning = "provider key metadata is unavailable"
		} else {
			for _, key := range keys {
				dto := oidcSigningKeyDTO(key)
				switch key.Status {
				case "active":
					response.ActiveKey = &dto
				case "retiring":
					response.RetiringKeys = append(response.RetiringKeys, dto)
				}
			}
			if response.ActiveKey == nil {
				response.Status = "degraded"
				response.Warning = "provider has no active signing key"
			}
		}
	}
	c.JSON(http.StatusOK, response)
}

func (h *OIDCProviderHandler) ListClients(c *gin.Context) {
	clients, err := h.service.AdminListClients(c.Request.Context())
	if err != nil {
		h.adminError(c, err)
		return
	}
	items := make([]oidcClientResponse, 0, len(clients))
	for _, client := range clients {
		items = append(items, oidcClientDTO(client))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h *OIDCProviderHandler) CreateClient(c *gin.Context) {
	var req oidcClientCreateRequest
	if !h.bindJSON(c, &req) {
		return
	}
	h.setMutationAudit(c, req.Reason, req.RequestID)
	client, secret, err := h.service.AdminCreateClient(c.Request.Context(), service.OIDCClientCreateInput{Name: req.Name, Owner: req.Owner, RedirectURIs: req.RedirectURIs, AllowedScopes: req.AllowedScopes, TrustedSkipConsent: req.TrustedSkipConsent, Reason: req.Reason, ActorID: adminActor(c)})
	if err != nil {
		h.adminError(c, err)
		return
	}
	secretRecord := latestSecret(client)
	response := oidcClientCreateResponse{Client: oidcClientDTO(*client), ClientSecret: secret}
	if secretRecord != nil {
		response.Secret = oidcClientSecretIssueMetadata{ID: secretRecord.ID, Fingerprint: secretRecord.Fingerprint, NotBefore: secretRecord.NotBefore, ExpiresAt: secretRecord.ExpiresAt}
	}
	c.JSON(http.StatusCreated, response)
}

func (h *OIDCProviderHandler) UpdateClient(c *gin.Context) {
	id, err := parseOIDCID(c.Param("id"))
	if err != nil {
		h.adminError(c, err)
		return
	}
	version, err := parseIfMatch(c.GetHeader("If-Match"))
	if err != nil {
		c.JSON(http.StatusPreconditionFailed, gin.H{"error": "version_required"})
		return
	}
	var req oidcClientUpdateRequest
	if !h.bindJSON(c, &req) {
		return
	}
	h.setMutationAudit(c, req.Reason, req.RequestID)
	client, err := h.service.AdminUpdateClient(c.Request.Context(), service.OIDCClientUpdateInput{ClientPK: id, ExpectedVersion: version, Name: req.Name, Owner: req.Owner, RedirectURIs: req.RedirectURIs, AllowedScopes: req.AllowedScopes, TrustedSkipConsent: req.TrustedSkipConsent, Reason: req.Reason, ActorID: adminActor(c)})
	if err != nil {
		h.adminError(c, err)
		return
	}
	c.JSON(http.StatusOK, oidcClientDTO(*client))
}

func (h *OIDCProviderHandler) GetClient(c *gin.Context) {
	id, err := parseOIDCID(c.Param("id"))
	if err != nil {
		h.adminError(c, err)
		return
	}
	client, err := h.service.AdminGetClient(c.Request.Context(), id)
	if err != nil {
		h.adminError(c, err)
		return
	}
	c.JSON(http.StatusOK, oidcClientDTO(*client))
}

func (h *OIDCProviderHandler) RotateSecret(c *gin.Context) {
	id, err := parseOIDCID(c.Param("id"))
	if err != nil {
		h.adminError(c, err)
		return
	}
	var req oidcAdminActionRequest
	if !h.bindJSON(c, &req) {
		return
	}
	h.setMutationAudit(c, req.Reason, req.RequestID)
	record, secret, err := h.service.AdminRotateSecret(c.Request.Context(), id, adminActor(c), req.Reason)
	if err != nil {
		h.adminError(c, err)
		return
	}
	c.JSON(http.StatusCreated, oidcClientSecretIssueResponse{ClientSecret: secret, Secret: oidcClientSecretIssueMetadata{ID: record.ID, Fingerprint: record.Fingerprint, NotBefore: record.NotBefore, ExpiresAt: record.ExpiresAt}})
}

func (h *OIDCProviderHandler) RevokeSecret(c *gin.Context) {
	clientID, err := parseOIDCID(c.Param("id"))
	secretID, err2 := parseOIDCID(c.Param("secretID"))
	if err != nil || err2 != nil {
		h.adminError(c, sql.ErrNoRows)
		return
	}
	var req oidcAdminActionRequest
	if !h.bindJSON(c, &req) {
		return
	}
	h.setMutationAudit(c, req.Reason, req.RequestID)
	if err := h.service.AdminRevokeSecret(c.Request.Context(), clientID, secretID, adminActor(c), req.Reason); err != nil {
		h.adminError(c, err)
		return
	}
	client, err := h.service.AdminGetClient(c.Request.Context(), clientID)
	if err != nil {
		h.adminError(c, err)
		return
	}
	c.JSON(http.StatusOK, oidcClientDTO(*client))
}

func (h *OIDCProviderHandler) SetClientEnabled(c *gin.Context) {
	id, err := parseOIDCID(c.Param("id"))
	if err != nil {
		h.adminError(c, err)
		return
	}
	var req oidcAdminActionRequest
	if !h.bindJSON(c, &req) {
		return
	}
	h.setMutationAudit(c, req.Reason, req.RequestID)
	enabled := strings.HasSuffix(c.FullPath(), "/enable")
	client, err := h.service.AdminEnableClient(c.Request.Context(), id, adminActor(c), enabled, req.Reason)
	if err != nil {
		h.adminError(c, err)
		return
	}
	c.JSON(http.StatusOK, oidcClientDTO(*client))
}

func (h *OIDCProviderHandler) ListKeys(c *gin.Context) {
	keys, err := h.service.AdminKeys(c.Request.Context())
	if err != nil {
		h.adminError(c, err)
		return
	}
	items := make([]oidcSigningKeyResponse, 0, len(keys))
	for _, key := range keys {
		items = append(items, oidcSigningKeyDTO(key))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h *OIDCProviderHandler) RotateKey(c *gin.Context) {
	var req oidcAdminActionRequest
	if !h.bindJSON(c, &req) {
		return
	}
	h.setMutationAudit(c, req.Reason, req.RequestID)
	key, err := h.service.AdminRotateKey(c.Request.Context(), adminActor(c), req.Reason)
	if err != nil {
		h.adminError(c, err)
		return
	}
	c.JSON(http.StatusCreated, oidcSigningKeyDTO(*key))
}

func (h *OIDCProviderHandler) SetKeyStatus(c *gin.Context) {
	var req oidcAdminActionRequest
	if !h.bindJSON(c, &req) {
		return
	}
	h.setMutationAudit(c, req.Reason, req.RequestID)
	status := "retired"
	if strings.HasSuffix(c.FullPath(), "/revoke") {
		status = "revoked"
	}
	if err := h.service.AdminSetKeyStatus(c.Request.Context(), c.Param("kid"), status, adminActor(c), req.Reason); err != nil {
		h.adminError(c, err)
		return
	}
	keys, err := h.service.AdminKeys(c.Request.Context())
	if err != nil {
		h.adminError(c, err)
		return
	}
	for _, key := range keys {
		if key.KID == c.Param("kid") {
			c.JSON(http.StatusOK, oidcSigningKeyDTO(key))
			return
		}
	}
	h.adminError(c, sql.ErrNoRows)
}

func (h *OIDCProviderHandler) ListConsents(c *gin.Context) {
	page, pageSize := parsePage(c)
	items, err := h.service.AdminListConsents(c.Request.Context(), service.OIDCConsentListInput{Page: page, PageSize: pageSize, ClientID: c.Query("client_id"), Status: c.Query("status"), Query: c.Query("q")})
	if err != nil {
		h.adminError(c, err)
		return
	}
	response := make([]oidcConsentResponse, 0, len(items))
	for _, item := range items {
		response = append(response, oidcConsentDTO(item))
	}
	c.JSON(http.StatusOK, gin.H{"items": response})
}

func (h *OIDCProviderHandler) RevokeConsent(c *gin.Context) {
	id, err := parseOIDCID(c.Param("id"))
	if err != nil {
		h.adminError(c, err)
		return
	}
	var req oidcAdminActionRequest
	if !h.bindJSON(c, &req) {
		return
	}
	h.setMutationAudit(c, req.Reason, req.RequestID)
	item, err := h.service.AdminRevokeConsent(c.Request.Context(), id, adminActor(c), req.Reason)
	if err != nil {
		h.adminError(c, err)
		return
	}
	c.JSON(http.StatusOK, oidcConsentDTO(*item))
}

func (h *OIDCProviderHandler) ListAuditEvents(c *gin.Context) {
	page, pageSize := parsePage(c)
	items, err := h.service.AdminListAuditEvents(c.Request.Context(), service.OIDCAuditListInput{Page: page, PageSize: pageSize, Query: c.Query("q")})
	if err != nil {
		h.adminError(c, err)
		return
	}
	response := make([]oidcAuditEventResponse, 0, len(items))
	for _, item := range items {
		response = append(response, oidcAuditEventDTO(item))
	}
	c.JSON(http.StatusOK, gin.H{"items": response})
}

func (h *OIDCProviderHandler) bindJSON(c *gin.Context, target any) bool {
	if err := c.ShouldBindJSON(target); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return false
	}
	return true
}

func (h *OIDCProviderHandler) setMutationAudit(c *gin.Context, reason, requestID string) {
	middleware.SetAuditExtra(c, map[string]any{"reason": strings.TrimSpace(reason), "request_id": strings.TrimSpace(requestID)})
}

func (h *OIDCProviderHandler) adminError(c *gin.Context, err error) {
	status := http.StatusBadRequest
	code := "invalid_request"
	switch {
	case errors.Is(err, service.ErrOIDCProviderDisabled):
		status, code = http.StatusServiceUnavailable, "oidc_provider_disabled"
	case errors.Is(err, service.ErrOIDCVersionConflict):
		status, code = http.StatusPreconditionFailed, "version_conflict"
	case errors.Is(err, service.ErrOIDCKeyStateConflict):
		status, code = http.StatusConflict, "key_state_conflict"
	case errors.Is(err, sql.ErrNoRows):
		status, code = http.StatusNotFound, "not_found"
	}
	c.JSON(status, gin.H{"error": code})
}

func parseOIDCID(raw string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || id <= 0 {
		return 0, sql.ErrNoRows
	}
	return id, nil
}

func parseIfMatch(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "W/")
	raw = strings.Trim(raw, "\"")
	version, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || version <= 0 {
		return 0, errors.New("invalid if-match")
	}
	return version, nil
}

func parsePage(c *gin.Context) (int, int) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 50
	}
	return page, pageSize
}

func oidcClientDTO(client service.OIDCClientRecord) oidcClientResponse {
	return oidcClientResponse{ID: client.ID, ClientID: client.ClientID, Name: client.Name, Owner: client.Owner, ClientType: client.ClientType, Enabled: client.Enabled, TrustedSkipConsent: client.TrustedSkipConsent, RedirectURIs: append([]string(nil), client.RedirectURIs...), AllowedScopes: append([]string(nil), client.AllowedScopes...), Secrets: oidcSecretDTOs(client.Secrets), CreatedAt: client.CreatedAt, UpdatedAt: client.UpdatedAt, DisabledAt: client.DisabledAt, DisabledReason: client.DisabledReason, PolicyVersion: client.PolicyVersion, Version: client.Version}
}

func oidcSecretDTOs(secrets []service.OIDCClientSecretRecord) []oidcClientSecretResponse {
	items := make([]oidcClientSecretResponse, 0, len(secrets))
	for _, secret := range secrets {
		items = append(items, oidcClientSecretResponse{ID: secret.ID, Fingerprint: secret.Fingerprint, Status: secret.Status, NotBefore: secret.NotBefore, ExpiresAt: secret.ExpiresAt, CreatedAt: secret.CreatedAt, RevokedAt: secret.RevokedAt})
	}
	return items
}

func latestSecret(client *service.OIDCClientRecord) *service.OIDCClientSecretRecord {
	if client == nil || len(client.Secrets) == 0 {
		return nil
	}
	return &client.Secrets[0]
}

func oidcSigningKeyDTO(key service.OIDCSigningKeyRecord) oidcSigningKeyResponse {
	return oidcSigningKeyResponse{KID: key.KID, Alg: key.Alg, Fingerprint: key.Fingerprint, Status: key.Status, NotBefore: key.NotBefore, NotAfter: key.NotAfter, CreatedAt: key.CreatedAt, ActivatedAt: key.ActivatedAt, RetiredAt: key.RetiredAt, RevokedAt: key.RevokedAt}
}

func oidcConsentDTO(item service.OIDCConsentRecord) oidcConsentResponse {
	return oidcConsentResponse{ID: item.ID, UserID: item.UserID, ClientID: item.ClientID, ClientName: item.ClientName, Scopes: append([]string(nil), item.Scopes...), Source: item.Source, Status: item.Status, PolicyVersion: item.PolicyVersion, ApprovedAt: item.ApprovedAt, RevokedAt: item.RevokedAt, RevokedReason: item.RevokedReason}
}

func oidcAuditEventDTO(item service.OIDCAuditEventRecord) oidcAuditEventResponse {
	return oidcAuditEventResponse{ID: item.ID, CreatedAt: item.CreatedAt, Action: item.Action, Result: item.Result, ActorUserID: item.ActorUserID, ClientID: item.ClientID, SecretID: item.SecretID, SecretFingerprint: item.SecretFingerprint, KID: item.KID, UserID: item.UserID, FamilyID: item.FamilyID, Scope: append([]string(nil), item.Scope...), OldStatus: item.OldStatus, NewStatus: item.NewStatus, Reason: item.Reason, RequestID: item.RequestID}
}

func adminActor(c *gin.Context) int64 {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		return 0
	}
	return subject.UserID
}
