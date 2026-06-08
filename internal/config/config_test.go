package config

import "testing"

func TestLoadMailConfigAppliesEnvWhenFileIsMissing(t *testing.T) {
	t.Setenv("LWYLINK_MAIL_ENABLED", "true")
	t.Setenv("LWYLINK_MAIL_SMTP_USER", "smtp-user")
	t.Setenv("LWYLINK_MAIL_SMTP_PASSWORD", "smtp-password")
	t.Setenv("LWYLINK_MAIL_FROM_ADDRESS", "lwylink@mail.lwylink.xyz")

	cfg, err := LoadMailConfig("configs/does-not-exist.json")
	if err != nil {
		t.Fatalf("load missing mail config: %v", err)
	}
	if !cfg.Enabled {
		t.Fatalf("mail config was not enabled from env")
	}
	if cfg.SMTPHost != "smtpdm.aliyun.com" || cfg.SMTPPort != 465 {
		t.Fatalf("mail defaults host=%q port=%d", cfg.SMTPHost, cfg.SMTPPort)
	}
	if cfg.SMTPUser != "smtp-user" || cfg.SMTPPassword != "smtp-password" || cfg.FromAddress != "lwylink@mail.lwylink.xyz" {
		t.Fatalf("mail env overrides not applied: %+v", cfg)
	}
}
