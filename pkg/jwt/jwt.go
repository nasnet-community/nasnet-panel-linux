package jwt

import (
	"context"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var (
	ErrInvalidToken     = errors.New("invalid token")
	ErrExpiredToken     = errors.New("token has expired")
	ErrInvalidClaims    = errors.New("invalid token claims")
	ErrTokenNotProvided = errors.New("token not provided")
)

// Claims represents the JWT claims
type Claims struct {
	UserID     uint   `json:"user_id"`
	TelegramID int64  `json:"telegram_id"`
	Username   string `json:"username"`
	IsAdmin    bool   `json:"is_admin"`
	TokenType  string `json:"token_type"` // "access" or "refresh"
	RememberMe bool   `json:"remember_me"`
	jwt.RegisteredClaims
}

// Config holds JWT configuration
type Config struct {
	SecretKey          string
	AccessTokenExpiry  time.Duration
	RefreshTokenExpiry time.Duration
	Issuer             string
	CookieDomain       string
	CookieSecure       bool // Should be true in production (HTTPS)
}

// Manager handles JWT operations
type Manager struct {
	config    Config
	blacklist *Blacklist
}

// SetBlacklist attaches a token blacklist to the manager.
func (m *Manager) SetBlacklist(bl *Blacklist) {
	m.blacklist = bl
}

// NewManager creates a new JWT manager
func NewManager(config Config) *Manager {
	if config.AccessTokenExpiry == 0 {
		config.AccessTokenExpiry = 15 * time.Minute
	}
	if config.RefreshTokenExpiry == 0 {
		config.RefreshTokenExpiry = 7 * 24 * time.Hour // 7 days
	}
	if config.Issuer == "" {
		config.Issuer = "nasnet-panel"
	}
	return &Manager{config: config}
}

// GetSecretKey returns the raw signing secret for use by other subsystems (e.g. HMAC tokens).
func (m *Manager) GetSecretKey() string {
	return m.config.SecretKey
}

// TokenPair represents access and refresh tokens
type TokenPair struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// GenerateTokenPair creates both access and refresh tokens using the manager's configured expiry.
func (m *Manager) GenerateTokenPair(userID uint, telegramID int64, username string, isAdmin bool, rememberMe bool) (*TokenPair, error) {
	return m.GenerateTokenPairWithExpiry(userID, telegramID, username, isAdmin, rememberMe, m.config.AccessTokenExpiry, m.config.RefreshTokenExpiry)
}

// GenerateTokenPairWithExpiry creates both access and refresh tokens with explicit expiry durations.
// This allows callers to pass dynamic values (e.g. from DB settings) so that the JWT claim
// expiration matches the cookie MaxAge exactly.
func (m *Manager) GenerateTokenPairWithExpiry(userID uint, telegramID int64, username string, isAdmin bool, rememberMe bool, accessDuration, refreshDuration time.Duration) (*TokenPair, error) {
	now := time.Now()

	// Generate access token
	accessExpiry := now.Add(accessDuration)
	accessClaims := Claims{
		UserID:     userID,
		TelegramID: telegramID,
		Username:   username,
		IsAdmin:    isAdmin,
		TokenType:  "access",
		RememberMe: rememberMe,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.New().String(),
			ExpiresAt: jwt.NewNumericDate(accessExpiry),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Issuer:    m.config.Issuer,
			Subject:   username,
		},
	}

	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	accessTokenString, err := accessToken.SignedString([]byte(m.config.SecretKey))
	if err != nil {
		return nil, err
	}

	// Generate refresh token
	refreshExpiry := now.Add(refreshDuration)
	refreshClaims := Claims{
		UserID:     userID,
		TelegramID: telegramID,
		Username:   username,
		IsAdmin:    isAdmin,
		TokenType:  "refresh",
		RememberMe: rememberMe,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.New().String(),
			ExpiresAt: jwt.NewNumericDate(refreshExpiry),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Issuer:    m.config.Issuer,
			Subject:   username,
		},
	}

	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
	refreshTokenString, err := refreshToken.SignedString([]byte(m.config.SecretKey))
	if err != nil {
		return nil, err
	}

	return &TokenPair{
		AccessToken:  accessTokenString,
		RefreshToken: refreshTokenString,
		ExpiresAt:    accessExpiry,
	}, nil
}

// ValidateToken validates a JWT token and returns the claims
func (m *Manager) ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return []byte(m.config.SecretKey), nil
	})

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrExpiredToken
		}
		return nil, ErrInvalidToken
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrInvalidClaims
	}

	// Check token revocation
	if m.blacklist != nil && claims.ID != "" {
		if m.blacklist.IsRevoked(context.Background(), claims.ID) {
			return nil, ErrInvalidToken
		}
	}

	return claims, nil
}

// RevokeToken adds a token to the blacklist by its JTI.
func (m *Manager) RevokeToken(ctx context.Context, tokenID string, expiresAt time.Time) error {
	if m.blacklist == nil {
		return nil
	}
	return m.blacklist.Revoke(ctx, tokenID, expiresAt)
}

// ValidateAccessToken validates an access token specifically
func (m *Manager) ValidateAccessToken(tokenString string) (*Claims, error) {
	claims, err := m.ValidateToken(tokenString)
	if err != nil {
		return nil, err
	}

	if claims.TokenType != "access" {
		return nil, ErrInvalidToken
	}

	return claims, nil
}

// ValidateRefreshToken validates a refresh token specifically
func (m *Manager) ValidateRefreshToken(tokenString string) (*Claims, error) {
	claims, err := m.ValidateToken(tokenString)
	if err != nil {
		return nil, err
	}

	if claims.TokenType != "refresh" {
		return nil, ErrInvalidToken
	}

	return claims, nil
}

// GetConfig returns the JWT configuration
func (m *Manager) GetConfig() Config {
	return m.config
}
