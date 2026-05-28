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
	Telegram    TelegramConfig
	Slack       SlackConfig
	AI          AIConfig
	KBSync      KBSyncConfig
	Healthcheck HealthcheckConfig
}

type TelegramConfig struct {
	Enabled        bool
	BotToken       string
	AllowedUserIDs map[int64]bool
}

type SlackConfig struct {
	Enabled  bool
	AppToken string
	BotToken string
	Debug    bool
}

type AIConfig struct {
	Enabled  bool
	Provider string
	APIKey   string
	Model    string
	Debug    bool
	RepoPath string
}

// KBSync mirrors object storage (S3 or OCI) into BaseDir. When Enabled, the
// remote bucket is the source of truth; AI.RepoPath is auto-derived as
// <BaseDir>/current if not set explicitly.
type KBSyncConfig struct {
	Enabled  bool
	Provider string
	BaseDir  string
	Interval time.Duration
	Delete   bool
	OnStart  bool
}

type HealthcheckConfig struct {
	Enabled bool
	Port    string
}

func LoadConfig() (*Config, error) {
	cfg := &Config{
		Telegram: TelegramConfig{
			Enabled:        parseBoolEnv("TELEGRAM_ENABLED"),
			BotToken:       os.Getenv("TELEGRAM_BOT_TOKEN"),
			AllowedUserIDs: map[int64]bool{},
		},
		Slack: SlackConfig{
			Enabled:  parseBoolEnv("SLACK_ENABLED"),
			AppToken: os.Getenv("SLACK_APP_TOKEN"),
			BotToken: os.Getenv("SLACK_BOT_TOKEN"),
			Debug:    parseBoolEnv("SLACK_DEBUG"),
		},
		AI: AIConfig{
			Enabled:  parseBoolEnv("AI_ENABLED"),
			Provider: os.Getenv("LLM_PROVIDER"),
			APIKey:   os.Getenv("LLM_API_KEY"),
			Model:    os.Getenv("LLM_MODEL"),
			Debug:    parseBoolEnv("LLM_DEBUG"),
			RepoPath: os.Getenv("REPO_PATH"),
		},
		KBSync: KBSyncConfig{
			Enabled:  parseBoolEnv("KBSYNC_ENABLED"),
			Provider: strings.ToLower(strings.TrimSpace(os.Getenv("KB_STORAGE_PROVIDER"))),
			BaseDir:  os.Getenv("KB_SYNC_BASE_DIR"),
		},
		Healthcheck: HealthcheckConfig{
			Enabled: parseBoolEnv("HEALTHCHECK_ENABLED"),
			Port:    strings.TrimSpace(os.Getenv("HEALTHCHECK_PORT")),
		},
	}

	if cfg.Healthcheck.Enabled {
		if cfg.Healthcheck.Port == "" {
			cfg.Healthcheck.Port = "8080"
		}
		port, err := strconv.Atoi(cfg.Healthcheck.Port)
		if err != nil || port < 1 || port > 65535 {
			return nil, fmt.Errorf("invalid HEALTHCHECK_PORT %q: must be 1-65535", cfg.Healthcheck.Port)
		}
	}

	if cfg.Telegram.Enabled && cfg.Telegram.BotToken == "" {
		return nil, errors.New("TELEGRAM_BOT_TOKEN is required when TELEGRAM_ENABLED=true")
	}
	if cfg.Slack.Enabled && (cfg.Slack.AppToken == "" || cfg.Slack.BotToken == "") {
		return nil, errors.New("SLACK_APP_TOKEN and SLACK_BOT_TOKEN are required when SLACK_ENABLED=true")
	}
	if (cfg.Telegram.Enabled || cfg.Slack.Enabled) && !cfg.AI.Enabled {
		return nil, errors.New("AI_ENABLED=true is required when a chat platform is enabled")
	}

	if err := loadKBSyncConfig(cfg); err != nil {
		return nil, err
	}
	if cfg.AI.Enabled {
		if cfg.AI.APIKey == "" {
			return nil, errors.New("LLM_API_KEY is required when AI_ENABLED=true")
		}
		if cfg.AI.RepoPath == "" {
			return nil, errors.New("REPO_PATH is required when AI_ENABLED=true")
		}
		if cfg.AI.Provider == "" {
			cfg.AI.Provider = "anthropic"
		}
		if cfg.AI.Model == "" {
			cfg.AI.Model = defaultModelFor(cfg.AI.Provider)
		}
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
		cfg.Telegram.AllowedUserIDs[id] = true
	}
	if cfg.Telegram.Enabled && len(cfg.Telegram.AllowedUserIDs) == 0 {
		return nil, errors.New("ALLOWED_USER_IDS must list at least one Telegram user ID when TELEGRAM_ENABLED=true")
	}
	return cfg, nil
}

func loadKBSyncConfig(cfg *Config) error {
	if !cfg.KBSync.Enabled {
		return nil
	}
	switch cfg.KBSync.Provider {
	case "s3", "oci":
	case "":
		return errors.New("KB_STORAGE_PROVIDER is required when KBSYNC_ENABLED=true")
	default:
		return fmt.Errorf("KB_STORAGE_PROVIDER must be \"s3\" or \"oci\", got %q", cfg.KBSync.Provider)
	}
	if cfg.KBSync.BaseDir == "" {
		return errors.New("KB_SYNC_BASE_DIR is required when KBSYNC_ENABLED=true")
	}
	cfg.KBSync.Interval = time.Hour
	if v := strings.TrimSpace(os.Getenv("KB_SYNC_INTERVAL")); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("invalid KB_SYNC_INTERVAL %q: %w", v, err)
		}
		cfg.KBSync.Interval = d
	}
	cfg.KBSync.Delete = true
	if v := strings.TrimSpace(os.Getenv("KB_SYNC_DELETE")); v != "" {
		cfg.KBSync.Delete = parseBoolEnv("KB_SYNC_DELETE")
	}
	cfg.KBSync.OnStart = true
	if v := strings.TrimSpace(os.Getenv("KB_SYNC_ON_START")); v != "" {
		cfg.KBSync.OnStart = parseBoolEnv("KB_SYNC_ON_START")
	}
	if cfg.AI.RepoPath == "" {
		cfg.AI.RepoPath = filepath.Join(cfg.KBSync.BaseDir, "current")
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
