package account

import "testing"

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
