package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	"aiops-platform/backend/internal/model"
	"github.com/golang-jwt/jwt/v5"
)

const tokenIssuer = "aiops-platform"

var ErrInvalidToken = errors.New("invalid token")

type Claims struct {
	UserID           int64  `json:"userId"`
	Username         string `json:"username"`
	Role             string `json:"role"`
	IssuedAtUnixNano int64  `json:"issuedAtUnixNano"`
	jwt.RegisteredClaims
}

type IssuedToken struct {
	Value     string
	ExpiresAt time.Time
}

type TokenManager struct {
	secret []byte
	expiry time.Duration
	now    func() time.Time
}

func NewTokenManager(secret string, expiry time.Duration) (*TokenManager, error) {
	if len(secret) < 32 {
		return nil, fmt.Errorf("JWT secret must contain at least 32 characters")
	}
	if expiry <= 0 {
		return nil, fmt.Errorf("JWT expiry must be positive")
	}
	return &TokenManager{secret: []byte(secret), expiry: expiry, now: time.Now}, nil
}

func (m *TokenManager) Issue(user *model.AppUser) (IssuedToken, error) {
	now := m.now().UTC()
	expiresAt := now.Add(m.expiry)
	claims := Claims{
		UserID:           user.ID,
		Username:         user.Username,
		Role:             user.Role,
		IssuedAtUnixNano: now.UnixNano(),
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    tokenIssuer,
			Subject:   strconv.FormatInt(user.ID, 10),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ID:        tokenID(),
		},
	}

	value, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
	if err != nil {
		return IssuedToken{}, fmt.Errorf("sign JWT: %w", err)
	}
	return IssuedToken{Value: value, ExpiresAt: expiresAt}, nil
}

func (m *TokenManager) Parse(value string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(
		value,
		claims,
		func(token *jwt.Token) (any, error) {
			if token.Method != jwt.SigningMethodHS256 {
				return nil, ErrInvalidToken
			}
			return m.secret, nil
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(tokenIssuer),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithTimeFunc(func() time.Time { return m.now().UTC() }),
	)
	if err != nil || !token.Valid || claims.UserID <= 0 || claims.IssuedAtUnixNano <= 0 ||
		claims.Subject != strconv.FormatInt(claims.UserID, 10) {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

func tokenID() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(value)
}
