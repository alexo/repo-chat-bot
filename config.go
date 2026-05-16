package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	TelegramBotToken string
	SlackAppToken    string
	SlackBotToken    string
	SlackDebug       bool
	LLMProvider      string
	LLMAPIKey        string
	LLMModel         string
	RepoPath         string
	AllowedUserIDs   map[int64]bool
}

func LoadConfig() (*Config, error) {
	cfg := &Config{
		TelegramBotToken: os.Getenv("TELEGRAM_BOT_TOKEN"),
		SlackAppToken:    os.Getenv("SLACK_APP_TOKEN"),
		SlackBotToken:    os.Getenv("SLACK_BOT_TOKEN"),
		SlackDebug:       parseBoolEnv("SLACK_DEBUG"),
		LLMProvider:      os.Getenv("LLM_PROVIDER"),
		LLMAPIKey:        os.Getenv("LLM_API_KEY"),
		LLMModel:         os.Getenv("LLM_MODEL"),
		RepoPath:         os.Getenv("REPO_PATH"),
		AllowedUserIDs:   map[int64]bool{},
	}
	if cfg.TelegramBotToken == "" {
		return nil, errors.New("TELEGRAM_BOT_TOKEN is required")
	}
	if cfg.LLMAPIKey == "" {
		return nil, errors.New("LLM_API_KEY is required")
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
	if len(cfg.AllowedUserIDs) == 0 {
		return nil, errors.New("ALLOWED_USER_IDS must list at least one Telegram user ID")
	}
	return cfg, nil
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
