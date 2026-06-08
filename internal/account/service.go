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
	ErrNotFound             = errors.New("account not found")
	ErrUnauthorized         = errors.New("invalid email or password")
	ErrDisabled             = errors.New("account is disabled")
	ErrSessionStore         = errors.New("account session store unavailable")
	ErrSessionStale         = errors.New("account has logged in elsewhere")
	ErrUsernameUsed         = errors.New("邮箱已注册")
	ErrRelayTrafficExceeded = errors.New("中继流量额度已用尽")
)

const (
	UsernameMinLength = 6
	UsernameMaxLength = 254
	PasswordMinLength = 6
	PasswordMaxLength = 128
)

type SessionStore interface {
	SetCurrentSession(ctx context.Context, userID int64, sessionID string, ttl time.Duration) error
	CurrentSession(ctx context.Context, userID int64) (string, bool, error)
	DeleteCurrentSession(ctx context.Context, userID int64) error
}

type accountRepository interface {
	Create(ctx context.Context, username, passwordHash, displayName string) (User, error)
	FindByUsername(ctx context.Context, username string) (User, error)
	FindByID(ctx context.Context, id int64) (User, error)
	UpdatePassword(ctx context.Context, userID int64, passwordHash string) error
	List(ctx context.Context, filter UserFilter) ([]User, error)
	Stats(ctx context.Context) (Stats, error)
	SetDisabled(ctx context.Context, userID int64, disabled bool) error
	SetMembership(ctx context.Context, userID int64, membership string) error
	AddRelayTraffic(ctx context.Context, userID, bytes int64) error
	ConsumeRelayTraffic(ctx context.Context, userID, bytes int64) error
	Delete(ctx context.Context, userID int64) error
}

type Service struct {
	repo     accountRepository
	tokens   *TokenManager
	sessions SessionStore
	tokenTTL time.Duration
}

type LoginResult struct {
	User  PublicUser `json:"user"`
	Token string     `json:"token"`
}

func NewService(repo accountRepository, tokenSecret string, sessions SessionStore) *Service {
	tokenTTL := 24 * time.Hour
	return &Service{
		repo:     repo,
		tokens:   NewTokenManager(tokenSecret, tokenTTL),
		sessions: sessions,
		tokenTTL: tokenTTL,
	}
}

func (s *Service) Register(ctx context.Context, username, password, displayName string) (LoginResult, error) {
	username = NormalizeEmail(username)
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		displayName = defaultDisplayName(username)
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
	u, err := s.repo.FindByUsername(ctx, NormalizeEmail(username))
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

func (s *Service) EmailExists(ctx context.Context, email string) (bool, error) {
	_, err := s.repo.FindByUsername(ctx, NormalizeEmail(email))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return false, err
}

func (s *Service) ResetPassword(ctx context.Context, email, password string) error {
	u, err := s.repo.FindByUsername(ctx, NormalizeEmail(email))
	if err != nil {
		return ErrUnauthorized
	}
	if err := validatePassword(password); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if err := s.repo.UpdatePassword(ctx, u.ID, string(hash)); err != nil {
		return err
	}
	if s.sessions != nil {
		_ = s.sessions.DeleteCurrentSession(ctx, u.ID)
	}
	return nil
}

func (s *Service) ChangePassword(ctx context.Context, userID int64, oldPassword, newPassword string) error {
	u, err := s.repo.FindByID(ctx, userID)
	if err != nil {
		return err
	}
	if u.Disabled {
		return ErrDisabled
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(oldPassword)); err != nil {
		return ErrUnauthorized
	}
	if err := validatePassword(newPassword); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return s.repo.UpdatePassword(ctx, u.ID, string(hash))
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

func (s *Service) AddRelayTraffic(ctx context.Context, userID, bytes int64) error {
	return s.repo.AddRelayTraffic(ctx, userID, bytes)
}

func (s *Service) ConsumeRelayTraffic(ctx context.Context, userID, bytes int64) error {
	return s.repo.ConsumeRelayTraffic(ctx, userID, bytes)
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

func NormalizeEmail(v string) string {
	return strings.ToLower(strings.TrimSpace(v))
}

func validateUsernamePassword(username, password string) error {
	if !IsValidEmail(username) {
		return fmt.Errorf("请输入有效邮箱地址")
	}
	return validatePassword(password)
}

func validatePassword(password string) error {
	if len(password) < PasswordMinLength || len(password) > PasswordMaxLength {
		return fmt.Errorf("密码长度必须是 %d-%d 位", PasswordMinLength, PasswordMaxLength)
	}
	return nil
}

func IsValidEmail(email string) bool {
	email = NormalizeEmail(email)
	if len(email) < UsernameMinLength || len(email) > UsernameMaxLength {
		return false
	}
	parts := strings.Split(email, "@")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return false
	}
	if strings.ContainsAny(email, " \t\r\n") || strings.Contains(parts[1], "..") {
		return false
	}
	if !strings.Contains(parts[1], ".") || strings.HasPrefix(parts[1], ".") || strings.HasSuffix(parts[1], ".") {
		return false
	}
	for _, r := range email {
		if r > 127 {
			return false
		}
	}
	return true
}

func defaultDisplayName(email string) string {
	local, _, ok := strings.Cut(email, "@")
	if !ok || strings.TrimSpace(local) == "" {
		return email
	}
	if len([]rune(local)) > 32 {
		return string([]rune(local)[:32])
	}
	return local
}
