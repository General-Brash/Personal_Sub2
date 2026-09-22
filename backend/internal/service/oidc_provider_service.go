package service

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// oidcSSOCodeTTL 是跨域 SSO 一次性凭证的有效期（含一次性消费标记的过期）。
const oidcSSOCodeTTL = 60 * time.Second

type OIDCProviderService struct {
	repo         OIDCProviderRepository
	users        UserRepository
	totp         *TotpService
	cfg          *config.Config
	protector    *oidcProtector
	signing      *OIDCSigningService
	loginGuard   *oidcLoginAttemptGuard
	ssoCodeCache OIDCSSOCodeCache
}

func NewOIDCProviderService(repo OIDCProviderRepository, users UserRepository, totp *TotpService, signing *OIDCSigningService, cfg *config.Config, ssoCodeCache OIDCSSOCodeCache) *OIDCProviderService {
	var protector *oidcProtector
	if cfg != nil && cfg.OIDCProvider.EncryptionKey != "" {
		protector, _ = newOIDCProtector(cfg)
	}
	return &OIDCProviderService{repo: repo, users: users, totp: totp, signing: signing, cfg: cfg, protector: protector, loginGuard: newOIDCLoginAttemptGuard(), ssoCodeCache: ssoCodeCache}
}

func (s *OIDCProviderService) enabled() error {
	if s == nil || s.cfg == nil || !s.cfg.OIDCProvider.Enabled {
		return ErrOIDCProviderDisabled
	}
	if s.repo == nil || s.users == nil {
		return ErrOIDCServerError
	}
	if s.protector == nil {
		return ErrOIDCKeyUnavailable
	}
	return nil
}

func (s *OIDCProviderService) ProviderEnabled() bool {
	return s != nil && s.cfg != nil && s.cfg.OIDCProvider.Enabled
}

// RequireActiveSigningKey is the protocol readiness gate. The Provider may
// remain administratively enabled so an operator can create or rotate the
// first key, but public protocol operations must fail closed until the active
// key is present, valid, and decryptable.
func (s *OIDCProviderService) RequireActiveSigningKey(ctx context.Context) error {
	if s == nil || s.cfg == nil || !s.cfg.OIDCProvider.Enabled {
		return ErrOIDCProviderDisabled
	}
	if s.repo == nil || s.users == nil {
		return ErrOIDCServerError
	}
	if s.signing == nil || s.protector == nil {
		return ErrOIDCKeyUnavailable
	}
	if _, err := s.signing.loadActive(ctx); err != nil {
		return ErrOIDCKeyUnavailable
	}
	return nil
}

func (s *OIDCProviderService) SigningJWKS(ctx context.Context) ([]map[string]any, error) {
	if err := s.RequireActiveSigningKey(ctx); err != nil {
		return nil, err
	}
	return s.signing.PublicJWKS(ctx)
}

func (s *OIDCProviderService) Discovery() map[string]any {
	scopes := []string{OIDCScopeOpenID, OIDCScopeProfile, OIDCScopeEmail, OIDCScopeRoles, OIDCScopeOfflineAccess}
	if s != nil && s.cfg != nil && len(s.cfg.OIDCProvider.AllowedScopes) > 0 {
		scopes = append([]string(nil), s.cfg.OIDCProvider.AllowedScopes...)
		sort.Strings(scopes)
	}
	return map[string]any{
		"issuer":                                         OIDCProviderIssuer,
		"authorization_endpoint":                         OIDCProviderIssuer + "/oauth/authorize",
		"token_endpoint":                                 OIDCProviderIssuer + "/oauth/token",
		"userinfo_endpoint":                              OIDCProviderIssuer + "/oauth/userinfo",
		"jwks_uri":                                       OIDCProviderIssuer + "/oauth/jwks",
		"revocation_endpoint":                            OIDCProviderIssuer + "/oauth/revoke",
		"response_types_supported":                       []string{"code"},
		"response_modes_supported":                       []string{"query"},
		"grant_types_supported":                          []string{OIDCGrantAuthorizationCode, OIDCGrantRefreshToken},
		"subject_types_supported":                        []string{"public"},
		"id_token_signing_alg_values_supported":          []string{OIDCSigningRS256},
		"code_challenge_methods_supported":               []string{OIDCCodeChallengeS256},
		"scopes_supported":                               scopes,
		"claims_supported":                               s.supportedClaims(scopes),
		"token_endpoint_auth_methods_supported":          []string{OIDCClientAuthBasic},
		"revocation_endpoint_auth_methods_supported":     []string{OIDCClientAuthBasic},
		"display_values_supported":                       []string{"page", "popup", "touch", "wap"},
		"authorization_response_iss_parameter_supported": true,
		"request_parameter_supported":                    false,
		"request_uri_parameter_supported":                false,
		"claims_parameter_supported":                     false,
	}
}

type OIDCAuthorizeInput struct {
	ClientID            string
	RedirectURI         string
	ResponseType        string
	ResponseMode        string
	Scope               string
	State               string
	Nonce               string
	CodeChallenge       string
	CodeChallengeMethod string
	Prompt              string
	MaxAgeSeconds       *int64
	Display             string
}

type OIDCAuthorizeResult struct {
	TransactionHandle string
	RedirectURI       string
	NeedsLogin        bool
	NeedsConsent      bool
	ErrorCode         string
	ErrorDescription  string
	Code              string
	State             string
	Issuer            string
}

