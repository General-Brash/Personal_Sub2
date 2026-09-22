//go:build integration

package handler

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	repositorypkg "github.com/Wei-Shaw/sub2api/internal/repository"
	validation "github.com/Wei-Shaw/sub2api/internal/repository/validation"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/testutil/integrationenv"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// TestOIDCProviderPositiveFlowPostgres runs the complete local-provider path
// against the isolated PostgreSQL target owned by the integration harness. The
// signing key and confidential-client secret are both created by application
// services; no OIDC credential is manually seeded by the fixture.
func TestOIDCProviderPositiveFlowPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg := oidcProviderIntegrationConfig()
	db, entClient := openOIDCIntegrationTarget(t, ctx)
	oidcRepo := repositorypkg.NewOIDCProviderRepository(db)
	userRepo := repositorypkg.NewUserRepository(entClient, db)
	signing := service.NewOIDCSigningService(oidcRepo, cfg)
	oidcService := service.NewOIDCProviderService(oidcRepo, userRepo, nil, signing, cfg, nil)
	oidcHandler := NewOIDCProviderHandler(oidcService, cfg)

	userEmail := "oidc-flow-" + time.Now().UTC().Format("20060102150405.000000000") + "@example.com"
	userRow, err := entClient.User.Create().
		SetEmail(userEmail).
		SetUsername("oidc-flow-user").
		SetPasswordHash("test-password-hash").
		SetRole(service.RoleUser).
		SetStatus(service.StatusActive).
		SetConcurrency(5).
		Save(ctx)
	require.NoError(t, err)
	user := &service.User{ID: userRow.ID, Email: userRow.Email, Username: userRow.Username, Role: userRow.Role, Status: userRow.Status}

	key, err := oidcService.AdminRotateKey(ctx, user.ID, "integration-positive-flow")
	require.NoError(t, err)
	require.Equal(t, "active", key.Status)
	require.Equal(t, service.OIDCSigningRS256, key.Alg)

	redirectURI := "https://client.example.test/callback"
	client, clientSecret, err := oidcService.AdminCreateClient(ctx, service.OIDCClientCreateInput{
		Name:               "OIDC integration positive flow",
		Owner:              "integration-test",
		RedirectURIs:       []string{redirectURI},
		AllowedScopes:      []string{service.OIDCScopeOpenID, service.OIDCScopeProfile, service.OIDCScopeEmail, service.OIDCScopeRoles, service.OIDCScopeOfflineAccess},
		TrustedSkipConsent: true,
		ActorID:            user.ID,
		Reason:             "integration-positive-flow",
	})
	require.NoError(t, err)
	require.NotEmpty(t, client.ClientID)
	require.True(t, strings.HasPrefix(client.ClientID, "sub2-"))
	require.NotEmpty(t, clientSecret, "the application must return the one-time generated secret")
	storedClient, err := oidcRepo.GetClientByID(ctx, client.ID)
	require.NoError(t, err)
	require.Len(t, storedClient.Secrets, 1)
	require.NotEqual(t, clientSecret, storedClient.Secrets[0].Fingerprint)
	require.Equal(t, sha256Hex(clientSecret), storedClient.Secrets[0].Fingerprint)

	t.Cleanup(func() {
		cleanupOIDCPositiveFlow(t, db, ctx, client.ID, user.ID, key.KID)
		_ = entClient.User.DeleteOneID(user.ID).Exec(context.Background())
	})

	sessionHandle := "session-" + randomOpaque(t, 24)
	now := time.Now().UTC()
	require.NoError(t, oidcRepo.CreateBrowserSession(ctx, oidcDigestForTest(sessionHandle), user.ID, now, service.OIDCSessionAMRPassword, now.Add(time.Hour), now.Add(2*time.Hour)))

	verifier := randomOpaque(t, 32)
	challenge := pkceS256(verifier)
	state := "state-" + randomOpaque(t, 12)
	nonce := "nonce-" + randomOpaque(t, 12)

	router := gin.New()
	router.GET("/oauth/authorize", oidcHandler.Authorize)
	router.POST("/oauth/token", oidcHandler.Token)
	router.GET("/oauth/userinfo", oidcHandler.UserInfo)
	router.POST("/oauth/revoke", oidcHandler.Revoke)
	router.GET("/oauth/jwks", oidcHandler.JWKS)

	authorizeQuery := url.Values{
		"client_id":             {client.ClientID},
		"redirect_uri":          {redirectURI},
		"response_type":         {"code"},
		"scope":                 {"openid profile email roles offline_access"},
		"state":                 {state},
		"nonce":                 {nonce},
		"code_challenge":        {challenge},
		"code_challenge_method": {service.OIDCCodeChallengeS256},
	}
	authorizeReq := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+authorizeQuery.Encode(), nil)
	authorizeReq.AddCookie(&http.Cookie{Name: cfg.OIDCProvider.Cookie.SessionName, Value: sessionHandle})
	authorizeRR := httptest.NewRecorder()
	router.ServeHTTP(authorizeRR, authorizeReq)
	require.Equal(t, http.StatusFound, authorizeRR.Code)
	authorizeLocation := mustLocation(t, authorizeRR)
	require.Equal(t, redirectURI, authorizeLocation.Scheme+"://"+authorizeLocation.Host+authorizeLocation.Path)
	require.Equal(t, state, authorizeLocation.Query().Get("state"))
	require.Equal(t, service.OIDCProviderIssuer, authorizeLocation.Query().Get("iss"))
	code := authorizeLocation.Query().Get("code")
	require.NotEmpty(t, code)

	tokenResponse := postOIDCForm(t, router, "/oauth/token", client.ClientID, clientSecret, url.Values{
		"grant_type":    {service.OIDCGrantAuthorizationCode},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	})
	require.Equal(t, http.StatusOK, tokenResponse.status)
	var tokenBody struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		Scope        string `json:"scope"`
	}
	require.NoError(t, json.Unmarshal(tokenResponse.body, &tokenBody))
	require.NotEmpty(t, tokenBody.AccessToken)
	require.NotEmpty(t, tokenBody.RefreshToken)
	require.NotEmpty(t, tokenBody.IDToken)
	require.Contains(t, tokenBody.Scope, service.OIDCScopeOfflineAccess)

	jwksResponse := getOIDC(t, router, "/oauth/jwks", "")
	require.Equal(t, http.StatusOK, jwksResponse.status)
	var jwks struct {
		Keys []struct {
			KID string `json:"kid"`
			Alg string `json:"alg"`
			Kty string `json:"kty"`
			Use string `json:"use"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	require.NoError(t, json.Unmarshal(jwksResponse.body, &jwks))
	require.Len(t, jwks.Keys, 1)
	require.Equal(t, key.KID, jwks.Keys[0].KID)
	require.Equal(t, service.OIDCSigningRS256, jwks.Keys[0].Alg)
	require.Equal(t, "RSA", jwks.Keys[0].Kty)
	require.Equal(t, "sig", jwks.Keys[0].Use)
	verifyOIDCIDToken(t, tokenBody.IDToken, jwks.Keys[0].KID, jwks.Keys[0].N, jwks.Keys[0].E, client.ClientID)

	userinfoResponse := getOIDC(t, router, "/oauth/userinfo", tokenBody.AccessToken)
	require.Equal(t, http.StatusOK, userinfoResponse.status)
	var userinfo map[string]any
	require.NoError(t, json.Unmarshal(userinfoResponse.body, &userinfo))
	require.Equal(t, user.Email, userinfo["email"])
	require.Equal(t, user.Username, userinfo["preferred_username"])
	require.Equal(t, "user", userinfo["role"])
	require.NotContains(t, userinfo, "balance")
	require.NotContains(t, userinfo, "api_key")

	rotatedResponse := postOIDCForm(t, router, "/oauth/token", client.ClientID, clientSecret, url.Values{
		"grant_type":    {service.OIDCGrantRefreshToken},
		"refresh_token": {tokenBody.RefreshToken},
	})
	require.Equal(t, http.StatusOK, rotatedResponse.status)
	var rotatedBody struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	require.NoError(t, json.Unmarshal(rotatedResponse.body, &rotatedBody))
	require.NotEmpty(t, rotatedBody.AccessToken)
	require.NotEmpty(t, rotatedBody.RefreshToken)
	require.NotEqual(t, tokenBody.RefreshToken, rotatedBody.RefreshToken)

	replayResponse := postOIDCForm(t, router, "/oauth/token", client.ClientID, clientSecret, url.Values{
		"grant_type":    {service.OIDCGrantRefreshToken},
		"refresh_token": {tokenBody.RefreshToken},
	})
	require.Equal(t, http.StatusBadRequest, replayResponse.status)
	require.Contains(t, string(replayResponse.body), `"error":"invalid_grant"`)

	revokeResponse := postOIDCForm(t, router, "/oauth/revoke", client.ClientID, clientSecret, url.Values{
		"token": {rotatedBody.AccessToken},
	})
	require.Equal(t, http.StatusOK, revokeResponse.status)
	revokedUserinfo := getOIDC(t, router, "/oauth/userinfo", rotatedBody.AccessToken)
	require.Equal(t, http.StatusUnauthorized, revokedUserinfo.status)
}

type oidcHTTPResponse struct {
	status int
	body   []byte
}

func oidcProviderIntegrationConfig() *config.Config {
	return &config.Config{OIDCProvider: config.OIDCProviderConfig{
		Enabled:                          true,
		Issuer:                           service.OIDCProviderIssuer,
		PublicHost:                       "auth.taffy.edu.kg",
		SigningAlg:                       service.OIDCSigningRS256,
		SigningKeySource:                 "database_encrypted",
		EncryptionKey:                    "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff",
		SecretPepper:                     "integration-secret-pepper-0123456789abcdef",
		TransactionTTLSeconds:            300,
		BrowserSessionIdleTTLSeconds:     1800,
		BrowserSessionAbsoluteTTLSeconds: 3600,
		AuthorizationCodeTTLSeconds:      120,
		AccessTokenTTLSeconds:            300,
		IDTokenTTLSeconds:                300,
		RefreshTokenIdleTTLSeconds:       3600,
		RefreshTokenAbsoluteTTLSeconds:   7200,
		ClockSkewSeconds:                 60,
		JWKSCacheMaxAgeSeconds:           300,
		ClientSecretMaxOverlapSeconds:    3600,
		RequirePKCES256:                  true,
		AuthorizationResponseIssuer:      true,
		AllowedScopes:                    []string{service.OIDCScopeOpenID, service.OIDCScopeProfile, service.OIDCScopeEmail, service.OIDCScopeRoles, service.OIDCScopeOfflineAccess},
		Cookie: config.OIDCProviderCookieConfig{
			SessionName:     "__Host-sub2_oidc_session",
			TransactionName: "__Host-sub2_oidc_tx",
			Secure:          true,
			HTTPOnly:        true,
			SameSite:        "lax",
		},
	}}
}

func postOIDCForm(t *testing.T, router http.Handler, path, clientID, clientSecret string, form url.Values) oidcHTTPResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(clientID, clientSecret)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	return oidcHTTPResponse{status: rr.Code, body: rr.Body.Bytes()}
}

func getOIDC(t *testing.T, router http.Handler, path, accessToken string) oidcHTTPResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	return oidcHTTPResponse{status: rr.Code, body: rr.Body.Bytes()}
}

func mustLocation(t *testing.T, rr *httptest.ResponseRecorder) *url.URL {
	t.Helper()
	location, err := url.Parse(rr.Header().Get("Location"))
	require.NoError(t, err)
	return location
}

func randomOpaque(t *testing.T, size int) string {
	t.Helper()
	buf := make([]byte, size)
	_, err := rand.Read(buf)
	require.NoError(t, err)
	return base64.RawURLEncoding.EncodeToString(buf)
}

func pkceS256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func oidcDigestForTest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func verifyOIDCIDToken(t *testing.T, raw, expectedKID, n, e, clientID string) {
	t.Helper()
	modulus, err := base64.RawURLEncoding.DecodeString(n)
	require.NoError(t, err)
	exponentBytes, err := base64.RawURLEncoding.DecodeString(e)
	require.NoError(t, err)
	exponent := int(new(big.Int).SetBytes(exponentBytes).Int64())
	key := &rsa.PublicKey{N: new(big.Int).SetBytes(modulus), E: exponent}
	parsed, err := jwt.Parse(raw, func(token *jwt.Token) (any, error) {
		require.Equal(t, service.OIDCSigningRS256, token.Method.Alg())
		require.Equal(t, expectedKID, token.Header["kid"])
		return key, nil
	}, jwt.WithAudience(clientID), jwt.WithIssuer(service.OIDCProviderIssuer))
	require.NoError(t, err)
	require.True(t, parsed.Valid)
}

func cleanupOIDCPositiveFlow(t *testing.T, db *sql.DB, ctx context.Context, clientID, userID int64, kid string) {
	t.Helper()
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cleanupStatements := []struct {
		query string
		args  []any
	}{
		{`UPDATE oidc_refresh_tokens SET parent_token_id = NULL, child_token_id = NULL WHERE family_id IN (SELECT family_id FROM oidc_refresh_token_families WHERE client_pk = $1 OR user_id = $2)`, []any{clientID, userID}},
		{`DELETE FROM oidc_refresh_tokens WHERE family_id IN (SELECT family_id FROM oidc_refresh_token_families WHERE client_pk = $1 OR user_id = $2)`, []any{clientID, userID}},
		{`DELETE FROM oidc_access_tokens WHERE client_pk = $1 OR user_id = $2`, []any{clientID, userID}},
		{`DELETE FROM oidc_authorization_codes WHERE client_pk = $1 OR user_id = $2`, []any{clientID, userID}},
		{`DELETE FROM oidc_authorization_transactions WHERE client_pk = $1`, []any{clientID}},
		{`DELETE FROM oidc_refresh_token_families WHERE client_pk = $1 OR user_id = $2`, []any{clientID, userID}},
		{`DELETE FROM oidc_consents WHERE client_pk = $1 OR user_id = $2`, []any{clientID, userID}},
		{`DELETE FROM oidc_browser_sessions WHERE user_id = $1`, []any{userID}},
		{`DELETE FROM oidc_client_redirect_uris WHERE client_pk = $1`, []any{clientID}},
		{`DELETE FROM oidc_client_scopes WHERE client_pk = $1`, []any{clientID}},
		{`DELETE FROM oidc_client_secrets WHERE client_pk = $1`, []any{clientID}},
		{`DELETE FROM oidc_clients WHERE id = $1`, []any{clientID}},
		{`UPDATE oidc_signing_keys SET status = 'retired' WHERE kid = $1`, []any{kid}},
		{`DELETE FROM oidc_signing_keys WHERE kid = $1`, []any{kid}},
	}
	for _, statement := range cleanupStatements {
		if _, err := db.ExecContext(cleanupCtx, statement.query, statement.args...); err != nil {
			t.Logf("OIDC integration cleanup statement failed: %v", err)
		}
	}
}

func openOIDCIntegrationTarget(t *testing.T, ctx context.Context) (*sql.DB, *dbent.Client) {
	t.Helper()
	cfg, cleanup, err := integrationenv.Load(ctx, false)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })
	dsn := validation.DatabaseDSN(cfg)
	require.NotEmpty(t, dsn)
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, db.PingContext(ctx))
	if cfg.AllowAutoMigrate {
		require.NoError(t, repositorypkg.ApplyMigrations(ctx, db))
	}
	driver := entsql.OpenDB(dialect.Postgres, db)
	entClient := dbent.NewClient(dbent.Driver(driver))
	t.Cleanup(func() { require.NoError(t, entClient.Close()) })
	return db, entClient
}
