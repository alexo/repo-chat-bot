package main

import (
	"strings"
	"testing"
	"time"
)

// setEnv sets the relevant env vars for LoadConfig, clearing any that aren't
// provided in m. t.Setenv with "" is equivalent to unset for os.Getenv.
func setEnv(t *testing.T, m map[string]string) {
	t.Helper()
	keys := []string{
		"TELEGRAM_ENABLED",
		"TELEGRAM_BOT_TOKEN",
		"SLACK_ENABLED",
		"SLACK_APP_TOKEN",
		"SLACK_BOT_TOKEN",
		"AI_ENABLED",
		"LLM_PROVIDER",
		"LLM_API_KEY",
		"LLM_MODEL",
		"REPO_PATH",
		"ALLOWED_USER_IDS",
		"KBSYNC_ENABLED",
		"KB_STORAGE_PROVIDER",
		"KB_SYNC_BASE_DIR",
		"KB_SYNC_INTERVAL",
		"KB_SYNC_DELETE",
		"KB_SYNC_ON_START",
		"HEALTHCHECK_ENABLED",
		"HEALTHCHECK_PORT",
	}
	for _, k := range keys {
		t.Setenv(k, m[k])
	}
}

func TestLoadConfig_Success(t *testing.T) {
	t.Run("required vars set, provider and model default", func(t *testing.T) {
		setEnv(t, map[string]string{
			"TELEGRAM_ENABLED":   "true",
			"TELEGRAM_BOT_TOKEN": "tok",
			"AI_ENABLED":         "true",
			"LLM_API_KEY":        "key",
			"REPO_PATH":          "/tmp/repo",
			"ALLOWED_USER_IDS":   "42",
		})
		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if cfg.AI.Provider != "anthropic" {
			t.Errorf("default provider: got %q, want anthropic", cfg.AI.Provider)
		}
		if cfg.AI.Model != "claude-sonnet-4-6" {
			t.Errorf("default model: got %q, want claude-sonnet-4-6", cfg.AI.Model)
		}
		if !cfg.Telegram.AllowedUserIDs[42] {
			t.Errorf("user 42 missing from Telegram.AllowedUserIDs")
		}
	})

	t.Run("custom model honored", func(t *testing.T) {
		setEnv(t, map[string]string{
			"TELEGRAM_ENABLED":   "true",
			"TELEGRAM_BOT_TOKEN": "tok",
			"AI_ENABLED":         "true",
			"LLM_API_KEY":        "key",
			"LLM_MODEL":          "claude-opus-4-7",
			"REPO_PATH":          "/tmp/repo",
			"ALLOWED_USER_IDS":   "42",
		})
		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if cfg.AI.Model != "claude-opus-4-7" {
			t.Errorf("model: got %q, want claude-opus-4-7", cfg.AI.Model)
		}
	})

	t.Run("custom provider honored", func(t *testing.T) {
		setEnv(t, map[string]string{
			"TELEGRAM_ENABLED":   "true",
			"TELEGRAM_BOT_TOKEN": "tok",
			"AI_ENABLED":         "true",
			"LLM_PROVIDER":       "openai",
			"LLM_API_KEY":        "key",
			"LLM_MODEL":          "gpt-4o",
			"REPO_PATH":          "/tmp/repo",
			"ALLOWED_USER_IDS":   "42",
		})
		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if cfg.AI.Provider != "openai" {
			t.Errorf("provider: got %q, want openai", cfg.AI.Provider)
		}
		if cfg.AI.Model != "gpt-4o" {
			t.Errorf("model: got %q, want gpt-4o", cfg.AI.Model)
		}
	})

	t.Run("multiple user IDs with whitespace", func(t *testing.T) {
		setEnv(t, map[string]string{
			"TELEGRAM_ENABLED":   "true",
			"TELEGRAM_BOT_TOKEN": "tok",
			"AI_ENABLED":         "true",
			"LLM_API_KEY":        "key",
			"REPO_PATH":          "/tmp/repo",
			"ALLOWED_USER_IDS":   " 1 , 2,3 ",
		})
		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		for _, id := range []int64{1, 2, 3} {
			if !cfg.Telegram.AllowedUserIDs[id] {
				t.Errorf("user %d not allowed", id)
			}
		}
	})

	t.Run("slack only — no Telegram token, no ALLOWED_USER_IDS", func(t *testing.T) {
		setEnv(t, map[string]string{
			"SLACK_ENABLED":   "true",
			"SLACK_APP_TOKEN": "xapp-1",
			"SLACK_BOT_TOKEN": "xoxb-1",
			"AI_ENABLED":      "true",
			"LLM_API_KEY":     "key",
			"REPO_PATH":       "/tmp/repo",
		})
		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if cfg.Telegram.BotToken != "" {
			t.Errorf("telegram token: got %q, want empty", cfg.Telegram.BotToken)
		}
		if cfg.Slack.AppToken != "xapp-1" || cfg.Slack.BotToken != "xoxb-1" {
			t.Errorf("slack tokens not loaded: %+v", cfg)
		}
	})

	t.Run("everything disabled — empty env boots", func(t *testing.T) {
		setEnv(t, map[string]string{})
		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if cfg.Telegram.Enabled || cfg.Slack.Enabled || cfg.AI.Enabled || cfg.KBSync.Enabled || cfg.Healthcheck.Enabled {
			t.Errorf("expected all toggles disabled, got %+v", cfg)
		}
		if cfg.AI.Provider != "" {
			t.Errorf("AI.Provider should not be defaulted when AI disabled, got %q", cfg.AI.Provider)
		}
	})

	t.Run("disabled platform tokens are ignored", func(t *testing.T) {
		// Telegram tokens linger from a previous session, but only Slack is enabled.
		// LoadConfig should not validate Telegram fields (no ALLOWED_USER_IDS required).
		setEnv(t, map[string]string{
			"TELEGRAM_BOT_TOKEN": "stale-tok",
			"SLACK_ENABLED":      "true",
			"SLACK_APP_TOKEN":    "xapp-1",
			"SLACK_BOT_TOKEN":    "xoxb-1",
			"AI_ENABLED":         "true",
			"LLM_API_KEY":        "key",
			"REPO_PATH":          "/tmp/repo",
		})
		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if cfg.Telegram.Enabled {
			t.Errorf("telegram should be disabled")
		}
		if !cfg.Slack.Enabled {
			t.Errorf("slack should be enabled")
		}
	})

	t.Run("telegram + slack both configured", func(t *testing.T) {
		setEnv(t, map[string]string{
			"TELEGRAM_ENABLED":   "true",
			"TELEGRAM_BOT_TOKEN": "tok",
			"SLACK_ENABLED":      "true",
			"SLACK_APP_TOKEN":    "xapp-1",
			"SLACK_BOT_TOKEN":    "xoxb-1",
			"AI_ENABLED":         "true",
			"LLM_API_KEY":        "key",
			"REPO_PATH":          "/tmp/repo",
			"ALLOWED_USER_IDS":   "42",
		})
		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if !cfg.Telegram.AllowedUserIDs[42] {
			t.Errorf("user 42 missing from Telegram.AllowedUserIDs")
		}
		if cfg.Slack.AppToken == "" || cfg.Slack.BotToken == "" {
			t.Errorf("slack tokens not loaded: %+v", cfg)
		}
	})
}