func (s *OIDCProviderService) BeginAuthorization(ctx context.Context, input OIDCAuthorizeInput, sessionHandle string) (*OIDCAuthorizeResult, error) {
	if err := s.RequireActiveSigningKey(ctx); err != nil {
		return nil, err
	}
	client, err := s.repo.GetClient(ctx, input.ClientID)
	if err != nil {
		return nil, ErrOIDCInvalidClient
	}
	if !client.Enabled || client.ClientType != OIDCClientTypeConfidential || !exactString(client.RedirectURIs, input.RedirectURI) {
		return nil, ErrOIDCInvalidClient
	}
	if input.ResponseType != "code" {
		return nil, ErrOIDCUnsupportedResponseType
	}
	if input.ResponseMode != "" && input.ResponseMode != "query" {
		return nil, ErrOIDCInvalidRequest
	}
	if !validOpaque(input.State, 8, 1024) || !validOpaque(input.Nonce, 8, 512) {
		return nil, ErrOIDCInvalidRequest
	}
	if !validOIDCPKCEChallenge(input.CodeChallenge) || input.CodeChallengeMethod != OIDCCodeChallengeS256 {
		return nil, ErrOIDCInvalidRequest
	}
	if input.Display != "" && !contains([]string{"page", "popup", "touch", "wap"}, input.Display) {
		return nil, ErrOIDCInvalidRequest
	}
	prompt, err := normalizeOIDCPrompt(input.Prompt)
	if err != nil {
		return nil, err
	}
	scopes, err := s.validateScopes(input.Scope, client)
	if err != nil {
		return nil, err
	}
	handle, err := oidcRandomToken(32)
	if err != nil {
		return nil, err
	}
	handleDigest := oidcDigest(handle)
	stateCiphertext, stateFingerprint, err := s.protector.seal(input.State, "state", handleDigest+"|"+input.ClientID)
	if err != nil {
		return nil, err
	}
	nonceCiphertext, nonceFingerprint, err := s.protector.seal(input.Nonce, "nonce", handleDigest+"|"+input.ClientID)
	if err != nil {
		return nil, err
	}
	_, err = s.repo.CreateAuthorizationTransaction(ctx, OIDCTransactionCreateInput{HandleDigest: handleDigest, ClientPK: client.ID, RedirectURI: input.RedirectURI, ScopeSnapshot: scopes, StateCiphertext: stateCiphertext, StateFingerprint: stateFingerprint, NonceCiphertext: nonceCiphertext, NonceFingerprint: nonceFingerprint, CodeChallenge: input.CodeChallenge, CodeChallengeMethod: OIDCCodeChallengeS256, Prompt: prompt, MaxAgeSeconds: input.MaxAgeSeconds, Display: displayPtr(input.Display), ExpiresAt: time.Now().UTC().Add(s.cfg.OIDCProvider.TransactionTTL())})
	if err != nil {
		return nil, err
	}
	result := &OIDCAuthorizeResult{TransactionHandle: handle, RedirectURI: input.RedirectURI, Issuer: OIDCProviderIssuer}
	transaction, err := s.repo.GetAuthorizationTransaction(ctx, handleDigest, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	session, user, sessionErr := s.loadSession(ctx, sessionHandle)
	if sessionErr != nil && !errors.Is(sessionErr, sql.ErrNoRows) && !errors.Is(sessionErr, ErrOIDCLoginRequired) && !errors.Is(sessionErr, ErrOIDCUserInactive) {
		return nil, sessionErr
	}
	mustLogin := sessionErr != nil || session == nil || user == nil || (contains(strings.Fields(prompt), "login") || contains(strings.Fields(prompt), "select_account")) || !withinMaxAge(session, input.MaxAgeSeconds)
	if mustLogin {
		if contains(strings.Fields(prompt), "none") {
			result.ErrorCode = "login_required"
			result.ErrorDescription = "interactive login is not permitted"
			result.State, err = s.transactionState(transaction, client, handle)
			if err != nil {
				return nil, err
			}
			return result, nil
		}
		result.NeedsLogin = true
		return result, nil
	}
	if err := s.repo.SetTransactionAuthenticated(ctx, transaction.ID, session.ID, user.ID, session.AuthTime); err != nil {
		return nil, err
	}
	return s.finishAuthorization(ctx, handle, client, transaction, user, prompt)
}

func (s *OIDCProviderService) loadSession(ctx context.Context, raw string) (*OIDCBrowserSessionRecord, *OIDCUserRecord, error) {
	if raw == "" {
		return nil, nil, sql.ErrNoRows
	}
	session, err := s.repo.GetBrowserSession(ctx, oidcDigest(raw), time.Now().UTC())
	if err != nil {
		return nil, nil, err
	}
	if session == nil || session.SessionVersion <= 0 {
		return nil, nil, ErrOIDCLoginRequired
	}
	user, err := s.repo.GetUser(ctx, session.UserID)
	if err != nil {
		return nil, nil, err
	}
	if user.Status != StatusActive || user.DeletedAt != nil {
		return nil, nil, ErrOIDCUserInactive
	}
	return session, user, nil
}

func (s *OIDCProviderService) finishAuthorization(ctx context.Context, handle string, client *OIDCClientRecord, transaction *OIDCTransactionRecord, user *OIDCUserRecord, prompt string) (*OIDCAuthorizeResult, error) {
	result := &OIDCAuthorizeResult{TransactionHandle: handle, RedirectURI: transaction.RedirectURI, Issuer: OIDCProviderIssuer}
	scopeHash := oidcFingerprint(transaction.ScopeSnapshot)
	consent, consentErr := s.repo.GetConsent(ctx, user.ID, client.ID, scopeHash, client.PolicyVersion)
	if consentErr != nil && !errors.Is(consentErr, sql.ErrNoRows) {
		return nil, consentErr
	}
	if !contains(strings.Fields(prompt), "consent") && consentErr == nil && consent.Status == "active" {
		if err := s.repo.SetTransactionConsent(ctx, transaction.ID, consent.ID); err != nil {
			return nil, err
		}
		return s.issueAuthorizationCode(ctx, handle, client, user)
	}
	if trustedOIDCConsentSkipAllowed(prompt, client.TrustedSkipConsent) {
		consentID, err := s.repo.CreateConsent(ctx, OIDCConsentCreateInput{UserID: user.ID, ClientPK: client.ID, ScopeSnapshot: transaction.ScopeSnapshot, ScopeSetHash: scopeHash, PolicyVersion: client.PolicyVersion, Source: "admin_pre_authorized"})
		if err != nil {
			return nil, err
		}
		if err := s.repo.SetTransactionConsent(ctx, transaction.ID, consentID); err != nil {
			return nil, err
		}
		return s.issueAuthorizationCode(ctx, handle, client, user)
	}
	if contains(strings.Fields(prompt), "none") {
		result.ErrorCode = "consent_required"
		result.ErrorDescription = "consent is required"
		state, stateErr := s.transactionState(transaction, client, handle)
		if stateErr != nil {
			return nil, stateErr
		}
		result.State = state
		return result, nil
	}
	result.NeedsConsent = true
	return result, nil
}

func (s *OIDCProviderService) transactionState(transaction *OIDCTransactionRecord, client *OIDCClientRecord, handle string) (string, error) {
	if transaction == nil || client == nil || s == nil || s.protector == nil {
		return "", ErrOIDCInvalidRequest
	}
	return s.protector.open(transaction.StateCiphertext, "state", oidcDigest(handle)+"|"+client.ClientID)
}

func (s *OIDCProviderService) issueAuthorizationCode(ctx context.Context, handle string, client *OIDCClientRecord, user *OIDCUserRecord) (*OIDCAuthorizeResult, error) {
	transaction, err := s.repo.GetAuthorizationTransaction(ctx, oidcDigest(handle), time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if transaction.AuthTime == nil || transaction.UserID == nil || transaction.ConsentID == nil || *transaction.ConsentID <= 0 {
		return nil, ErrOIDCConsentRequired
	}
	rawCode, err := oidcRandomToken(32)
	if err != nil {
		return nil, err
	}
	if _, err := s.repo.CreateAuthorizationCode(ctx, OIDCAuthorizationCodeCreateInput{TransactionID: transaction.ID, TransactionHandleDigest: oidcDigest(handle), CodeDigest: oidcDigest(rawCode), ClientPK: client.ID, UserID: user.ID, ConsentID: *transaction.ConsentID, BrowserSessionID: transaction.BrowserSessionID, RedirectURI: transaction.RedirectURI, ScopeSnapshot: transaction.ScopeSnapshot, NonceCiphertext: transaction.NonceCiphertext, NonceFingerprint: transaction.NonceFingerprint, CodeChallenge: transaction.CodeChallenge, CodeChallengeMethod: transaction.CodeChallengeMethod, AuthTime: *transaction.AuthTime, IssuedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(s.cfg.OIDCProvider.AuthorizationCodeTTL())}); err != nil {
		return nil, err
	}
	state, err := s.protector.open(transaction.StateCiphertext, "state", oidcDigest(handle)+"|"+client.ClientID)
	if err != nil {
		return nil, err
	}
	return &OIDCAuthorizeResult{TransactionHandle: handle, RedirectURI: transaction.RedirectURI, Code: rawCode, State: state, Issuer: OIDCProviderIssuer}, nil
}

func (s *OIDCProviderService) ContinueAuthorization(ctx context.Context, handle, sessionHandle string) (*OIDCAuthorizeResult, error) {
	if err := s.RequireActiveSigningKey(ctx); err != nil {
		return nil, err
	}
	transaction, err := s.repo.GetAuthorizationTransaction(ctx, oidcDigest(handle), time.Now().UTC())
	if err != nil {
		return nil, ErrOIDCInvalidRequest
	}
	client, err := s.repo.GetClientByID(ctx, transaction.ClientPK)
	if err != nil {
		return nil, ErrOIDCInvalidClient
	}
	if !client.Enabled || client.ClientType != OIDCClientTypeConfidential || !exactString(client.RedirectURIs, transaction.RedirectURI) {
		return nil, ErrOIDCInvalidClient
	}
	session, user, err := s.loadSession(ctx, sessionHandle)
	if err != nil {
		return nil, ErrOIDCLoginRequired
	}
	if err := s.repo.SetTransactionAuthenticated(ctx, transaction.ID, session.ID, user.ID, session.AuthTime); err != nil {
		return nil, err
	}
	transaction.UserID = &user.ID
	transaction.BrowserSessionID = &session.ID
	if transaction.AuthTime == nil {
		transaction.AuthTime = &session.AuthTime
	}
	return s.finishAuthorization(ctx, handle, client, transaction, user, transaction.Prompt)
}
func (s *OIDCProviderService) AuthenticateTransaction(ctx context.Context, handle, email, password, totpCode, attemptKey string) (string, error) {
	return s.AuthenticateTransactionWithSession(ctx, handle, email, password, totpCode, "", attemptKey)
}

func (s *OIDCProviderService) AuthenticateTransactionWithSession(ctx context.Context, handle, email, password, totpCode, currentSessionHandle, attemptKey string) (string, error) {
	if err := s.RequireActiveSigningKey(ctx); err != nil {
		return "", err
	}
	if retry, blocked := s.loginGuard.check(attemptKey, time.Now().UTC()); blocked {
		return "", ErrOIDCLoginRequired
	} else if retry > 0 {
		return "", ErrOIDCLoginRequired
	}
	transaction, err := s.repo.GetAuthorizationTransaction(ctx, oidcDigest(handle), time.Now().UTC())
	if err != nil {
		return "", ErrOIDCInvalidRequest
	}
	fail := func() (string, error) {
		blocked := s.loginGuard.failure(attemptKey, time.Now().UTC())
		if blocked {
			slog.Warn("oidc provider login attempt rejected", "event", "oidc_login_guard_reject")
		}
		return "", ErrOIDCLoginRequired
	}
	user, err := s.users.GetByEmail(ctx, strings.TrimSpace(email))
	if err != nil || user == nil || !user.IsActive() || !user.CheckPassword(password) {
		return fail()
	}
	amr := OIDCSessionAMRPassword
	if user.TotpEnabled {
		if s.totp == nil || strings.TrimSpace(totpCode) == "" {
			return fail()
		}
		if err := s.totp.VerifyCode(ctx, user.ID, totpCode); err != nil {
			return fail()
		}
		amr += " " + OIDCSessionAMRTOTP
	}
	rawSession, err := s.establishBrowserSession(ctx, transaction, user.ID, amr, currentSessionHandle)
	if err != nil {
		return "", err
	}
	s.loginGuard.success(attemptKey, time.Now().UTC())
	return rawSession, nil
}

// establishBrowserSession 建立新的浏览器会话、按需轮换旧会话，并把授权事务标记为已认证，
// 返回新的 raw session handle。密码登录与跨域 SSO 共用此逻辑（不含登录限流的成功计数）。
func (s *OIDCProviderService) establishBrowserSession(ctx context.Context, transaction *OIDCTransactionRecord, userID int64, amr, currentSessionHandle string) (string, error) {
	now := time.Now().UTC()
	rawSession, err := oidcRandomToken(32)
	if err != nil {
		return "", err
	}
	if err := s.repo.CreateBrowserSession(ctx, oidcDigest(rawSession), userID, now, amr, now.Add(s.cfg.OIDCProvider.BrowserSessionIdleTTL()), now.Add(s.cfg.OIDCProvider.BrowserSessionAbsoluteTTL())); err != nil {
		return "", err
	}
	if currentSessionHandle != "" {
		oldSession, oldErr := s.repo.GetBrowserSession(ctx, oidcDigest(currentSessionHandle), now)
		if oldErr != nil && !errors.Is(oldErr, sql.ErrNoRows) {
			_ = s.repo.RevokeBrowserSession(ctx, oidcDigest(rawSession), "session_rotation_failed")
			return "", oldErr
		}
		if oldErr == nil && oldSession != nil {
			if oldSession.SessionVersion <= 0 {
				_ = s.repo.RevokeBrowserSession(ctx, oidcDigest(rawSession), "session_rotation_failed")
				return "", ErrOIDCLoginRequired
			}
			if err := s.repo.RevokeBrowserSession(ctx, oidcDigest(currentSessionHandle), "reauthenticated"); err != nil {
				_ = s.repo.RevokeBrowserSession(ctx, oidcDigest(rawSession), "session_rotation_failed")
				return "", err
			}
		}
	}
	session, err := s.repo.GetBrowserSession(ctx, oidcDigest(rawSession), now)
	if err != nil || session == nil || session.UserID != userID || session.SessionVersion <= 0 {
		return "", ErrOIDCLoginRequired
	}
	if err := s.repo.SetTransactionAuthenticated(ctx, transaction.ID, session.ID, userID, session.AuthTime); err != nil {
		return "", err
	}
	return rawSession, nil
}

func (s *OIDCProviderService) ApproveConsent(ctx context.Context, handle, sessionHandle string, approve bool) (*OIDCAuthorizeResult, error) {
	if err := s.RequireActiveSigningKey(ctx); err != nil {
		return nil, err
	}
	transaction, err := s.repo.GetAuthorizationTransaction(ctx, oidcDigest(handle), time.Now().UTC())
	if err != nil {
		return nil, ErrOIDCInvalidRequest
	}
	if !approve {
		if err := s.repo.DenyTransaction(ctx, transaction.ID); err != nil {
			return nil, err
		}
		client, clientErr := s.repo.GetClientByID(ctx, transaction.ClientPK)
		if clientErr != nil {
			return nil, ErrOIDCInvalidClient
		}
		state, stateErr := s.transactionState(transaction, client, handle)
		if stateErr != nil {
			return nil, stateErr
		}
		return &OIDCAuthorizeResult{RedirectURI: transaction.RedirectURI, ErrorCode: "access_denied", ErrorDescription: "authorization denied", State: state, Issuer: OIDCProviderIssuer}, nil
	}
	session, user, err := s.loadSession(ctx, sessionHandle)
	if err != nil {
		return nil, ErrOIDCLoginRequired
	}
	if transaction.UserID == nil || *transaction.UserID != user.ID || transaction.BrowserSessionID == nil || *transaction.BrowserSessionID != session.ID {
		return nil, ErrOIDCInvalidRequest
	}
	client, err := s.repo.GetClientByID(ctx, transaction.ClientPK)
	if err != nil {
		return nil, ErrOIDCInvalidClient
	}
	if !client.Enabled || client.ClientType != OIDCClientTypeConfidential || !exactString(client.RedirectURIs, transaction.RedirectURI) {
		return nil, ErrOIDCInvalidClient
	}
	consentID, err := s.repo.CreateConsent(ctx, OIDCConsentCreateInput{UserID: user.ID, ClientPK: client.ID, ScopeSnapshot: transaction.ScopeSnapshot, ScopeSetHash: oidcFingerprint(transaction.ScopeSnapshot), PolicyVersion: client.PolicyVersion, Source: "interactive"})
	if err != nil {
		return nil, err
	}
	if err := s.repo.SetTransactionConsent(ctx, transaction.ID, consentID); err != nil {
		return nil, err
	}
	return s.issueAuthorizationCode(ctx, handle, client, user)
}

func (s *OIDCProviderService) TokenCode(ctx context.Context, clientID, secret, code, redirectURI, verifier string) (*OIDCTokenResponse, error) {
	if err := s.RequireActiveSigningKey(ctx); err != nil {
		return nil, err
	}
	client, err := s.authenticateClient(ctx, clientID, secret)
	if err != nil {
		return nil, err
	}
	if !validOpaque(code, 32, 512) || !validOIDCPKCEVerifier(verifier) || strings.TrimSpace(redirectURI) == "" || !exactString(client.RedirectURIs, redirectURI) {
		return nil, ErrOIDCInvalidGrant
	}
	codeRecord, err := s.repo.GetAuthorizationCode(ctx, oidcDigest(code), time.Now().UTC())
	if err != nil {
		return nil, ErrOIDCInvalidGrant
	}
	if codeRecord.ClientPK != client.ID || codeRecord.RedirectURI != redirectURI || codeRecord.ConsentID <= 0 {
		return nil, ErrOIDCInvalidGrant
	}
	user, err := s.repo.GetUser(ctx, codeRecord.UserID)
	if err != nil || user.Status != StatusActive || user.DeletedAt != nil {
		return nil, ErrOIDCInvalidGrant
	}
	nonce, err := s.protector.open(codeRecord.NonceCiphertext, "nonce", codeRecord.TransactionHandleDigest+"|"+client.ClientID)
	if err != nil {
		return nil, ErrOIDCInvalidGrant
	}
	accessToken, err := oidcRandomToken(32)
	if err != nil {
		return nil, err
	}
	refreshToken := ""
	familyID := ""
	if _, ok := scopeSet(codeRecord.ScopeSnapshot)[OIDCScopeOfflineAccess]; ok {
		refreshToken, err = oidcRandomToken(48)
		if err != nil {
			return nil, err
		}
		familyID, err = oidcRandomToken(24)
		if err != nil {
			return nil, err
		}
	}
	role, err := MapOIDCRole(user.Role)
	if err != nil {
		return nil, ErrOIDCInvalidGrant
	}
	_ = role
	idToken, err := s.signing.SignIDToken(ctx, client.ClientID, OIDCClaimsSubject{Subject: user.Subject, Email: user.Email, Username: user.Username, Role: user.Role, AuthTime: codeRecord.AuthTime}, nonce, codeRecord.ScopeSnapshot, s.cfg.OIDCProvider.IDTokenTTL())
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if _, err := s.repo.ExchangeAuthorizationCode(ctx, OIDCAuthorizationCodeExchangeInput{CodeDigest: oidcDigest(code), ClientPK: client.ID, RedirectURI: redirectURI, ConsentID: codeRecord.ConsentID, CodeVerifier: verifier, RawAccessToken: accessToken, RawRefreshToken: refreshToken, FamilyID: familyID, AccessIssuedAt: now, AccessExpiresAt: now.Add(s.cfg.OIDCProvider.AccessTokenTTL()), RefreshIssuedAt: now, RefreshIdleUntil: now.Add(s.cfg.OIDCProvider.RefreshTokenIdleTTL()), RefreshAbsoluteUntil: now.Add(s.cfg.OIDCProvider.RefreshTokenAbsoluteTTL())}); err != nil {
		return nil, err
	}
	return &OIDCTokenResponse{AccessToken: accessToken, RefreshToken: refreshToken, IDToken: idToken, Scope: codeRecord.ScopeSnapshot, ExpiresIn: s.cfg.OIDCProvider.AccessTokenTTLSeconds}, nil
}

type OIDCTokenResponse struct {
	AccessToken  string
	RefreshToken string
	IDToken      string
	Scope        string
	ExpiresIn    int
}

func (s *OIDCProviderService) TokenRefresh(ctx context.Context, clientID, secret, refreshToken, requestedScope string) (*OIDCTokenResponse, error) {
	if err := s.RequireActiveSigningKey(ctx); err != nil {
		return nil, err
	}
	client, err := s.authenticateClient(ctx, clientID, secret)
	if err != nil {
		return nil, err
	}
	if !validOpaque(refreshToken, 32, 512) {
		return nil, ErrOIDCInvalidGrant
	}
	normalizedScope, err := s.normalizeRefreshScope(requestedScope, client)
	if err != nil {
		return nil, err
	}
	newRefresh, err := oidcRandomToken(48)
	if err != nil {
		return nil, err
	}
	newAccess, err := oidcRandomToken(32)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	result, err := s.repo.RotateRefreshToken(ctx, OIDCRefreshRotationInput{ClientPK: client.ID, TokenDigest: oidcDigest(refreshToken), NewTokenDigest: newRefresh, NewAccessTokenDigest: newAccess, Now: now, NewRefreshIssuedAt: now, NewRefreshExpiresAt: now.Add(s.cfg.OIDCProvider.RefreshTokenIdleTTL()), NewAccessIssuedAt: now, NewAccessExpiresAt: now.Add(s.cfg.OIDCProvider.AccessTokenTTL()), RequestedScope: normalizedScope})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, ErrOIDCServerError
	}
	if result.Replayed {
		return nil, ErrOIDCReplayDetected
	}
	return &OIDCTokenResponse{AccessToken: newAccess, RefreshToken: newRefresh, Scope: result.Scope, ExpiresIn: s.cfg.OIDCProvider.AccessTokenTTLSeconds}, nil
}

func (s *OIDCProviderService) UserInfo(ctx context.Context, accessToken string) (map[string]any, error) {
	if err := s.RequireActiveSigningKey(ctx); err != nil {
		return nil, err
	}
	if strings.TrimSpace(accessToken) == "" {
		return nil, ErrOIDCInvalidGrant
	}
	user, scope, _, err := s.repo.GetAccessToken(ctx, oidcDigest(accessToken), time.Now().UTC())
	if err != nil {
		return nil, ErrOIDCInvalidGrant
	}
	role, err := MapOIDCRole(user.Role)
	if err != nil {
		return nil, ErrOIDCInvalidGrant
	}
	_ = role
	return UserInfoClaims(OIDCClaimsSubject{Subject: user.Subject, Email: user.Email, Username: user.Username, Role: user.Role}, scope)
}

func (s *OIDCProviderService) Revoke(ctx context.Context, clientID, secret, token string) error {
	if !validOIDCRevocationToken(token) {
		return ErrOIDCInvalidRequest
	}
	if err := s.RequireActiveSigningKey(ctx); err != nil {
		return err
	}
	client, err := s.authenticateClient(ctx, clientID, secret)
	if err != nil {
		return err
	}
	return s.repo.RevokeToken(ctx, client.ID, oidcDigest(token), "client_revocation")
}

func (s *OIDCProviderService) authenticateClient(ctx context.Context, clientID, secret string) (*OIDCClientRecord, error) {
	if strings.TrimSpace(clientID) == "" || strings.TrimSpace(secret) == "" {
		return nil, ErrOIDCInvalidClient
	}
	client, err := s.repo.AuthenticateClient(ctx, clientID, oidcSecretDigest(s.cfg.OIDCProvider.SecretPepper, secret), time.Now().UTC())
	if err != nil {
		return nil, ErrOIDCInvalidClient
	}
	return client, nil
}

func (s *OIDCProviderService) ValidateClientRedirect(ctx context.Context, clientID, redirectURI string) (*OIDCClientRecord, error) {
	if err := s.enabled(); err != nil {
		return nil, err
	}
	client, err := s.repo.GetClient(ctx, clientID)
	if err != nil || !client.Enabled || !exactString(client.RedirectURIs, redirectURI) {
		return nil, ErrOIDCInvalidClient
	}
	return client, nil
}

func (s *OIDCProviderService) AdminListClients(ctx context.Context) ([]OIDCClientRecord, error) {
	if err := s.enabled(); err != nil {
		return nil, err
	}
	return s.repo.ListClients(ctx)
}

func (s *OIDCProviderService) AdminGetClient(ctx context.Context, clientPK int64) (*OIDCClientRecord, error) {
	if err := s.enabled(); err != nil {
		return nil, err
	}
	return s.repo.GetClientByID(ctx, clientPK)
}

func (s *OIDCProviderService) AdminCreateClient(ctx context.Context, input OIDCClientCreateInput) (*OIDCClientRecord, string, error) {
	if err := s.enabled(); err != nil {
		return nil, "", err
	}
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Owner) == "" || len(input.RedirectURIs) == 0 {
		return nil, "", ErrOIDCInvalidRequest
	}
	for _, redirect := range input.RedirectURIs {
		if err := validateOIDCRedirect(redirect); err != nil {
			return nil, "", err
		}
	}
	allowed, err := s.validateAllowedScopes(input.AllowedScopes)
	if err != nil {
		return nil, "", err
	}
	clientID, err := oidcRandomToken(24)
	if err != nil {
		return nil, "", err
	}
	secret, err := oidcRandomToken(48)
	if err != nil {
		return nil, "", err
	}
	now := time.Now().UTC()
	input.ClientID = "sub2-" + clientID
	input.SecretDigest = oidcSecretDigest(s.cfg.OIDCProvider.SecretPepper, secret)
	input.SecretFingerprint = oidcFingerprint(secret)
	input.AllowedScopes = allowed
	input.SecretNotBefore = now
	input.SecretExpiresAt = now.Add(time.Duration(s.cfg.OIDCProvider.ClientSecretMaxOverlapSeconds) * time.Second)
	input.Now = now
	client, err := s.repo.CreateClient(ctx, input)
	if err != nil {
		return nil, "", err
	}
	return client, secret, nil
}

