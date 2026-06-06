package account

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrNotFound     = errors.New("account not found")
	ErrUnauthorized = errors.New("invalid username or password")
	ErrDisabled     = errors.New("account is disabled")
	ErrSessionStore = errors.New("account session store unavailable")
	ErrSessionStale = errors.New("account has logged in elsewhere")
	ErrUsernameUsed = errors.New("用户名已存在")
)

const (
	UsernameMinLength = 3
	UsernameMaxLength = 32
	PasswordMinLength = 6
	PasswordMaxLength = 128
)

type SessionStore interface {
	SetCurrentSession(ctx context.Context, userID int64, sessionID string, ttl time.Duration) error
	CurrentSession(ctx context.Context, userID int64) (string, bool, error)
	DeleteCurrentSession(ctx context.Context, userID int64) error
}

type Service struct {
	repo     *Repository
	tokens   *TokenManager
	sessions SessionStore
	tokenTTL time.Duration
}

type LoginResult struct {
	User  PublicUser `json:"user"`
	Token string     `json:"token"`
}

func NewService(repo *Repository, tokenSecret string, sessions SessionStore) *Service {
	tokenTTL := 24 * time.Hour
	return &Service{
		repo:     repo,
		tokens:   NewTokenManager(tokenSecret, tokenTTL),
		sessions: sessions,
		tokenTTL: tokenTTL,
	}
}

func (s *Service) Register(ctx context.Context, username, password, displayName string) (LoginResult, error) {
	username = normalizeUsername(username)
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		displayName = username
	}
	if err := validateUsernamePassword(username, password); err != nil {
		return LoginResult{}, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return LoginResult{}, err
	}
	u, err := s.repo.Create(ctx, username, string(hash), displayName)
	if err != nil {
		return LoginResult{}, err
	}
	token, err := s.issueToken(ctx, u)
	if err != nil {
		return LoginResult{}, err
	}
	return LoginResult{User: u.Public(), Token: token}, nil
}

func (s *Service) Login(ctx context.Context, username, password string) (LoginResult, error) {
	u, err := s.repo.FindByUsername(ctx, normalizeUsername(username))
	if err != nil {
		return LoginResult{}, ErrUnauthorized
	}
	if u.Disabled {
		return LoginResult{}, ErrDisabled
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return LoginResult{}, ErrUnauthorized
	}
	token, err := s.issueToken(ctx, u)
	if err != nil {
		return LoginResult{}, err
	}
	return LoginResult{User: u.Public(), Token: token}, nil
}

func (s *Service) AuthenticateToken(ctx context.Context, token string) (User, error) {
	u, _, err := s.AuthenticateTokenClaims(ctx, token)
	return u, err
}

func (s *Service) AuthenticateTokenClaims(ctx context.Context, token string) (User, TokenClaims, error) {
	claims, err := s.tokens.Verify(strings.TrimSpace(token))
	if err != nil {
		return User{}, TokenClaims{}, err
	}
	if err := s.EnsureSession(ctx, claims.UserID, claims.SessionID); err != nil {
		return User{}, TokenClaims{}, err
	}
	u, err := s.repo.FindByID(ctx, claims.UserID)
	if err != nil {
		return User{}, TokenClaims{}, err
	}
	if u.Disabled {
		return User{}, TokenClaims{}, ErrDisabled
	}
	return u, claims, nil
}

func (s *Service) EnsureSession(ctx context.Context, userID int64, sessionID string) error {
	if s.sessions == nil {
		return ErrSessionStore
	}
	current, ok, err := s.sessions.CurrentSession(ctx, userID)
	if err != nil {
		return err
	}
	if !ok || current != sessionID {
		return ErrSessionStale
	}
	return nil
}

func (s *Service) EnsureActive(ctx context.Context, id int64) error {
	u, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if u.Disabled {
		return ErrDisabled
	}
	return nil
}

func (s *Service) SearchUsers(ctx context.Context, filter UserFilter) ([]User, error) {
	return s.repo.List(ctx, filter)
}

func (s *Service) Stats(ctx context.Context) (Stats, error) {
	return s.repo.Stats(ctx)
}

func (s *Service) SetDisabled(ctx context.Context, userID int64, disabled bool) error {
	if err := s.repo.SetDisabled(ctx, userID, disabled); err != nil {
		return err
	}
	if disabled && s.sessions != nil {
		_ = s.sessions.DeleteCurrentSession(ctx, userID)
	}
	return nil
}

func (s *Service) SetMembership(ctx context.Context, userID int64, membership string) error {
	return s.repo.SetMembership(ctx, userID, membership)
}

func (s *Service) Delete(ctx context.Context, userID int64) error {
	if s.sessions != nil {
		_ = s.sessions.DeleteCurrentSession(ctx, userID)
	}
	return s.repo.Delete(ctx, userID)
}

func (s *Service) issueToken(ctx context.Context, u User) (string, error) {
	if s.sessions == nil {
		return "", ErrSessionStore
	}
	sessionID, err := randomSessionID()
	if err != nil {
		return "", err
	}
	if err := s.sessions.SetCurrentSession(ctx, u.ID, sessionID, s.tokenTTL); err != nil {
		return "", err
	}
	return s.tokens.Sign(u, sessionID)
}

func randomSessionID() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

func normalizeUsername(v string) string {
	return strings.ToLower(strings.TrimSpace(v))
}

func validateUsernamePassword(username, password string) error {
	if len(username) < UsernameMinLength || len(username) > UsernameMaxLength {
		return fmt.Errorf("用户名长度必须是 %d-%d 位", UsernameMinLength, UsernameMaxLength)
	}
	for _, r := range username {
		if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '_' && r != '-' {
			return fmt.Errorf("用户名只能包含小写字母、数字、下划线和短横线")
		}
	}
	if len(password) < PasswordMinLength || len(password) > PasswordMaxLength {
		return fmt.Errorf("密码长度必须是 %d-%d 位", PasswordMinLength, PasswordMaxLength)
	}
	return nil
}
