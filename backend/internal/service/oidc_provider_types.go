package service

import (
	"context"
	"crypto/rsa"
	"errors"
	"time"
)

const (
	OIDCProviderIssuer         = "https://auth.taffy.edu.kg"
	OIDCProviderPath           = "/oauth"
	OIDCGrantAuthorizationCode = "authorization_code"
	OIDCGrantRefreshToken      = "refresh_token"
	OIDCScopeOpenID            = "openid"
	OIDCScopeProfile           = "profile"
	OIDCScopeEmail             = "email"
	OIDCScopeRoles             = "roles"
	OIDCScopeOfflineAccess     = "offline_access"
	OIDCClientTypeConfidential = "confidential"
	OIDCClientAuthBasic        = "client_secret_basic"
	OIDCCodeChallengeS256      = "S256"
	OIDCSigningRS256           = "RS256"
	OIDCSessionAMRPassword     = "pwd"
	OIDCSessionAMRTOTP         = "otp"
)

var (
	ErrOIDCProviderDisabled         = errors.New("oidc provider disabled")
	ErrOIDCInvalidClient            = errors.New("invalid oidc client")
	ErrOIDCInvalidGrant             = errors.New("invalid oidc grant")
	ErrOIDCInvalidRequest           = errors.New("invalid oidc request")
	ErrOIDCInvalidScope             = errors.New("invalid oidc scope")
	ErrOIDCUnauthorized             = errors.New("unauthorized oidc client")
	ErrOIDCUnsupportedResponseType  = errors.New("unsupported oidc response type")
	ErrOIDCInteractionRequired      = errors.New("oidc interaction required")
	ErrOIDCAccountSelectionRequired = errors.New("oidc account selection required")
	ErrOIDCServerError              = errors.New("oidc server error")
	ErrOIDCTemporarilyUnavailable   = errors.New("oidc temporarily unavailable")
	ErrOIDCVersionConflict          = errors.New("oidc client version conflict")
	ErrOIDCKeyStateConflict         = errors.New("oidc signing key state conflict")
	ErrOIDCLoginRequired            = errors.New("oidc login required")
	ErrOIDCConsentRequired          = errors.New("oidc consent required")
	ErrOIDCAccessDenied             = errors.New("oidc access denied")
	ErrOIDCKeyUnavailable           = errors.New("oidc signing key unavailable")
	ErrOIDCReplayDetected           = errors.New("oidc refresh replay detected")
	ErrOIDCUserInactive             = errors.New("oidc user inactive")
	ErrOIDCCSRFFailed               = errors.New("oidc csrf validation failed")
	// ErrOIDCProviderMisconfigured 表示授权登录入口已收敛到主面板 SSO，但
	// server.frontend_url 未配置，无法引导登录（不再降级到密码登录）。
	ErrOIDCProviderMisconfigured = errors.New("oidc provider misconfigured")
)

type OIDCClientRecord struct {
	ID                 int64
	ClientID           string
	Name               string
	Owner              string
	ClientType         string
	Enabled            bool
	TrustedSkipConsent bool
	PolicyVersion      int64
	Version            int64
	CreatedAt          time.Time
	UpdatedAt          time.Time
	DisabledAt         *time.Time
	DisabledReason     string
	RedirectURIs       []string
	AllowedScopes      []string
	Secrets            []OIDCClientSecretRecord
}

type OIDCClientSecretRecord struct {
	ID          int64
	ClientPK    int64
	Fingerprint string
	Status      string
	NotBefore   time.Time
	ExpiresAt   time.Time
	CreatedAt   time.Time
	RevokedAt   *time.Time
}

type OIDCBrowserSessionRecord struct {
	ID                int64
	UserID            int64
	AuthTime          time.Time
	AMR               string
	SessionVersion    int64
	IdleExpiresAt     time.Time
	AbsoluteExpiresAt time.Time
	LastSeenAt        time.Time
}

type OIDCTransactionRecord struct {
	ID                  int64
	HandleDigest        string
	ClientPK            int64
	BrowserSessionID    *int64
	UserID              *int64
	RedirectURI         string
	ScopeSnapshot       string
	StateCiphertext     string
	StateFingerprint    string
	NonceCiphertext     string
	NonceFingerprint    string
	CodeChallenge       string
	CodeChallengeMethod string
	Prompt              string
	MaxAgeSeconds       *int64
	Display             *string
	AuthTime            *time.Time
	ConsentID           *int64
	Status              string
	ExpiresAt           time.Time
}

