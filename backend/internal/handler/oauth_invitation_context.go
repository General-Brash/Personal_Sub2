package handler

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const oauthInvitationCookie = "personal_oauth_invitation"
const oauthInvitationContextKey = "validated_oauth_invitation"
const oauthInvitationClaim = "personal_registration_invitation"
const oauthAffiliateClaim = "personal_registration_affiliate"

type signedOAuthInvitation struct {
	State      string `json:"state"`
	Invitation string `json:"invitation"`
	Affiliate  string `json:"affiliate"`
	Expires    int64  `json:"expires"`
}

func encodeOAuthInvitation(value signedOAuthInvitation, secret string) (string, error) {
	if len(secret) < 16 {
		return "", errors.New("OAuth invitation signing is not configured")
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte("oauth-registration-v1:" + payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}
func decodeOAuthInvitation(raw, state, secret string, now time.Time) (signedOAuthInvitation, error) {
	var value signedOAuthInvitation
	parts := strings.Split(raw, ".")
	if len(parts) != 2 || len(raw) > 4096 || len(secret) < 16 {
		return value, service.ErrPlayerInvitationInvalid
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte("oauth-registration-v1:" + parts[0]))
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(signature, mac.Sum(nil)) {
		return value, service.ErrPlayerInvitationInvalid
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || json.Unmarshal(payload, &value) != nil {
		return value, service.ErrPlayerInvitationInvalid
	}
	if state == "" || value.State != state || value.Expires <= now.Unix() || value.Expires > now.Add(10*time.Minute).Unix() {
		return value, service.ErrPlayerInvitationInvalid
	}
	return value, nil
}
func (h *AuthHandler) respondOAuthStartWithInvitation(c *gin.Context, authorizeURL string) {
	invite := strings.TrimSpace(firstNonEmpty(c.Query("invite"), c.Query("invitation_code")))
	affiliate := strings.TrimSpace(firstNonEmpty(c.Query("aff"), c.Query("aff_code")))
	if len(invite) > 512 || len(affiliate) > 128 {
		response.BadRequest(c, "Invalid registration invitation")
		return
	}
	if invite == "" && affiliate == "" {
		c.SetCookie(oauthInvitationCookie, "", -1, "/api/v1/auth", "", isRequestHTTPS(c), true)
		respondOAuthStart(c, authorizeURL)
		return
	}
	u, err := url.Parse(authorizeURL)
	if err != nil || u.Query().Get("state") == "" || h.cfg == nil {
		response.BadRequest(c, "OAuth state is unavailable")
		return
	}
	cookie, err := encodeOAuthInvitation(signedOAuthInvitation{State: u.Query().Get("state"), Invitation: invite, Affiliate: affiliate, Expires: time.Now().Add(10 * time.Minute).Unix()}, h.cfg.JWT.Secret)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(oauthInvitationCookie, cookie, 600, "/api/v1/auth", "", isRequestHTTPS(c), true)
	respondOAuthStart(c, authorizeURL)
}
func (h *AuthHandler) restoreOAuthInvitation(c *gin.Context) bool {
	raw, err := c.Cookie(oauthInvitationCookie)
	if errors.Is(err, http.ErrNoCookie) {
		return true
	}
	if err != nil {
		return false
	}
	c.SetCookie(oauthInvitationCookie, "", -1, "/api/v1/auth", "", isRequestHTTPS(c), true)
	if h.cfg == nil {
		response.BadRequest(c, "Invalid OAuth invitation context")
		return false
	}
	value, err := decodeOAuthInvitation(raw, c.Query("state"), h.cfg.JWT.Secret, time.Now())
	if err != nil {
		response.BadRequest(c, "Invalid or expired OAuth invitation context")
		return false
	}
	c.Set(oauthInvitationContextKey, value)
	return true
}
func validatedOAuthInvitation(c *gin.Context) signedOAuthInvitation {
	if c == nil {
		return signedOAuthInvitation{}
	}
	value, _ := c.Get(oauthInvitationContextKey)
	invitation, _ := value.(signedOAuthInvitation)
	return invitation
}

const oauthEncryptedInvitationClaim = "personal_registration_context_v1"
const legacyPendingOAuthAffiliateClaim = "aff_code"

func oauthInvitationAEAD(secret string) (cipher.AEAD, error) {
	if len(secret) < 16 {
		return nil, service.ErrServiceUnavailable
	}
	key := sha256.Sum256([]byte("pending-invitation-v1:" + secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
func (h *AuthHandler) protectPendingInvitation(value signedOAuthInvitation) (string, error) {
	if h.cfg == nil {
		return "", service.ErrServiceUnavailable
	}
	aead, err := oauthInvitationAEAD(h.cfg.JWT.Secret)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ciphertext := aead.Seal(nonce, nonce, raw, []byte(oauthEncryptedInvitationClaim))
	return base64.RawURLEncoding.EncodeToString(ciphertext), nil
}
func (h *AuthHandler) mergePendingInvitationClaims(claims map[string]any, invite, affiliate *string) error {
	protected, _ := claims[oauthEncryptedInvitationClaim].(string)
	if protected == "" {
		return nil
	}
	if h.cfg == nil {
		return service.ErrServiceUnavailable
	}
	aead, err := oauthInvitationAEAD(h.cfg.JWT.Secret)
	if err != nil {
		return err
	}
	raw, err := base64.RawURLEncoding.DecodeString(protected)
	if err != nil || len(raw) < aead.NonceSize() {
		return service.ErrPlayerInvitationInvalid
	}
	clear, err := aead.Open(nil, raw[:aead.NonceSize()], raw[aead.NonceSize():], []byte(oauthEncryptedInvitationClaim))
	if err != nil {
		return service.ErrPlayerInvitationInvalid
	}
	var fixed signedOAuthInvitation
	if json.Unmarshal(clear, &fixed) != nil {
		return service.ErrPlayerInvitationInvalid
	}
	// Pending session expiry is separately enforced by the browser-session
	// service. Credential expiry and one-shot use are rechecked by claim.
	for _, field := range []struct {
		value  string
		target *string
	}{{fixed.Invitation, invite}, {fixed.Affiliate, affiliate}} {
		if field.value == "" {
			continue
		}
		if value := strings.TrimSpace(*field.target); value != "" && value != field.value {
			return service.ErrPlayerInvitationSourceConflict
		}
		*field.target = field.value
	}
	return nil
}