func TestLoadConfig_KBSync(t *testing.T) {
	t.Run("disabled by default", func(t *testing.T) {
		setEnv(t, map[string]string{
			"TELEGRAM_ENABLED":   "true",
			"TELEGRAM_BOT_TOKEN": "tok",
			"AI_ENABLED":         "true",
			"LLM_API_KEY":        "key",
			"REPO_PATH":          "/tmp/repo",
			"ALLOWED_USER_IDS":   "42",
		})
		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if cfg.KBSync.Enabled {
			t.Errorf("expected sync disabled")
		}
	})

	t.Run("provider ignored when toggle off", func(t *testing.T) {
		// KB_STORAGE_PROVIDER alone is no longer enough — KBSYNC_ENABLED must be true.
		setEnv(t, map[string]string{
			"TELEGRAM_ENABLED":    "true",
			"TELEGRAM_BOT_TOKEN":  "tok",
			"AI_ENABLED":          "true",
			"LLM_API_KEY":         "key",
			"REPO_PATH":           "/tmp/repo",
			"ALLOWED_USER_IDS":    "42",
			"KB_STORAGE_PROVIDER": "s3",
			"KB_SYNC_BASE_DIR":    "/tmp/kb",
		})
		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if cfg.KBSync.Enabled {
			t.Errorf("expected sync disabled when KBSYNC_ENABLED unset")
		}
		if cfg.KBSync.Interval != 0 {
			t.Errorf("interval should be zero when disabled, got %v", cfg.KBSync.Interval)
		}
	})

	t.Run("s3 provider with defaults", func(t *testing.T) {
		setEnv(t, map[string]string{
			"TELEGRAM_ENABLED":    "true",
			"TELEGRAM_BOT_TOKEN":  "tok",
			"AI_ENABLED":          "true",
			"LLM_API_KEY":         "key",
			"ALLOWED_USER_IDS":    "42",
			"KBSYNC_ENABLED":      "true",
			"KB_STORAGE_PROVIDER": "s3",
			"KB_SYNC_BASE_DIR":    "/tmp/kb",
		})
		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if cfg.KBSync.Provider != "s3" {
			t.Errorf("provider: got %q, want s3", cfg.KBSync.Provider)
		}
		if cfg.KBSync.Interval != time.Hour {
			t.Errorf("interval default: got %v, want 1h", cfg.KBSync.Interval)
		}
		if !cfg.KBSync.Delete {
			t.Errorf("delete default: got false, want true")
		}
		if !cfg.KBSync.OnStart {
			t.Errorf("on-start default: got false, want true")
		}
		if cfg.AI.RepoPath != "/tmp/kb/current" {
			t.Errorf("repo path derived: got %q, want /tmp/kb/current", cfg.AI.RepoPath)
		}
	})

	t.Run("explicit overrides honored", func(t *testing.T) {
		setEnv(t, map[string]string{
			"TELEGRAM_ENABLED":    "true",
			"TELEGRAM_BOT_TOKEN":  "tok",
			"AI_ENABLED":          "true",
			"LLM_API_KEY":         "key",
			"ALLOWED_USER_IDS":    "42",
			"KBSYNC_ENABLED":      "true",
			"KB_STORAGE_PROVIDER": "oci",
			"KB_SYNC_BASE_DIR":    "/tmp/kb",
			"KB_SYNC_INTERVAL":    "15m",
			"KB_SYNC_DELETE":      "false",
			"KB_SYNC_ON_START":    "false",
		})
		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if cfg.KBSync.Interval != 15*time.Minute {
			t.Errorf("interval: got %v, want 15m", cfg.KBSync.Interval)
		}
		if cfg.KBSync.Delete {
			t.Errorf("delete: got true, want false")
		}
		if cfg.KBSync.OnStart {
			t.Errorf("on-start: got true, want false")
		}
	})

	t.Run("invalid provider rejected", func(t *testing.T) {
		setEnv(t, map[string]string{
			"TELEGRAM_ENABLED":    "true",
			"TELEGRAM_BOT_TOKEN":  "tok",
			"AI_ENABLED":          "true",
			"LLM_API_KEY":         "key",
			"ALLOWED_USER_IDS":    "42",
			"KBSYNC_ENABLED":      "true",
			"KB_STORAGE_PROVIDER": "gcs",
			"KB_SYNC_BASE_DIR":    "/tmp/kb",
		})
		_, err := LoadConfig()
		if err == nil || !strings.Contains(err.Error(), "KB_STORAGE_PROVIDER") {
			t.Fatalf("expected provider validation error, got %v", err)
		}
	})

	t.Run("provider required when enabled", func(t *testing.T) {
		setEnv(t, map[string]string{
			"TELEGRAM_ENABLED":   "true",
			"TELEGRAM_BOT_TOKEN": "tok",
			"AI_ENABLED":         "true",
			"LLM_API_KEY":        "key",
			"REPO_PATH":          "/tmp/repo",
			"ALLOWED_USER_IDS":   "42",
			"KBSYNC_ENABLED":     "true",
			"KB_SYNC_BASE_DIR":   "/tmp/kb",
		})
		_, err := LoadConfig()
		if err == nil || !strings.Contains(err.Error(), "KB_STORAGE_PROVIDER") {
			t.Fatalf("expected provider error, got %v", err)
		}
	})

	t.Run("base dir required when enabled", func(t *testing.T) {
		setEnv(t, map[string]string{
			"TELEGRAM_ENABLED":    "true",
			"TELEGRAM_BOT_TOKEN":  "tok",
			"AI_ENABLED":          "true",
			"LLM_API_KEY":         "key",
			"ALLOWED_USER_IDS":    "42",
			"KBSYNC_ENABLED":      "true",
			"KB_STORAGE_PROVIDER": "s3",
		})
		_, err := LoadConfig()
		if err == nil || !strings.Contains(err.Error(), "KB_SYNC_BASE_DIR") {
			t.Fatalf("expected base dir error, got %v", err)
		}
	})

	t.Run("invalid interval rejected", func(t *testing.T) {
		setEnv(t, map[string]string{
			"TELEGRAM_ENABLED":    "true",
			"TELEGRAM_BOT_TOKEN":  "tok",
			"AI_ENABLED":          "true",
			"LLM_API_KEY":         "key",
			"ALLOWED_USER_IDS":    "42",
			"KBSYNC_ENABLED":      "true",
			"KB_STORAGE_PROVIDER": "s3",
			"KB_SYNC_BASE_DIR":    "/tmp/kb",
			"KB_SYNC_INTERVAL":    "garbage",
		})
		_, err := LoadConfig()
		if err == nil || !strings.Contains(err.Error(), "KB_SYNC_INTERVAL") {
			t.Fatalf("expected interval error, got %v", err)
		}
	})
}

