package account

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type TokenManager struct {
	secret []byte
	ttl    time.Duration
}

// TokenClaims is intentionally small because the database is rechecked on every
// authenticated request. Disabling a user therefore takes effect immediately
// without waiting for the token to expire.
type TokenClaims struct {
	UserID    int64  `json:"userID"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	SessionID string `json:"sessionID"`
	Expires   int64  `json:"expires"`
}

func NewTokenManager(secret string, ttl time.Duration) *TokenManager {
	return &TokenManager{secret: []byte(secret), ttl: ttl}
}

// Sign creates a compact HMAC token for the prototype API. It is JWT-like but
// deliberately local and minimal, so later OAuth/JWT integration can replace
// only this manager.
func (m *TokenManager) Sign(u User, sessionID string) (string, error) {
	claims := TokenClaims{
		UserID:    u.ID,
		Username:  u.Username,
		Role:      u.Role,
		SessionID: sessionID,
		Expires:   time.Now().Add(m.ttl).Unix(),
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payloadPart := base64.RawURLEncoding.EncodeToString(payload)
	sig := m.sign(payloadPart)
	return payloadPart + "." + sig, nil
}

func (m *TokenManager) Verify(token string) (TokenClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return TokenClaims{}, errors.New("bad token format")
	}
	if !hmac.Equal([]byte(m.sign(parts[0])), []byte(parts[1])) {
		return TokenClaims{}, errors.New("bad token signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return TokenClaims{}, err
	}
	var claims TokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return TokenClaims{}, err
	}
	if claims.Expires <= time.Now().Unix() {
		return TokenClaims{}, errors.New("token expired")
	}
	if claims.UserID <= 0 {
		return TokenClaims{}, fmt.Errorf("bad user id %s", strconv.FormatInt(claims.UserID, 10))
	}
	if strings.TrimSpace(claims.SessionID) == "" {
		return TokenClaims{}, errors.New("missing session id")
	}
	return claims, nil
}

func (m *TokenManager) sign(payloadPart string) string {
	mac := hmac.New(sha256.New, m.secret)
	_, _ = mac.Write([]byte(payloadPart))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
