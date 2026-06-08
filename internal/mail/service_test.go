package mail

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestServiceSendAndVerifyRegisterCode(t *testing.T) {
	repo := newFakeVerificationRepo()
	sender := &fakeSender{}
	svc := NewService(repo, sender, fakeAccountLookup{}, true)

	if err := svc.SendVerification(context.Background(), SendInput{
		Email:   " PLAYER@Example.COM ",
		Purpose: PurposeRegister,
		IP:      "127.0.0.1",
	}); err != nil {
		t.Fatalf("send verification: %v", err)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("sent messages=%d, want 1", len(sender.messages))
	}
	code := extractCode(t, sender.messages[0].body)
	if err := svc.Verify(context.Background(), "player@example.com", PurposeRegister, code); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if err := svc.Verify(context.Background(), "player@example.com", PurposeRegister, code); !errors.Is(err, ErrVerificationUsed) {
		t.Fatalf("verify reused err=%v, want %v", err, ErrVerificationUsed)
	}
}

func TestServiceRejectsBadExpiredAndRateLimitedCodes(t *testing.T) {
	repo := newFakeVerificationRepo()
	sender := &fakeSender{}
	svc := NewService(repo, sender, fakeAccountLookup{}, true)

	if err := svc.SendVerification(context.Background(), SendInput{Email: "player@example.com", Purpose: PurposeRegister, IP: "127.0.0.1"}); err != nil {
		t.Fatalf("send verification: %v", err)
	}
	if err := svc.SendVerification(context.Background(), SendInput{Email: "player@example.com", Purpose: PurposeRegister, IP: "127.0.0.1"}); !errors.Is(err, ErrVerificationRateLimit) {
		t.Fatalf("resend err=%v, want rate limit", err)
	}
	if err := svc.Verify(context.Background(), "player@example.com", PurposeRegister, "000000"); !errors.Is(err, ErrVerificationCode) {
		t.Fatalf("bad code err=%v, want invalid code", err)
	}

	repo.items[0].CreatedAt = time.Now().Add(-2 * time.Minute)
	repo.items[0].ExpiresAt = time.Now().Add(-time.Second)
	code := extractCode(t, sender.messages[0].body)
	if err := svc.Verify(context.Background(), "player@example.com", PurposeRegister, code); !errors.Is(err, ErrVerificationExpired) {
		t.Fatalf("expired code err=%v, want expired", err)
	}
}

func TestServiceValidatesPurposeTargets(t *testing.T) {
	repo := newFakeVerificationRepo()
	svc := NewService(repo, &fakeSender{}, fakeAccountLookup{existing: map[string]bool{"used@example.com": true}}, true)

	if err := svc.SendVerification(context.Background(), SendInput{Email: "used@example.com", Purpose: PurposeRegister}); err == nil {
		t.Fatalf("register code for existing email succeeded")
	}
	if err := svc.SendVerification(context.Background(), SendInput{Email: "missing@example.com", Purpose: PurposePasswordReset}); err == nil {
		t.Fatalf("password reset code for missing email succeeded")
	}
	if err := svc.SendVerification(context.Background(), SendInput{Email: "used@example.com", Purpose: PurposePasswordReset}); err != nil {
		t.Fatalf("password reset code for existing email failed: %v", err)
	}
}

type fakeSender struct {
	messages []sentMessage
}

type sentMessage struct {
	to      string
	subject string
	body    string
}

func (s *fakeSender) Send(_ context.Context, to, subject, body string) error {
	s.messages = append(s.messages, sentMessage{to: to, subject: subject, body: body})
	return nil
}

type fakeAccountLookup struct {
	existing map[string]bool
	err      error
}

func (l fakeAccountLookup) EmailExists(_ context.Context, email string) (bool, error) {
	if l.err != nil {
		return false, l.err
	}
	return l.existing[email], nil
}

type fakeVerificationRepo struct {
	nextID int64
	items  []Verification
}

func newFakeVerificationRepo() *fakeVerificationRepo {
	return &fakeVerificationRepo{}
}

func (r *fakeVerificationRepo) Create(_ context.Context, v Verification) error {
	r.nextID++
	now := time.Now()
	v.ID = r.nextID
	v.CreatedAt = now
	v.UpdatedAt = now
	r.items = append(r.items, v)
	return nil
}

func (r *fakeVerificationRepo) Latest(_ context.Context, email, purpose string) (Verification, error) {
	for i := len(r.items) - 1; i >= 0; i-- {
		v := r.items[i]
		if v.Email == email && v.Purpose == purpose {
			return v, nil
		}
	}
	return Verification{}, ErrVerificationNotFound
}

func (r *fakeVerificationRepo) MarkUsed(_ context.Context, id int64) error {
	for i := range r.items {
		if r.items[i].ID == id && !r.items[i].UsedAt.Valid {
			r.items[i].UsedAt.Valid = true
			r.items[i].UsedAt.Time = time.Now()
			return nil
		}
	}
	return ErrVerificationNotFound
}

func (r *fakeVerificationRepo) CountRecentByEmail(_ context.Context, email, purpose string, since time.Time) (int, error) {
	return r.count(func(v Verification) bool {
		return v.Email == email && v.Purpose == purpose && !v.CreatedAt.Before(since)
	}), nil
}

func (r *fakeVerificationRepo) CountRecentByIP(_ context.Context, ip, purpose string, since time.Time) (int, error) {
	return r.count(func(v Verification) bool {
		return v.IP == ip && v.Purpose == purpose && !v.CreatedAt.Before(since)
	}), nil
}

func (r *fakeVerificationRepo) count(match func(Verification) bool) int {
	count := 0
	for _, v := range r.items {
		if match(v) {
			count++
		}
	}
	return count
}

func extractCode(t *testing.T, body string) string {
	t.Helper()
	match := regexp.MustCompile(`\d{6}`).FindString(strings.TrimSpace(body))
	if match != "" {
		return match
	}
	t.Fatalf("verification code not found in body %q", body)
	return ""
}