type OIDCAuthorizationCodeRecord struct {
	ID                      int64
	TransactionID           int64
	TransactionHandleDigest string
	CodeDigest              string
	ClientPK                int64
	UserID                  int64
	ConsentID               int64
	BrowserSessionID        *int64
	RedirectURI             string
	ScopeSnapshot           string
	NonceCiphertext         string
	NonceFingerprint        string
	CodeChallenge           string
	CodeChallengeMethod     string
	AuthTime                time.Time
	Status                  string
	IssuedAt                time.Time
	ExpiresAt               time.Time
}

type OIDCUserRecord struct {
	ID        int64
	Subject   string
	Email     string
	Username  string
	Role      string
	Status    string
	DeletedAt *time.Time
}

type OIDCSigningKeyRecord struct {
	ID                   int64
	KID                  string
	Alg                  string
	PublicJWK            string
	PrivateKeyCiphertext string
	Fingerprint          string
	Status               string
	NotBefore            time.Time
	NotAfter             time.Time
	CreatedAt            time.Time
	ActivatedAt          *time.Time
	RetiredAt            *time.Time
	RevokedAt            *time.Time
}

type OIDCConsentRecord struct {
	ID            int64
	UserID        int64
	ClientPK      int64
	ClientID      string
	ClientName    string
	ScopeSnapshot string
	Scopes        []string
	ScopeSetHash  string
	PolicyVersion int64
	Source        string
	Status        string
	ApprovedAt    *time.Time
	RevokedAt     *time.Time
	RevokedReason string
}

type OIDCAuditEventRecord struct {
	ID                int64
	CreatedAt         time.Time
	Action            string
	Result            string
	ActorUserID       *int64
	ClientID          *string
	SecretID          *string
	SecretFingerprint *string
	KID               *string
	UserID            *int64
	FamilyID          *string
	Scope             []string
	OldStatus         *string
	NewStatus         *string
	Reason            *string
	RequestID         *string
}

type OIDCProviderRepository interface {
	GetClient(ctx context.Context, clientID string) (*OIDCClientRecord, error)
	GetClientByID(ctx context.Context, clientPK int64) (*OIDCClientRecord, error)
	ListClients(ctx context.Context) ([]OIDCClientRecord, error)
	CreateClient(ctx context.Context, input OIDCClientCreateInput) (*OIDCClientRecord, error)
	SetClientEnabled(ctx context.Context, clientPK, actorID int64, enabled bool, reason string) (*OIDCClientRecord, error)
	UpdateClient(ctx context.Context, input OIDCClientUpdateInput) (*OIDCClientRecord, error)
	CreateClientSecret(ctx context.Context, clientPK, actorID int64, digest, fingerprint string, notBefore, expiresAt time.Time, reason string) (*OIDCClientSecretRecord, error)
	RevokeClientSecret(ctx context.Context, clientPK, secretID, actorID int64, reason string) error
	AuthenticateClient(ctx context.Context, clientID, digest string, now time.Time) (*OIDCClientRecord, error)

	CreateBrowserSession(ctx context.Context, handleDigest string, userID int64, authTime time.Time, amr string, idleExpiresAt, absoluteExpiresAt time.Time) error
	GetBrowserSession(ctx context.Context, handleDigest string, now time.Time) (*OIDCBrowserSessionRecord, error)
	RevokeBrowserSession(ctx context.Context, handleDigest, reason string) error

	CreateAuthorizationTransaction(ctx context.Context, input OIDCTransactionCreateInput) (int64, error)
	GetAuthorizationTransaction(ctx context.Context, handleDigest string, now time.Time) (*OIDCTransactionRecord, error)
	SetTransactionAuthenticated(ctx context.Context, transactionID, sessionID, userID int64, authTime time.Time) error
	SetTransactionConsent(ctx context.Context, transactionID, consentID int64) error
	DenyTransaction(ctx context.Context, transactionID int64) error
	CreateAuthorizationCode(ctx context.Context, input OIDCAuthorizationCodeCreateInput) (string, error)
	GetAuthorizationCode(ctx context.Context, codeDigest string, now time.Time) (*OIDCAuthorizationCodeRecord, error)
	ExchangeAuthorizationCode(ctx context.Context, input OIDCAuthorizationCodeExchangeInput) (*OIDCUserRecord, error)

	GetConsent(ctx context.Context, userID, clientPK int64, scopeSetHash string, policyVersion int64) (*OIDCConsentRecord, error)
	CreateConsent(ctx context.Context, input OIDCConsentCreateInput) (int64, error)
	RevokeConsent(ctx context.Context, consentID, actorID int64, reason string) error
	ListConsents(ctx context.Context, input OIDCConsentListInput) ([]OIDCConsentRecord, error)
	GetConsentByID(ctx context.Context, consentID int64) (*OIDCConsentRecord, error)
	ListAuditEvents(ctx context.Context, input OIDCAuditListInput) ([]OIDCAuditEventRecord, error)

	GetUser(ctx context.Context, userID int64) (*OIDCUserRecord, error)
	GetAccessToken(ctx context.Context, tokenDigest string, now time.Time) (*OIDCUserRecord, string, int64, error)
	RotateRefreshToken(ctx context.Context, input OIDCRefreshRotationInput) (*OIDCRefreshRotationResult, error)
	RevokeToken(ctx context.Context, clientPK int64, tokenDigest, reason string) error

	ListSigningKeys(ctx context.Context, now time.Time) ([]OIDCSigningKeyRecord, error)
	ListAllSigningKeys(ctx context.Context) ([]OIDCSigningKeyRecord, error)
	GetActiveSigningKey(ctx context.Context, now time.Time) (*OIDCSigningKeyRecord, error)
	CreateSigningKey(ctx context.Context, input OIDCSigningKeyCreateInput) error
	SetSigningKeyStatus(ctx context.Context, kid, status string, actorID int64, reason string) error
}