func (s *OIDCProviderService) AdminUpdateClient(ctx context.Context, input OIDCClientUpdateInput) (*OIDCClientRecord, error) {
	if err := s.enabled(); err != nil {
		return nil, err
	}
	if input.ClientPK <= 0 || input.ExpectedVersion <= 0 || strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Owner) == "" || len(input.RedirectURIs) == 0 {
		return nil, ErrOIDCInvalidRequest
	}
	for _, redirect := range input.RedirectURIs {
		if err := validateOIDCRedirect(redirect); err != nil {
			return nil, err
		}
	}
	allowed, err := s.validateAllowedScopes(input.AllowedScopes)
	if err != nil {
		return nil, err
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Owner = strings.TrimSpace(input.Owner)
	input.AllowedScopes = allowed
	return s.repo.UpdateClient(ctx, input)
}

func (s *OIDCProviderService) AdminRevokeSecret(ctx context.Context, clientPK, secretID, actorID int64, reason string) error {
	if err := s.enabled(); err != nil {
		return err
	}
	return s.repo.RevokeClientSecret(ctx, clientPK, secretID, actorID, reason)
}

func (s *OIDCProviderService) AdminSetKeyStatus(ctx context.Context, kid, status string, actorID int64, reason string) error {
	if err := s.enabled(); err != nil {
		return err
	}
	return s.repo.SetSigningKeyStatus(ctx, kid, status, actorID, reason)
}

