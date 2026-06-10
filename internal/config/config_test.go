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

func TestControlConfigVoiceICEServersDefaultAndEnv(t *testing.T) {
	cfg := ControlConfig{}
	applyControlDefaults(&cfg)
	if len(cfg.VoiceICEServers) != 1 || len(cfg.VoiceICEServers[0].URLs) != 1 {
		t.Fatalf("default voice ICE servers not applied: %+v", cfg.VoiceICEServers)
	}

	t.Setenv("LWYLINK_VOICE_ICE_SERVERS", "stun:stun.example.com:3478,turn:turn.example.com:3478|user|pass")
	t.Setenv("LWYLINK_VOICE_TURN_URLS", "turn:turn.example.com:3478,turns:turn.example.com:5349")
	t.Setenv("LWYLINK_VOICE_TURN_SECRET", "turn-secret")
	t.Setenv("LWYLINK_VOICE_TURN_USERNAME_PREFIX", "lwylink")
	t.Setenv("LWYLINK_VOICE_TURN_TTL_SECONDS", "600")
	t.Setenv("LWYLINK_VOICE_TURN_USAGE_LOG", "/var/log/lwylink/coturn.log")
	t.Setenv("LWYLINK_VOICE_TURN_USAGE_STATE", "/opt/lwylink/data/coturn-usage.offset")
	t.Setenv("LWYLINK_VOICE_TURN_USAGE_POLL_SECONDS", "5")
	applyControlEnvOverrides(&cfg)
	if got, want := len(cfg.VoiceICEServers), 2; got != want {
		t.Fatalf("voice ICE server count=%d, want %d", got, want)
	}
	if cfg.VoiceICEServers[1].URLs[0] != "turn:turn.example.com:3478" || cfg.VoiceICEServers[1].Username != "user" || cfg.VoiceICEServers[1].Credential != "pass" {
		t.Fatalf("turn ICE server env not parsed: %+v", cfg.VoiceICEServers[1])
	}
	if cfg.VoiceTURNCredential.StaticAuthSecret != "turn-secret" || cfg.VoiceTURNCredential.TTLSeconds != 600 || cfg.VoiceTURNCredential.UsernamePrefix != "lwylink" {
		t.Fatalf("turn REST env not parsed: %+v", cfg.VoiceTURNCredential)
	}
	if got, want := len(cfg.VoiceTURNCredential.URLs), 2; got != want {
		t.Fatalf("turn REST urls=%d, want %d", got, want)
	}
	if cfg.VoiceTURNCredential.UsageLogPath != "/var/log/lwylink/coturn.log" ||
		cfg.VoiceTURNCredential.UsageStatePath != "/opt/lwylink/data/coturn-usage.offset" ||
		cfg.VoiceTURNCredential.UsagePollIntervalSeconds != 5 {
		t.Fatalf("turn usage env not parsed: %+v", cfg.VoiceTURNCredential)
	}
}
