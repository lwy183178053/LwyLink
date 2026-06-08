package mail

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/LwyV-Labs/LwyLink/internal/account"
	"golang.org/x/crypto/bcrypt"
)

const (
	PurposeRegister      = "register"
	PurposePasswordReset = "password_reset"

	codeTTL             = 10 * time.Minute
	resendCooldown      = time.Minute
	recentWindow        = time.Hour
	maxRecentEmailCodes = 8
	maxRecentIPCodes    = 40
)

var (
	ErrMailDisabled          = errors.New("mail service is disabled")
	ErrVerificationNotFound  = errors.New("verification code not found")
	ErrVerificationExpired   = errors.New("verification code expired")
	ErrVerificationUsed      = errors.New("verification code already used")
	ErrVerificationCode      = errors.New("verification code is invalid")
	ErrVerificationRateLimit = errors.New("verification requests are too frequent")
)

type verificationRepository interface {
	Create(ctx context.Context, v Verification) error
	Latest(ctx context.Context, email, purpose string) (Verification, error)
	MarkUsed(ctx context.Context, id int64) error
	CountRecentByEmail(ctx context.Context, email, purpose string, since time.Time) (int, error)
	CountRecentByIP(ctx context.Context, ip, purpose string, since time.Time) (int, error)
}

type AccountLookup interface {
	EmailExists(ctx context.Context, email string) (bool, error)
}

type Service struct {
	repo     verificationRepository
	sender   Sender
	accounts AccountLookup
	enabled  bool
}

type SendInput struct {
	Email   string
	Purpose string
	IP      string
}

func NewService(repo verificationRepository, sender Sender, accounts AccountLookup, enabled bool) *Service {
	return &Service{repo: repo, sender: sender, accounts: accounts, enabled: enabled}
}

func (s *Service) Enabled() bool {
	return s != nil && s.enabled && s.sender != nil
}

func (s *Service) SendVerification(ctx context.Context, input SendInput) error {
	if s == nil || !s.enabled || s.sender == nil {
		return ErrMailDisabled
	}
	email := account.NormalizeEmail(input.Email)
	if !account.IsValidEmail(email) {
		return fmt.Errorf("请输入有效邮箱地址")
	}
	purpose, err := normalizePurpose(input.Purpose)
	if err != nil {
		return err
	}
	now := time.Now()
	latest, err := s.repo.Latest(ctx, email, purpose)
	if err != nil && !errors.Is(err, ErrVerificationNotFound) {
		return err
	}
	if err == nil && latest.CreatedAt.After(now.Add(-resendCooldown)) {
		return ErrVerificationRateLimit
	}
	if err := s.enforceRateLimits(ctx, email, purpose, input.IP, now); err != nil {
		return err
	}
	shouldSend, err := s.validatePurposeTarget(ctx, email, purpose)
	if err != nil {
		return err
	}
	code, err := randomCode()
	if err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(code), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if err := s.repo.Create(ctx, Verification{
		Email:     email,
		Purpose:   purpose,
		CodeHash:  string(hash),
		IP:        strings.TrimSpace(input.IP),
		ExpiresAt: now.Add(codeTTL),
	}); err != nil {
		return err
	}
	if !shouldSend {
		return nil
	}
	return s.sender.Send(ctx, email, subjectForPurpose(purpose), bodyForPurpose(purpose, code))
}

func (s *Service) Verify(ctx context.Context, email, purpose, code string) error {
	if s == nil {
		return ErrMailDisabled
	}
	email = account.NormalizeEmail(email)
	purpose, err := normalizePurpose(purpose)
	if err != nil {
		return err
	}
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return ErrVerificationCode
	}
	latest, err := s.repo.Latest(ctx, email, purpose)
	if err != nil {
		return err
	}
	if latest.UsedAt.Valid {
		return ErrVerificationUsed
	}
	if time.Now().After(latest.ExpiresAt) {
		return ErrVerificationExpired
	}
	if err := bcrypt.CompareHashAndPassword([]byte(latest.CodeHash), []byte(code)); err != nil {
		return ErrVerificationCode
	}
	return s.repo.MarkUsed(ctx, latest.ID)
}

func (s *Service) validatePurposeTarget(ctx context.Context, email, purpose string) (bool, error) {
	if s.accounts == nil {
		return true, nil
	}
	exists, err := s.accounts.EmailExists(ctx, email)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	switch purpose {
	case PurposeRegister:
		if exists {
			return false, account.ErrUsernameUsed
		}
	case PurposePasswordReset:
		if !exists {
			return false, nil
		}
	}
	return true, nil
}

func (s *Service) enforceRateLimits(ctx context.Context, email, purpose, ip string, now time.Time) error {
	since := now.Add(-recentWindow)
	emailCount, err := s.repo.CountRecentByEmail(ctx, email, purpose, since)
	if err != nil {
		return err
	}
	if emailCount >= maxRecentEmailCodes {
		return ErrVerificationRateLimit
	}
	if strings.TrimSpace(ip) == "" {
		return nil
	}
	ipCount, err := s.repo.CountRecentByIP(ctx, ip, purpose, since)
	if err != nil {
		return err
	}
	if ipCount >= maxRecentIPCodes {
		return ErrVerificationRateLimit
	}
	return nil
}

func normalizePurpose(value string) (string, error) {
	switch strings.TrimSpace(value) {
	case PurposeRegister:
		return PurposeRegister, nil
	case PurposePasswordReset:
		return PurposePasswordReset, nil
	default:
		return "", fmt.Errorf("bad verification purpose")
	}
}

func randomCode() (string, error) {
	max := big.NewInt(1000000)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

func subjectForPurpose(purpose string) string {
	switch purpose {
	case PurposePasswordReset:
		return "LwyLink 重置密码验证码"
	default:
		return "LwyLink 注册验证码"
	}
}

func bodyForPurpose(purpose, code string) string {
	action := "注册 LwyLink"
	if purpose == PurposePasswordReset {
		action = "重置 LwyLink 密码"
	}
	return fmt.Sprintf("你的验证码是：%s\n\n该验证码用于%s，10 分钟内有效。若不是你本人操作，请忽略这封邮件。\n", code, action)
}