func (s *OIDCProviderService) AdminRotateSecret(ctx context.Context, clientPK, actorID int64, reason string) (*OIDCClientSecretRecord, string, error) {
	if err := s.enabled(); err != nil {
		return nil, "", err
	}
	secret, err := oidcRandomToken(48)
	if err != nil {
		return nil, "", err
	}
	now := time.Now().UTC()
	record, err := s.repo.CreateClientSecret(ctx, clientPK, actorID, oidcSecretDigest(s.cfg.OIDCProvider.SecretPepper, secret), oidcFingerprint(secret), now, now.Add(time.Duration(s.cfg.OIDCProvider.ClientSecretMaxOverlapSeconds)*time.Second), reason)
	if err != nil {
		return nil, "", err
	}
	return record, secret, nil
}

func (s *OIDCProviderService) AdminRotateKey(ctx context.Context, actorID int64, reason string) (*OIDCSigningKeyRecord, error) {
	if err := s.enabled(); err != nil {
		return nil, err
	}
	if s.signing == nil {
		return nil, ErrOIDCKeyUnavailable
	}
	return s.signing.GenerateAndActivate(ctx, actorID, reason)
}

func (s *OIDCProviderService) AdminKeys(ctx context.Context) ([]OIDCSigningKeyRecord, error) {
	if err := s.enabled(); err != nil {
		return nil, err
	}
	if s.signing == nil {
		return nil, ErrOIDCKeyUnavailable
	}
	return s.signing.KeyMetadata(ctx)
}

