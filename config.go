package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	TelegramBotToken string
	SlackAppToken    string
	SlackBotToken    string
	SlackDebug       bool
	LLMDebug         bool
	LLMProvider      string
	LLMAPIKey        string
	LLMModel         string
	RepoPath         string
	AllowedUserIDs   map[int64]bool

	// KB sync (object storage → local mirror). All optional; sync is enabled
	// only when KBStorageProvider != "". When enabled, RepoPath is derived as
	// <KBSyncBaseDir>/current so consumers don't have to know about the symlink.
	KBStorageProvider string        // "s3" or "oci"
	KBSyncBaseDir     string        // where snapshots + `current` symlink live
	KBSyncInterval    time.Duration // poll cadence
	KBSyncDelete      bool          // mirror mode: delete locally what's gone remotely
	KBSyncOnStart     bool          // block boot on first successful sync
}

func LoadConfig() (*Config, error) {
	cfg := &Config{
		TelegramBotToken:  os.Getenv("TELEGRAM_BOT_TOKEN"),
		SlackAppToken:     os.Getenv("SLACK_APP_TOKEN"),
		SlackBotToken:     os.Getenv("SLACK_BOT_TOKEN"),
		SlackDebug:        parseBoolEnv("SLACK_DEBUG"),
		LLMDebug:          parseBoolEnv("LLM_DEBUG"),
		LLMProvider:       os.Getenv("LLM_PROVIDER"),
		LLMAPIKey:         os.Getenv("LLM_API_KEY"),
		LLMModel:          os.Getenv("LLM_MODEL"),
		RepoPath:          os.Getenv("REPO_PATH"),
		AllowedUserIDs:    map[int64]bool{},
		KBStorageProvider: strings.ToLower(strings.TrimSpace(os.Getenv("KB_STORAGE_PROVIDER"))),
		KBSyncBaseDir:     os.Getenv("KB_SYNC_BASE_DIR"),
	}
	telegramConfigured := cfg.TelegramBotToken != ""
	slackAppSet := cfg.SlackAppToken != ""
	slackBotSet := cfg.SlackBotToken != ""
	if slackAppSet != slackBotSet {
		return nil, errors.New("SLACK_APP_TOKEN and SLACK_BOT_TOKEN must both be set or both empty")
	}
	slackConfigured := slackAppSet && slackBotSet
	if !telegramConfigured && !slackConfigured {
		return nil, errors.New("at least one chat platform must be configured: set TELEGRAM_BOT_TOKEN, or both SLACK_APP_TOKEN and SLACK_BOT_TOKEN")
	}

	if cfg.LLMAPIKey == "" {
		return nil, errors.New("LLM_API_KEY is required")
	}
	if err := loadKBSyncConfig(cfg); err != nil {
		return nil, err
	}
	if cfg.RepoPath == "" {
		return nil, errors.New("REPO_PATH is required")
	}
	if cfg.LLMProvider == "" {
		cfg.LLMProvider = "anthropic"
	}
	if cfg.LLMModel == "" {
		cfg.LLMModel = defaultModelFor(cfg.LLMProvider)
	}
	for _, s := range strings.Split(os.Getenv("ALLOWED_USER_IDS"), ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid ALLOWED_USER_IDS entry %q: %w", s, err)
		}
		cfg.AllowedUserIDs[id] = true
	}
	if telegramConfigured && len(cfg.AllowedUserIDs) == 0 {
		return nil, errors.New("ALLOWED_USER_IDS must list at least one Telegram user ID when TELEGRAM_BOT_TOKEN is set")
	}
	return cfg, nil
}

func loadKBSyncConfig(cfg *Config) error {
	if cfg.KBStorageProvider == "" {
		return nil
	}
	switch cfg.KBStorageProvider {
	case "s3", "oci":
	default:
		return fmt.Errorf("KB_STORAGE_PROVIDER must be \"s3\" or \"oci\", got %q", cfg.KBStorageProvider)
	}
	if cfg.KBSyncBaseDir == "" {
		return errors.New("KB_SYNC_BASE_DIR is required when KB_STORAGE_PROVIDER is set")
	}
	cfg.KBSyncInterval = time.Hour
	if v := strings.TrimSpace(os.Getenv("KB_SYNC_INTERVAL")); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("invalid KB_SYNC_INTERVAL %q: %w", v, err)
		}
		cfg.KBSyncInterval = d
	}
	cfg.KBSyncDelete = true
	if v := strings.TrimSpace(os.Getenv("KB_SYNC_DELETE")); v != "" {
		cfg.KBSyncDelete = parseBoolEnv("KB_SYNC_DELETE")
	}
	cfg.KBSyncOnStart = true
	if v := strings.TrimSpace(os.Getenv("KB_SYNC_ON_START")); v != "" {
		cfg.KBSyncOnStart = parseBoolEnv("KB_SYNC_ON_START")
	}
	if cfg.RepoPath == "" {
		cfg.RepoPath = filepath.Join(cfg.KBSyncBaseDir, "current")
	}
	return nil
}

func parseBoolEnv(name string) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	switch v {
	case "1", "true", "yes", "y", "on":
		return true
	}
	return false
}

func defaultModelFor(provider string) string {
	switch provider {
	case "anthropic":
		return "claude-sonnet-4-6"
	case "github":
		return "gpt-4o"
	case "openrouter":
		return "openai/gpt-4o-mini"
	}
	return ""
}