func TestLoadConfig_Healthcheck(t *testing.T) {
	t.Run("disabled by default", func(t *testing.T) {
		setEnv(t, map[string]string{})
		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if cfg.Healthcheck.Enabled {
			t.Errorf("expected healthcheck disabled by default")
		}
	})

	t.Run("enabled with default port", func(t *testing.T) {
		setEnv(t, map[string]string{
			"HEALTHCHECK_ENABLED": "true",
		})
		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if !cfg.Healthcheck.Enabled {
			t.Errorf("expected healthcheck enabled")
		}
		if cfg.Healthcheck.Port != "8080" {
			t.Errorf("default port: got %q, want 8080", cfg.Healthcheck.Port)
		}
	})

	t.Run("custom port honored", func(t *testing.T) {
		setEnv(t, map[string]string{
			"HEALTHCHECK_ENABLED": "true",
			"HEALTHCHECK_PORT":    "9090",
		})
		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if cfg.Healthcheck.Port != "9090" {
			t.Errorf("port: got %q, want 9090", cfg.Healthcheck.Port)
		}
	})

	t.Run("invalid port rejected", func(t *testing.T) {
		setEnv(t, map[string]string{
			"HEALTHCHECK_ENABLED": "true",
			"HEALTHCHECK_PORT":    "99999",
		})
		_, err := LoadConfig()
		if err == nil || !strings.Contains(err.Error(), "HEALTHCHECK_PORT") {
			t.Fatalf("expected port error, got %v", err)
		}
	})

	t.Run("port ignored when disabled", func(t *testing.T) {
		setEnv(t, map[string]string{
			"HEALTHCHECK_PORT": "garbage",
		})
		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if cfg.Healthcheck.Enabled {
			t.Errorf("expected healthcheck disabled")
		}
	})
}