func (s *OIDCProviderService) AdminEnableClient(ctx context.Context, clientPK, actorID int64, enabled bool, reason string) (*OIDCClientRecord, error) {
	if err := s.enabled(); err != nil {
		return nil, err
	}
	return s.repo.SetClientEnabled(ctx, clientPK, actorID, enabled, reason)
}

func (s *OIDCProviderService) AdminListConsents(ctx context.Context, input OIDCConsentListInput) ([]OIDCConsentRecord, error) {
	if err := s.enabled(); err != nil {
		return nil, err
	}
	return s.repo.ListConsents(ctx, input)
}

func (s *OIDCProviderService) AdminRevokeConsent(ctx context.Context, consentID, actorID int64, reason string) (*OIDCConsentRecord, error) {
	if err := s.enabled(); err != nil {
		return nil, err
	}
	if err := s.repo.RevokeConsent(ctx, consentID, actorID, reason); err != nil {
		return nil, err
	}
	return s.repo.GetConsentByID(ctx, consentID)
}

func (s *OIDCProviderService) AdminListAuditEvents(ctx context.Context, input OIDCAuditListInput) ([]OIDCAuditEventRecord, error) {
	if err := s.enabled(); err != nil {
		return nil, err
	}
	return s.repo.ListAuditEvents(ctx, input)
}

