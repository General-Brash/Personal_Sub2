package service

import (
	"fmt"
	"strings"
	"time"
)

type OIDCClaimsSubject struct {
	Subject  string
	Email    string
	Username string
	Role     string
	AuthTime time.Time
	AMR      string
}

func MapOIDCRole(role string) (string, error) {
	switch strings.TrimSpace(role) {
	case RoleUser:
		return "user", nil
	case RoleAdmin:
		return "admin", nil
	case RoleSuperAdmin:
		return "superadmin", nil
	default:
		return "", fmt.Errorf("unknown oidc role %q", role)
	}
}

func UserInfoClaims(subject OIDCClaimsSubject, scope string) (map[string]any, error) {
	claims := map[string]any{"sub": subject.Subject}
	scopes := scopeSet(scope)
	if _, ok := scopes[OIDCScopeProfile]; ok && strings.TrimSpace(subject.Username) != "" {
		claims["preferred_username"] = subject.Username
	}
	if _, ok := scopes[OIDCScopeEmail]; ok && strings.TrimSpace(subject.Email) != "" {
		claims["email"] = subject.Email
	}
	if _, ok := scopes[OIDCScopeRoles]; ok {
		role, err := MapOIDCRole(subject.Role)
		if err != nil {
			return nil, err
		}
		claims["role"] = role
	}
	return claims, nil
}

func scopeSet(scope string) map[string]struct{} {
	result := make(map[string]struct{})
	for _, item := range strings.Fields(scope) {
		result[item] = struct{}{}
	}
	return result
}