func TestLoadConfig_Errors(t *testing.T) {
	cases := []struct {
		name      string
		env       map[string]string
		errSubstr string
	}{
		{
			name: "telegram enabled but token missing",
			env: map[string]string{
				"TELEGRAM_ENABLED": "true",
				"LLM_API_KEY":      "key",
				"REPO_PATH":        "/tmp/repo",
				"ALLOWED_USER_IDS": "42",
			},
			errSubstr: "TELEGRAM_BOT_TOKEN is required",
		},
		{
			name: "slack enabled but app token missing",
			env: map[string]string{
				"SLACK_ENABLED":   "true",
				"SLACK_BOT_TOKEN": "xoxb-1",
				"LLM_API_KEY":     "key",
				"REPO_PATH":       "/tmp/repo",
			},
			errSubstr: "SLACK_APP_TOKEN and SLACK_BOT_TOKEN",
		},
		{
			name: "slack enabled but bot token missing",
			env: map[string]string{
				"SLACK_ENABLED":   "true",
				"SLACK_APP_TOKEN": "xapp-1",
				"LLM_API_KEY":     "key",
				"REPO_PATH":       "/tmp/repo",
			},
			errSubstr: "SLACK_APP_TOKEN and SLACK_BOT_TOKEN",
		},
		{
			name: "chat enabled but AI disabled",
			env: map[string]string{
				"TELEGRAM_ENABLED":   "true",
				"TELEGRAM_BOT_TOKEN": "tok",
				"ALLOWED_USER_IDS":   "42",
			},
			errSubstr: "AI_ENABLED=true is required when a chat platform is enabled",
		},
		{
			name: "AI enabled but LLM_API_KEY missing",
			env: map[string]string{
				"AI_ENABLED": "true",
				"REPO_PATH":  "/tmp/repo",
			},
			errSubstr: "LLM_API_KEY is required",
		},
		{
			name: "AI enabled but REPO_PATH missing",
			env: map[string]string{
				"AI_ENABLED":  "true",
				"LLM_API_KEY": "key",
			},
			errSubstr: "REPO_PATH is required",
		},
		{
			name: "telegram enabled but ALLOWED_USER_IDS missing",
			env: map[string]string{
				"TELEGRAM_ENABLED":   "true",
				"TELEGRAM_BOT_TOKEN": "tok",
				"AI_ENABLED":         "true",
				"LLM_API_KEY":        "key",
				"REPO_PATH":          "/tmp/repo",
			},
			errSubstr: "ALLOWED_USER_IDS",
		},
		{
			name: "non-numeric user ID",
			env: map[string]string{
				"TELEGRAM_ENABLED":   "true",
				"TELEGRAM_BOT_TOKEN": "tok",
				"AI_ENABLED":         "true",
				"LLM_API_KEY":        "key",
				"REPO_PATH":          "/tmp/repo",
				"ALLOWED_USER_IDS":   "42,notanumber",
			},
			errSubstr: "invalid ALLOWED_USER_IDS",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setEnv(t, tc.env)
			_, err := LoadConfig()
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.errSubstr) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.errSubstr)
			}
		})
	}
}