func (s *OIDCProviderService) CSRFToken(handle, action string) string {
	if s == nil || s.cfg == nil {
		return ""
	}
	return oidcSecretDigest(s.cfg.OIDCProvider.SecretPepper, action+"|"+handle)
}

// OIDCAdminCSRFToken derives a per-admin-session synchronizer token. The token
// is deliberately stateless: it is bound to the authenticated subject, the
// current JWT session id, and the current token version, so it changes when a
// session is rotated or the account epoch is invalidated.
func OIDCAdminCSRFToken(secretPepper string, userID int64, sessionID string, tokenVersion int64) string {
	if strings.TrimSpace(secretPepper) == "" || userID <= 0 || strings.TrimSpace(sessionID) == "" {
		return ""
	}
	return oidcSecretDigest(secretPepper, "oidc-admin-csrf|"+strconv.FormatInt(userID, 10)+"|"+sessionID+"|"+strconv.FormatInt(tokenVersion, 10))
}

func (s *OIDCProviderService) ValidCSRF(handle, action, supplied, cookie string) bool {
	expected := s.CSRFToken(handle, action)
	return expected != "" && subtle.ConstantTimeCompare([]byte(expected), []byte(supplied)) == 1 && subtle.ConstantTimeCompare([]byte(expected), []byte(cookie)) == 1
}

