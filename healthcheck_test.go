package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthcheckMux(t *testing.T) {
	cfg := &Config{
		Telegram:    TelegramConfig{Enabled: true},
		Slack:       SlackConfig{Enabled: false},
		AI:          AIConfig{Enabled: true},
		KBSync:      KBSyncConfig{Enabled: false},
		Healthcheck: HealthcheckConfig{Enabled: true},
	}
	mux := healthcheckMux(cfg)

	t.Run("/healthz returns 200 ok", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("status: got %d, want 200", rec.Code)
		}
		if rec.Body.String() != "ok" {
			t.Errorf("body: got %q, want \"ok\"", rec.Body.String())
		}
	})

	t.Run("/featurez returns toggle booleans", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/featurez", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status: got %d, want 200", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("content-type: got %q, want application/json", ct)
		}
		var got map[string]bool
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		want := map[string]bool{
			"telegram":    true,
			"slack":       false,
			"ai":          true,
			"kbsync":      false,
			"healthcheck": true,
		}
		if len(got) != len(want) {
			t.Errorf("key count: got %d (%v), want %d (%v)", len(got), got, len(want), want)
		}
		for k, v := range want {
			if got[k] != v {
				t.Errorf("key %q: got %v, want %v", k, got[k], v)
			}
		}
	})

	t.Run("/featurez never leaks sensitive values", func(t *testing.T) {
		// Build a config with values that should NEVER appear in the response.
		sensitive := &Config{
			Telegram:    TelegramConfig{Enabled: true, BotToken: "secret-tg-token"},
			Slack:       SlackConfig{Enabled: true, AppToken: "secret-slack-app", BotToken: "secret-slack-bot"},
			AI:          AIConfig{Enabled: true, APIKey: "secret-llm-key", Model: "claude-x", RepoPath: "/private/path"},
			KBSync:      KBSyncConfig{Enabled: true, Provider: "s3", BaseDir: "/private/kb"},
			Healthcheck: HealthcheckConfig{Enabled: true, Port: "8080"},
		}
		req := httptest.NewRequest(http.MethodGet, "/featurez", nil)
		rec := httptest.NewRecorder()
		healthcheckMux(sensitive).ServeHTTP(rec, req)
		body := rec.Body.String()
		forbidden := []string{
			"secret-tg-token",
			"secret-slack-app",
			"secret-slack-bot",
			"secret-llm-key",
			"claude-x",
			"/private/path",
			"/private/kb",
			"8080",
		}
		for _, s := range forbidden {
			if strings.Contains(body, s) {
				t.Errorf("response leaked sensitive value %q: %s", s, body)
			}
		}
	})
}
