package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type DeploymentClaims struct {
	NodeID uint   `json:"node_id"`
	Type   string `json:"type"` // "deployment"
	jwt.RegisteredClaims
}

type TokenManager struct {
	secretKey string
}

func NewTokenManager(secretKey string) *TokenManager {
	return &TokenManager{secretKey: secretKey}
}

func (m *TokenManager) GenerateDeploymentToken(nodeID uint, duration time.Duration) (string, error) {
	claims := DeploymentClaims{
		NodeID: nodeID,
		Type:   "deployment",
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.New().String(),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(duration)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(m.secretKey))
}

func (m *TokenManager) ValidateDeploymentToken(tokenString string) (*DeploymentClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &DeploymentClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(m.secretKey), nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*DeploymentClaims); ok && token.Valid {
		if claims.Type != "deployment" {
			return nil, errors.New("invalid token type")
		}
		return claims, nil
	}

	return nil, errors.New("invalid token")
}