func (s *OIDCProviderService) supportedClaims(scopes []string) []string {
	claims := []string{"iss", "sub", "aud", "exp", "iat", "nonce", "auth_time"}
	if contains(scopes, OIDCScopeProfile) {
		claims = append(claims, "preferred_username")
	}
	if contains(scopes, OIDCScopeEmail) {
		claims = append(claims, "email")
	}
	if contains(scopes, OIDCScopeRoles) {
		claims = append(claims, "role")
	}
	return claims
}

func (s *OIDCProviderService) normalizeRefreshScope(raw string, client *OIDCClientRecord) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	if !validOpaque(raw, 1, 512) {
		return "", ErrOIDCInvalidScope
	}
	items := strings.Fields(raw)
	for _, item := range items {
		if !contains(s.cfg.OIDCProvider.AllowedScopes, item) || !contains(client.AllowedScopes, item) {
			return "", ErrOIDCInvalidScope
		}
	}
	return oidcCanonicalScope(items)
}

func (s *OIDCProviderService) validateScopes(raw string, client *OIDCClientRecord) (string, error) {
	items := strings.Fields(raw)
	if len(items) == 0 {
		return "", ErrOIDCInvalidScope
	}
	if _, ok := scopeSet(raw)[OIDCScopeOpenID]; !ok {
		return "", ErrOIDCInvalidScope
	}
	for _, item := range items {
		if !contains(s.cfg.OIDCProvider.AllowedScopes, item) || !contains(client.AllowedScopes, item) {
			return "", ErrOIDCInvalidScope
		}
	}
	return oidcCanonicalScope(items)
}
func (s *OIDCProviderService) validateAllowedScopes(scopes []string) ([]string, error) {
	seen := make(map[string]struct{}, len(scopes))
	for _, item := range scopes {
		item = strings.TrimSpace(item)
		if item == "" {
			return nil, ErrOIDCInvalidScope
		}
		if _, duplicate := seen[item]; duplicate {
			return nil, ErrOIDCInvalidScope
		}
		if !contains(s.cfg.OIDCProvider.AllowedScopes, item) {
			return nil, ErrOIDCInvalidScope
		}
		seen[item] = struct{}{}
	}
	if _, ok := seen[OIDCScopeOpenID]; !ok {
		scopes = append(scopes, OIDCScopeOpenID)
	}
	sort.Strings(scopes)
	return scopes, nil
}
func exactString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
func validOpaque(value string, min, max int) bool {
	if len(value) < min || len(value) > max {
		return false
	}
	for _, r := range value {
		if r < 0x21 || r == 0x7f {
			return false
		}
	}
	return true
}
func normalizeOIDCPrompt(raw string) (string, error) {
	seen := map[string]struct{}{}
	for _, item := range strings.Fields(raw) {
		if !contains([]string{"none", "login", "consent", "select_account"}, item) {
			return "", ErrOIDCInvalidRequest
		}
		if _, ok := seen[item]; ok {
			return "", ErrOIDCInvalidRequest
		}
		seen[item] = struct{}{}
	}
	if _, ok := seen["none"]; ok && len(seen) > 1 {
		return "", ErrOIDCInvalidRequest
	}
	items := make([]string, 0, len(seen))
	for item := range seen {
		items = append(items, item)
	}
	sort.Strings(items)
	return strings.Join(items, " "), nil
}
func trustedOIDCConsentSkipAllowed(prompt string, trusted bool) bool {
	return trusted && !contains(strings.Fields(prompt), "consent")
}

