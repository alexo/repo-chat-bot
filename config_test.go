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
		"TELEGRAM_BOT_TOKEN",
		"LLM_PROVIDER",
		"LLM_API_KEY",
		"LLM_MODEL",
		"REPO_PATH",
		"ALLOWED_USER_IDS",
		"KB_STORAGE_PROVIDER",
		"KB_SYNC_BASE_DIR",
		"KB_SYNC_INTERVAL",
		"KB_SYNC_DELETE",
		"KB_SYNC_ON_START",
	}
	for _, k := range keys {
		t.Setenv(k, m[k])
	}
}

func TestLoadConfig_Success(t *testing.T) {
	t.Run("required vars set, provider and model default", func(t *testing.T) {
		setEnv(t, map[string]string{
			"TELEGRAM_BOT_TOKEN": "tok",
			"LLM_API_KEY":        "key",
			"REPO_PATH":          "/tmp/repo",
			"ALLOWED_USER_IDS":   "42",
		})
		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if cfg.LLMProvider != "anthropic" {
			t.Errorf("default provider: got %q, want anthropic", cfg.LLMProvider)
		}
		if cfg.LLMModel != "claude-sonnet-4-6" {
			t.Errorf("default model: got %q, want claude-sonnet-4-6", cfg.LLMModel)
		}
		if !cfg.AllowedUserIDs[42] {
			t.Errorf("user 42 missing from AllowedUserIDs")
		}
	})

	t.Run("custom model honored", func(t *testing.T) {
		setEnv(t, map[string]string{
			"TELEGRAM_BOT_TOKEN": "tok",
			"LLM_API_KEY":        "key",
			"LLM_MODEL":          "claude-opus-4-7",
			"REPO_PATH":          "/tmp/repo",
			"ALLOWED_USER_IDS":   "42",
		})
		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if cfg.LLMModel != "claude-opus-4-7" {
			t.Errorf("model: got %q, want claude-opus-4-7", cfg.LLMModel)
		}
	})

	t.Run("custom provider honored", func(t *testing.T) {
		setEnv(t, map[string]string{
			"TELEGRAM_BOT_TOKEN": "tok",
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
		if cfg.LLMProvider != "openai" {
			t.Errorf("provider: got %q, want openai", cfg.LLMProvider)
		}
		if cfg.LLMModel != "gpt-4o" {
			t.Errorf("model: got %q, want gpt-4o", cfg.LLMModel)
		}
	})

	t.Run("multiple user IDs with whitespace", func(t *testing.T) {
		setEnv(t, map[string]string{
			"TELEGRAM_BOT_TOKEN": "tok",
			"LLM_API_KEY":        "key",
			"REPO_PATH":          "/tmp/repo",
			"ALLOWED_USER_IDS":   " 1 , 2,3 ",
		})
		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		for _, id := range []int64{1, 2, 3} {
			if !cfg.AllowedUserIDs[id] {
				t.Errorf("user %d not allowed", id)
			}
		}
	})
}

func TestLoadConfig_KBSync(t *testing.T) {
	t.Run("disabled by default", func(t *testing.T) {
		setEnv(t, map[string]string{
			"TELEGRAM_BOT_TOKEN": "tok",
			"LLM_API_KEY":        "key",
			"REPO_PATH":          "/tmp/repo",
			"ALLOWED_USER_IDS":   "42",
		})
		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if cfg.KBStorageProvider != "" {
			t.Errorf("expected sync disabled, got provider=%q", cfg.KBStorageProvider)
		}
	})

	t.Run("s3 provider with defaults", func(t *testing.T) {
		setEnv(t, map[string]string{
			"TELEGRAM_BOT_TOKEN":  "tok",
			"LLM_API_KEY":         "key",
			"ALLOWED_USER_IDS":    "42",
			"KB_STORAGE_PROVIDER": "s3",
			"KB_SYNC_BASE_DIR":    "/tmp/kb",
		})
		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if cfg.KBStorageProvider != "s3" {
			t.Errorf("provider: got %q, want s3", cfg.KBStorageProvider)
		}
		if cfg.KBSyncInterval != time.Hour {
			t.Errorf("interval default: got %v, want 1h", cfg.KBSyncInterval)
		}
		if !cfg.KBSyncDelete {
			t.Errorf("delete default: got false, want true")
		}
		if !cfg.KBSyncOnStart {
			t.Errorf("on-start default: got false, want true")
		}
		if cfg.RepoPath != "/tmp/kb/current" {
			t.Errorf("repo path derived: got %q, want /tmp/kb/current", cfg.RepoPath)
		}
	})

	t.Run("explicit overrides honored", func(t *testing.T) {
		setEnv(t, map[string]string{
			"TELEGRAM_BOT_TOKEN":  "tok",
			"LLM_API_KEY":         "key",
			"ALLOWED_USER_IDS":    "42",
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
		if cfg.KBSyncInterval != 15*time.Minute {
			t.Errorf("interval: got %v, want 15m", cfg.KBSyncInterval)
		}
		if cfg.KBSyncDelete {
			t.Errorf("delete: got true, want false")
		}
		if cfg.KBSyncOnStart {
			t.Errorf("on-start: got true, want false")
		}
	})

	t.Run("invalid provider rejected", func(t *testing.T) {
		setEnv(t, map[string]string{
			"TELEGRAM_BOT_TOKEN":  "tok",
			"LLM_API_KEY":         "key",
			"ALLOWED_USER_IDS":    "42",
			"KB_STORAGE_PROVIDER": "gcs",
			"KB_SYNC_BASE_DIR":    "/tmp/kb",
		})
		_, err := LoadConfig()
		if err == nil || !strings.Contains(err.Error(), "KB_STORAGE_PROVIDER") {
			t.Fatalf("expected provider validation error, got %v", err)
		}
	})

	t.Run("base dir required when provider set", func(t *testing.T) {
		setEnv(t, map[string]string{
			"TELEGRAM_BOT_TOKEN":  "tok",
			"LLM_API_KEY":         "key",
			"ALLOWED_USER_IDS":    "42",
			"KB_STORAGE_PROVIDER": "s3",
		})
		_, err := LoadConfig()
		if err == nil || !strings.Contains(err.Error(), "KB_SYNC_BASE_DIR") {
			t.Fatalf("expected base dir error, got %v", err)
		}
	})

	t.Run("invalid interval rejected", func(t *testing.T) {
		setEnv(t, map[string]string{
			"TELEGRAM_BOT_TOKEN":  "tok",
			"LLM_API_KEY":         "key",
			"ALLOWED_USER_IDS":    "42",
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

func TestLoadConfig_Errors(t *testing.T) {
	cases := []struct {
		name      string
		env       map[string]string
		errSubstr string
	}{
		{
			name: "missing TELEGRAM_BOT_TOKEN",
			env: map[string]string{
				"LLM_API_KEY":      "key",
				"REPO_PATH":        "/tmp/repo",
				"ALLOWED_USER_IDS": "42",
			},
			errSubstr: "TELEGRAM_BOT_TOKEN",
		},
		{
			name: "missing LLM_API_KEY",
			env: map[string]string{
				"TELEGRAM_BOT_TOKEN": "tok",
				"REPO_PATH":          "/tmp/repo",
				"ALLOWED_USER_IDS":   "42",
			},
			errSubstr: "LLM_API_KEY",
		},
		{
			name: "missing REPO_PATH",
			env: map[string]string{
				"TELEGRAM_BOT_TOKEN": "tok",
				"LLM_API_KEY":        "key",
				"ALLOWED_USER_IDS":   "42",
			},
			errSubstr: "REPO_PATH",
		},
		{
			name: "missing ALLOWED_USER_IDS",
			env: map[string]string{
				"TELEGRAM_BOT_TOKEN": "tok",
				"LLM_API_KEY":        "key",
				"REPO_PATH":          "/tmp/repo",
			},
			errSubstr: "ALLOWED_USER_IDS",
		},
		{
			name: "non-numeric user ID",
			env: map[string]string{
				"TELEGRAM_BOT_TOKEN": "tok",
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
