// internal/auth/jwt.go
// JWT generation and validation for the CMP-Core API.
//
// Claims carry user_id, organization_id, and role so downstream handlers
// never need a DB round-trip just to identify the caller.

package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Claims is the payload embedded in every issued JWT.
type Claims struct {
	UserID         uuid.UUID `json:"user_id"`
	OrganizationID uuid.UUID `json:"organization_id"`
	Role           string    `json:"role"`
	jwt.RegisteredClaims
}

// Manager issues and validates JWTs.
type Manager struct {
	secret []byte
	expiry time.Duration
}

// NewManager constructs a Manager.
//
//	secret      — signing key (read from JWT_SECRET env var in main.go)
//	expiryHours — token lifetime; use JWT_EXPIRY_HOURS env var (default 24)
func NewManager(secret string, expiryHours int) *Manager {
	if expiryHours <= 0 {
		expiryHours = 24
	}
	return &Manager{
		secret: []byte(secret),
		expiry: time.Duration(expiryHours) * time.Hour,
	}
}

// Generate creates a signed HS256 JWT for the given user.
func (m *Manager) Generate(userID, orgID uuid.UUID, role string) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:         userID,
		OrganizationID: orgID,
		Role:           role,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.expiry)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(m.secret)
	if err != nil {
		return "", fmt.Errorf("jwt: sign: %w", err)
	}
	return signed, nil
}

// Validate parses and verifies a JWT string, returning the embedded Claims.
func (m *Manager) Validate(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("jwt: unexpected signing method: %v", t.Header["alg"])
		}
		return m.secret, nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, fmt.Errorf("jwt: token expired")
		}
		return nil, fmt.Errorf("jwt: invalid token: %w", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("jwt: malformed claims")
	}
	return claims, nil
}