func validOIDCPKCEChallenge(raw string) bool {
	if len(raw) != 43 {
		return false
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(raw)
	return err == nil && len(decoded) == sha256.Size
}

func validOIDCPKCEVerifier(raw string) bool {
	if len(raw) < 43 || len(raw) > 128 {
		return false
	}
	for _, r := range raw {
		if !strings.ContainsRune("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~", r) {
			return false
		}
	}
	return true
}
func validOIDCRevocationToken(raw string) bool {
	return validOpaque(raw, 1, 512)
}

func displayPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
func withinMaxAge(session *OIDCBrowserSessionRecord, maxAge *int64) bool {
	return maxAge == nil || session == nil || time.Since(session.AuthTime) <= time.Duration(*maxAge)*time.Second
}
func validateOIDCRedirect(raw string) error {
	if !validOpaque(raw, 1, 2048) {
		return ErrOIDCInvalidRequest
	}
	parsed, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.Host == "" || parsed.Hostname() == "" || parsed.Opaque != "" || parsed.Fragment != "" || parsed.RawFragment != "" || parsed.User != nil {
		return ErrOIDCInvalidRequest
	}
	return nil
}

// OIDCConsentView 授权确认页展示所需的只读信息。
type OIDCConsentView struct {
	ClientName  string
	ClientOwner string
	Scopes      []string
	UserEmail   string
	UserName    string
}

// LoadConsentView 汇总授权确认页展示数据：接入应用名/负责人、请求的 scope 列表、
// 以及当前登录用户身份。优先取事务上已认证用户，回退到浏览器会话。
func (s *OIDCProviderService) LoadConsentView(ctx context.Context, handle, sessionHandle string) (*OIDCConsentView, error) {
	transaction, err := s.repo.GetAuthorizationTransaction(ctx, oidcDigest(handle), time.Now().UTC())
	if err != nil {
		return nil, ErrOIDCInvalidRequest
	}
	client, err := s.repo.GetClientByID(ctx, transaction.ClientPK)
	if err != nil {
		return nil, ErrOIDCInvalidClient
	}
	view := &OIDCConsentView{
		ClientName:  client.Name,
		ClientOwner: client.Owner,
		Scopes:      strings.Fields(transaction.ScopeSnapshot),
	}
	if transaction.UserID != nil && *transaction.UserID != 0 {
		if user, uErr := s.repo.GetUser(ctx, *transaction.UserID); uErr == nil && user != nil {
			view.UserEmail = user.Email
			view.UserName = user.Username
			return view, nil
		}
	}
	if sessionHandle != "" {
		if _, user, sErr := s.loadSession(ctx, sessionHandle); sErr == nil && user != nil {
			view.UserEmail = user.Email
			view.UserName = user.Username
		}
	}
	return view, nil
}

// LoginPrecheck 判断某邮箱在登录第二步是否需要 TOTP。
// 为避免账号枚举，对不存在/未激活用户返回 false（与"未启用 2FA"一致），
// 仅当用户存在、激活且个人开启了 2FA 时返回 true。
// attemptKey 命中登录限流时同样返回 false（不泄露 2FA 状态，仅退回密码页）。
func (s *OIDCProviderService) LoginPrecheck(ctx context.Context, email, attemptKey string) bool {
	if _, blocked := s.loginGuard.check(attemptKey, time.Now().UTC()); blocked {
		return false
	}
	user, err := s.users.GetByEmail(ctx, strings.TrimSpace(email))
	if err != nil || user == nil || !user.IsActive() {
		return false
	}
	return user.TotpEnabled
}

// IssueSSOCode 为主面板已登录用户签发一次性跨域 SSO 凭证，绑定 user_id + 授权事务摘要，
// 60s 过期。凭证由 protector 加密签名（不可伪造），供主面板引导页换取 auth 子域会话。
// 仅当事务有效且尚未认证、用户存在且激活时签发。
func (s *OIDCProviderService) IssueSSOCode(ctx context.Context, userID int64, handle string) (string, error) {
	if err := s.RequireActiveSigningKey(ctx); err != nil {
		return "", err
	}
	if s.protector == nil {
		return "", ErrOIDCInvalidRequest
	}
	if userID <= 0 || strings.TrimSpace(handle) == "" {
		return "", ErrOIDCInvalidRequest
	}
	transaction, err := s.repo.GetAuthorizationTransaction(ctx, oidcDigest(handle), time.Now().UTC())
	if err != nil {
		return "", ErrOIDCInvalidRequest
	}
	if transaction.UserID != nil && *transaction.UserID != 0 {
		return "", ErrOIDCInvalidRequest
	}
	user, err := s.repo.GetUser(ctx, userID)
	if err != nil || user == nil || user.Status != StatusActive || user.DeletedAt != nil {
		return "", ErrOIDCUserInactive
	}
	exp := time.Now().UTC().Add(oidcSSOCodeTTL).Unix()
	payload := strconv.FormatInt(userID, 10) + "|" + oidcDigest(handle) + "|" + strconv.FormatInt(exp, 10)
	code, _, err := s.protector.seal(payload, "sso-code", oidcDigest(handle))
	if err != nil {
		return "", err
	}
	return code, nil
}

// RedeemSSOCode 校验并一次性消费跨域 SSO 凭证，返回其绑定的 user_id。
// 校验：签名有效、事务摘要匹配、未过期；随后用 Redis SETNX 标记消费，防止重放。
func (s *OIDCProviderService) RedeemSSOCode(ctx context.Context, code, handle string) (int64, error) {
	if s.protector == nil {
		return 0, ErrOIDCInvalidRequest
	}
	plain, err := s.protector.open(strings.TrimSpace(code), "sso-code", oidcDigest(handle))
	if err != nil {
		return 0, ErrOIDCInvalidRequest
	}
	parts := strings.Split(plain, "|")
	if len(parts) != 3 {
		return 0, ErrOIDCInvalidRequest
	}
	userID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || userID <= 0 {
		return 0, ErrOIDCInvalidRequest
	}
	if subtle.ConstantTimeCompare([]byte(parts[1]), []byte(oidcDigest(handle))) != 1 {
		return 0, ErrOIDCInvalidRequest
	}
	expUnix, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || time.Now().UTC().Unix() > expUnix {
		return 0, ErrOIDCInvalidRequest
	}
	if s.ssoCodeCache != nil {
		ok, cErr := s.ssoCodeCache.ConsumeOnce(ctx, oidcFingerprint(plain), oidcSSOCodeTTL)
		if cErr != nil {
			return 0, cErr
		}
		if !ok {
			return 0, ErrOIDCInvalidRequest
		}
	}
	return userID, nil
}

// EstablishBrowserSessionForUser 为已由主站鉴权的用户建立 OIDC 浏览器会话（跳过密码/TOTP 校验），
// 并把当前授权事务标记为已认证。返回新的 raw session handle，供 handler 种入会话 cookie。
func (s *OIDCProviderService) EstablishBrowserSessionForUser(ctx context.Context, handle string, userID int64, currentSessionHandle string) (string, error) {
	if err := s.RequireActiveSigningKey(ctx); err != nil {
		return "", err
	}
	transaction, err := s.repo.GetAuthorizationTransaction(ctx, oidcDigest(handle), time.Now().UTC())
	if err != nil {
		return "", ErrOIDCInvalidRequest
	}
	user, err := s.repo.GetUser(ctx, userID)
	if err != nil || user == nil || user.Status != StatusActive || user.DeletedAt != nil {
		return "", ErrOIDCUserInactive
	}
	return s.establishBrowserSession(ctx, transaction, user.ID, OIDCSessionAMRPassword, currentSessionHandle)
}
