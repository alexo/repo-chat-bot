package main

import (
	"strings"
	"testing"
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