type OIDCClientCreateInput struct {
	Name               string
	Owner              string
	ClientID           string
	SecretDigest       string
	SecretFingerprint  string
	RedirectURIs       []string
	AllowedScopes      []string
	TrustedSkipConsent bool
	Reason             string
	ActorID            int64
	Now                time.Time
	SecretNotBefore    time.Time
	SecretExpiresAt    time.Time
}

type OIDCClientUpdateInput struct {
	ClientPK           int64
	ExpectedVersion    int64
	Name               string
	Owner              string
	RedirectURIs       []string
	AllowedScopes      []string
	TrustedSkipConsent bool
	ActorID            int64
	Reason             string
}

type OIDCConsentListInput struct {
	Page     int
	PageSize int
	ClientID string
	Status   string
	Query    string
}

type OIDCAuditListInput struct {
	Page     int
	PageSize int
	Query    string
}

type OIDCTransactionCreateInput struct {
	HandleDigest        string
	ClientPK            int64
	ConsentID           *int64
	RedirectURI         string
	ScopeSnapshot       string
	StateCiphertext     string
	StateFingerprint    string
	NonceCiphertext     string
	NonceFingerprint    string
	CodeChallenge       string
	CodeChallengeMethod string
	Prompt              string
	MaxAgeSeconds       *int64
	Display             *string
	ExpiresAt           time.Time
}

type OIDCAuthorizationCodeCreateInput struct {
	TransactionID           int64
	TransactionHandleDigest string
	CodeDigest              string
	ClientPK                int64
	UserID                  int64
	ConsentID               int64
	BrowserSessionID        *int64
	RedirectURI             string
	ScopeSnapshot           string
	NonceCiphertext         string
	NonceFingerprint        string
	CodeChallenge           string
	CodeChallengeMethod     string
	AuthTime                time.Time
	IssuedAt                time.Time
	ExpiresAt               time.Time
}

type OIDCAuthorizationCodeExchangeInput struct {
	CodeDigest           string
	ClientPK             int64
	RedirectURI          string
	ConsentID            int64
	CodeVerifier         string
	RawAccessToken       string
	RawRefreshToken      string
	FamilyID             string
	AccessIssuedAt       time.Time
	AccessExpiresAt      time.Time
	RefreshIssuedAt      time.Time
	RefreshIdleUntil     time.Time
	RefreshAbsoluteUntil time.Time
}

type OIDCConsentCreateInput struct {
	UserID        int64
	ClientPK      int64
	ScopeSnapshot string
	ScopeSetHash  string
	PolicyVersion int64
	Source        string
	ActorID       *int64
}

type OIDCRefreshRotationInput struct {
	ClientPK             int64
	TokenDigest          string
	NewTokenDigest       string
	NewAccessTokenDigest string
	FamilyID             string
	Now                  time.Time
	NewRefreshIssuedAt   time.Time
	NewRefreshExpiresAt  time.Time
	NewAccessIssuedAt    time.Time
	NewAccessExpiresAt   time.Time
	RequestedScope       string
}

type OIDCRefreshRotationResult struct {
	User     *OIDCUserRecord
	Scope    string
	FamilyID string
	Replayed bool
}

type OIDCSigningKeyCreateInput struct {
	KID                  string
	Alg                  string
	PublicJWK            string
	PrivateKeyCiphertext string
	Fingerprint          string
	Status               string
	NotBefore            time.Time
	NotAfter             time.Time
	CreatedBy            int64
	ChangeReason         string
}

type OIDCSigningKeyMaterial struct {
	Record     OIDCSigningKeyRecord
	PrivateKey *rsa.PrivateKey
}
