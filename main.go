package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/alexo/repo-chat-bot/ai"
	"github.com/alexo/repo-chat-bot/ai/anthropic"
	"github.com/alexo/repo-chat-bot/ai/openai"
	"github.com/alexo/repo-chat-bot/kbsync"
	"github.com/alexo/repo-chat-bot/kbsync/provider"
	"github.com/alexo/repo-chat-bot/kbsync/provider/oci"
	"github.com/alexo/repo-chat-bot/kbsync/provider/s3"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/joho/godotenv"
)

// Set at build time via -ldflags (see Dockerfile + docker-publish.yml).
var (
	Version     = "dev"
	ReleaseDate = "unknown"
)

type chatState struct {
	mu      sync.Mutex
	history []ai.Turn
}

type app struct {
	cfg   *Config
	llm   ai.Provider
	chats sync.Map // chatID -> *chatState
}

func main() {
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		log.Printf("Warning: .env file present but failed to load: %v", err)
	}

	log.Printf("repo-chat-bot version=%s release_date=%s", Version, ReleaseDate)

	cfg, err := LoadConfig()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if cfg.KBStorageProvider != "" {
		syncer, err := buildSyncer(ctx, cfg)
		if err != nil {
			log.Fatalf("kbsync: %v", err)
		}
		if cfg.KBSyncOnStart {
			log.Printf("kbsync: initial sync from %s (this may take a moment)", cfg.KBStorageProvider)
			if err := syncer.SyncNow(ctx); err != nil {
				log.Fatalf("kbsync: initial sync: %v", err)
			}
		}
		go syncer.Start(ctx)
		log.Printf("kbsync: background sync every %s (delete=%t)", cfg.KBSyncInterval, cfg.KBSyncDelete)
	}

	repo, err := NewRepo(cfg.RepoPath)
	if err != nil {
		log.Fatalf("repo: %v", err)
	}
	log.Printf("SUCCESS: Loaded REPO_PATH from config: %s", cfg.RepoPath)
	log.Printf("SUCCESS: Repo root resolved to: %s", repo.Root())
	log.Printf("SUCCESS: LLM provider: %s", cfg.LLMProvider)
	log.Printf("SUCCESS: LLM model: %s", cfg.LLMModel)
	if cfg.LLMDebug {
		log.Printf("SUCCESS: LLM debug mode enabled")
	}

	llm, err := newLLM(cfg, repo)
	if err != nil {
		log.Fatalf("provider: %v", err)
	}

	a := &app{cfg: cfg, llm: llm}

	if cfg.TelegramBotToken != "" {
		b, err := bot.New(cfg.TelegramBotToken, bot.WithDefaultHandler(a.handleMessage))
		if err != nil {
			log.Printf("WARNING: Telegram bot failed to initialize: %v. Continuing without Telegram.", err)
		} else {
			log.Println("telegram bot starting")
			go b.Start(ctx)
		}
	}

	if cfg.SlackAppToken != "" && cfg.SlackBotToken != "" {
		slackBot, err := NewSlackBot(cfg.SlackAppToken, cfg.SlackBotToken, cfg.SlackDebug, a)
		if err != nil {
			log.Printf("failed to start slack bot: %v", err)
		} else {
			log.Println("slack bot starting")
			go slackBot.Run(ctx)
		}
	}

	log.Println("repo-chat-bot is running (press Ctrl+C to exit)")
	<-ctx.Done()
	log.Println("shutting down...")
}

func newLLM(cfg *Config, kb ai.KnowledgeBase) (ai.Provider, error) {
	switch cfg.LLMProvider {
	case "anthropic", "":
		return anthropic.New(cfg.LLMAPIKey, cfg.LLMModel, cfg.LLMDebug, kb), nil
	case "github":
		return openai.NewGitHubProvider(cfg.LLMAPIKey, cfg.LLMModel, cfg.LLMDebug, kb), nil
	case "openrouter":
		return openai.NewOpenRouterProvider(cfg.LLMAPIKey, cfg.LLMModel, cfg.LLMDebug, kb), nil
	default:
		return nil, fmt.Errorf("unknown LLM provider %q", cfg.LLMProvider)
	}
}

func buildSyncer(ctx context.Context, cfg *Config) (*kbsync.Syncer, error) {
	var backend provider.Provider
	var err error
	switch cfg.KBStorageProvider {
	case "s3":
		backend, err = s3.NewProvider(ctx, s3.ConfigFromEnv())
	case "oci":
		backend, err = oci.NewProvider(ctx, oci.ConfigFromEnv())
	default:
		return nil, fmt.Errorf("unknown provider %q", cfg.KBStorageProvider)
	}
	if err != nil {
		return nil, err
	}
	return kbsync.NewSyncer(kbsync.Options{
		BaseDir:  cfg.KBSyncBaseDir,
		Provider: backend,
		Interval: cfg.KBSyncInterval,
		Delete:   cfg.KBSyncDelete,
	}), nil
}

func (a *app) handleMessage(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.Message == nil || update.Message.Text == "" {
		return
	}
	msg := update.Message

	if !a.cfg.AllowedUserIDs[msg.From.ID] {
		log.Printf("denied: user %d (%s) chat %d", msg.From.ID, msg.From.Username, msg.Chat.ID)
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: msg.Chat.ID,
			Text:   "Not authorized.",
		})
		return
	}

	if msg.Text == "/reset" {
		a.chats.Delete(msg.Chat.ID)
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: msg.Chat.ID,
			Text:   "Conversation cleared.",
		})
		return
	}

	state := a.stateFor(msg.Chat.ID)
	state.mu.Lock()
	defer state.mu.Unlock()

	_, _ = b.SendChatAction(ctx, &bot.SendChatActionParams{
		ChatID: msg.Chat.ID,
		Action: models.ChatActionTyping,
	})

	reply, newHistory, err := a.llm.Ask(ctx, state.history, msg.Text)
	if err != nil {
		log.Printf("llm error: %v", err)
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: msg.Chat.ID,
			Text:   "Sorry — something broke. Try /reset.",
		})
		return
	}
	state.history = newHistory

	for _, chunk := range chunkForTelegram(reply, 3500) {
		if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: msg.Chat.ID,
			Text:   chunk,
		}); err != nil {
			log.Printf("telegram send: %v", err)
			break
		}
	}
}

func (a *app) stateFor(chatID any) *chatState {
	v, _ := a.chats.LoadOrStore(chatID, &chatState{})
	return v.(*chatState)
}

// Telegram caps messages at 4096 chars; chunk on paragraph boundaries when possible.
func chunkForTelegram(s string, limit int) []string {
	if len(s) <= limit {
		return []string{s}
	}
	var out []string
	for len(s) > limit {
		cut := limit
		if nl := lastIndexBefore(s, "\n\n", limit); nl > 0 {
			cut = nl + 2
		} else if nl := lastIndexBefore(s, "\n", limit); nl > 0 {
			cut = nl + 1
		}
		out = append(out, s[:cut])
		s = s[cut:]
	}
	if s != "" {
		out = append(out, s)
	}
	return out
}

func lastIndexBefore(s, sep string, before int) int {
	if before > len(s) {
		before = len(s)
	}
	idx := -1
	for i := 0; i+len(sep) <= before; i++ {
		if s[i:i+len(sep)] == sep {
			idx = i
		}
	}
	return idx
}
