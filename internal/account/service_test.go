package account

import (
	"context"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestEmailValidation(t *testing.T) {
	valid := []string{
		"player@example.com",
		"PLAYER+tag@Example.COM",
		"a.b-c_d@example.co",
	}
	for _, email := range valid {
		if !IsValidEmail(email) {
			t.Fatalf("email %q should be valid", email)
		}
	}

	invalid := []string{
		"player",
		"player@",
		"@example.com",
		"player@example",
		"player example@example.com",
		"玩家@example.com",
	}
	for _, email := range invalid {
		if IsValidEmail(email) {
			t.Fatalf("email %q should be invalid", email)
		}
	}
}

func TestValidateUsernamePasswordRequiresEmail(t *testing.T) {
	if err := validateUsernamePassword("player", "secret1"); err == nil {
		t.Fatalf("plain username was accepted")
	}
	if err := validateUsernamePassword("player@example.com", "secret1"); err != nil {
		t.Fatalf("email was rejected: %v", err)
	}
}

func TestDefaultDisplayNameUsesEmailLocalPart(t *testing.T) {
	if got, want := defaultDisplayName("player@example.com"), "player"; got != want {
		t.Fatalf("display name=%q, want %q", got, want)
	}
}

func TestRegisterNormalizesEmailAndRejectsDuplicate(t *testing.T) {
	repo := &fakeAccountRepo{users: make(map[string]User)}
	svc := NewService(repo, "secret", &fakeSessionStore{sessions: make(map[int64]string)})
	res, err := svc.Register(context.Background(), "PLAYER@Example.COM ", "secret1", "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if res.User.Email != "player@example.com" || res.User.Username != "player@example.com" {
		t.Fatalf("user email=%q username=%q", res.User.Email, res.User.Username)
	}
	if res.User.DisplayName != "player" {
		t.Fatalf("displayName=%q, want player", res.User.DisplayName)
	}
	if _, err := svc.Register(context.Background(), "player@example.com", "secret1", ""); err != ErrUsernameUsed {
		t.Fatalf("duplicate register err=%v, want %v", err, ErrUsernameUsed)
	}
}

func TestLoginWithEmailAndPassword(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("secret1"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	repo := &fakeAccountRepo{
		users: map[string]User{
			"player@example.com": {
				ID:           7,
				Username:     "player@example.com",
				PasswordHash: string(hash),
				DisplayName:  "Player",
				Role:         "player",
			},
		},
	}
	svc := NewService(repo, "secret", &fakeSessionStore{sessions: make(map[int64]string)})
	res, err := svc.Login(context.Background(), " PLAYER@example.com ", "secret1")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if res.User.ID != 7 || res.Token == "" {
		t.Fatalf("bad login result: %+v token=%q", res.User, res.Token)
	}
	if _, err := svc.Login(context.Background(), "player@example.com", "wrong"); err != ErrUnauthorized {
		t.Fatalf("wrong password err=%v, want %v", err, ErrUnauthorized)
	}
}

func TestResetPasswordReplacesPasswordAndClearsSession(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("oldpass"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	repo := &fakeAccountRepo{
		users: map[string]User{
			"player@example.com": {
				ID:           7,
				Username:     "player@example.com",
				PasswordHash: string(hash),
				DisplayName:  "Player",
				Role:         "player",
			},
		},
	}
	sessions := &fakeSessionStore{sessions: map[int64]string{7: "session"}}
	svc := NewService(repo, "secret", sessions)

	if err := svc.ResetPassword(context.Background(), " PLAYER@example.com ", "newpass"); err != nil {
		t.Fatalf("reset password: %v", err)
	}
	if _, ok := sessions.sessions[7]; ok {
		t.Fatalf("reset password did not clear current session")
	}
	if _, err := svc.Login(context.Background(), "player@example.com", "oldpass"); err != ErrUnauthorized {
		t.Fatalf("old password err=%v, want unauthorized", err)
	}
	if _, err := svc.Login(context.Background(), "player@example.com", "newpass"); err != nil {
		t.Fatalf("login with new password: %v", err)
	}
}

func TestChangePasswordRequiresOldPassword(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("oldpass"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	repo := &fakeAccountRepo{
		users: map[string]User{
			"player@example.com": {
				ID:           7,
				Username:     "player@example.com",
				PasswordHash: string(hash),
				DisplayName:  "Player",
				Role:         "player",
			},
		},
	}
	svc := NewService(repo, "secret", &fakeSessionStore{sessions: make(map[int64]string)})
	if err := svc.ChangePassword(context.Background(), 7, "wrong", "newpass"); err != ErrUnauthorized {
		t.Fatalf("wrong old password err=%v, want unauthorized", err)
	}
	if err := svc.ChangePassword(context.Background(), 7, "oldpass", "newpass"); err != nil {
		t.Fatalf("change password: %v", err)
	}
	if _, err := svc.Login(context.Background(), "player@example.com", "newpass"); err != nil {
		t.Fatalf("login with changed password: %v", err)
	}
}

type fakeAccountRepo struct {
	nextID int64
	users  map[string]User
}

func (r *fakeAccountRepo) Create(_ context.Context, username, passwordHash, displayName string) (User, error) {
	if _, ok := r.users[username]; ok {
		return User{}, ErrUsernameUsed
	}
	r.nextID++
	u := User{
		ID:           r.nextID,
		Username:     username,
		PasswordHash: passwordHash,
		DisplayName:  displayName,
		Role:         "player",
	}
	r.users[username] = u
	return u, nil
}

func (r *fakeAccountRepo) FindByUsername(_ context.Context, username string) (User, error) {
	u, ok := r.users[username]
	if !ok {
		return User{}, ErrNotFound
	}
	return u, nil
}

func (r *fakeAccountRepo) FindByID(_ context.Context, id int64) (User, error) {
	for _, u := range r.users {
		if u.ID == id {
			return u, nil
		}
	}
	return User{}, ErrNotFound
}

func (r *fakeAccountRepo) UpdatePassword(_ context.Context, userID int64, passwordHash string) error {
	for email, u := range r.users {
		if u.ID == userID {
			u.PasswordHash = passwordHash
			r.users[email] = u
			return nil
		}
	}
	return ErrNotFound
}

func (r *fakeAccountRepo) List(context.Context, UserFilter) ([]User, error) { return nil, nil }
func (r *fakeAccountRepo) Stats(context.Context) (Stats, error)             { return Stats{}, nil }
func (r *fakeAccountRepo) SetDisabled(context.Context, int64, bool) error   { return nil }
func (r *fakeAccountRepo) SetMembership(context.Context, int64, string) error {
	return nil
}
func (r *fakeAccountRepo) AddRelayTraffic(context.Context, int64, int64) error { return nil }
func (r *fakeAccountRepo) ConsumeRelayTraffic(context.Context, int64, int64) error {
	return nil
}
func (r *fakeAccountRepo) Delete(context.Context, int64) error { return nil }

type fakeSessionStore struct {
	sessions map[int64]string
}

func (s *fakeSessionStore) SetCurrentSession(_ context.Context, userID int64, sessionID string, _ time.Duration) error {
	s.sessions[userID] = sessionID
	return nil
}

func (s *fakeSessionStore) CurrentSession(_ context.Context, userID int64) (string, bool, error) {
	value, ok := s.sessions[userID]
	return value, ok, nil
}

func (s *fakeSessionStore) DeleteCurrentSession(_ context.Context, userID int64) error {
	delete(s.sessions, userID)
	return nil
}
