package main

import (
	"context"
	"log"
	"os/signal"
	"sync"
	"syscall"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/joho/godotenv"
)

type chatState struct {
	mu      sync.Mutex
	history []Turn
}

type app struct {
	cfg   *Config
	llm   LLMProvider
	chats sync.Map // chatID -> *chatState
}

func main() {
	if err := godotenv.Load(); err != nil {
		log.Printf("No .env file found or error loading it: %v", err)
	}

	cfg, err := LoadConfig()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	repo, err := NewRepo(cfg.RepoPath)
	if err != nil {
		log.Fatalf("repo: %v", err)
	}
	log.Printf("SUCCESS: Loaded REPO_PATH from config: %s", cfg.RepoPath)
	log.Printf("SUCCESS: Repo root resolved to: %s", repo.Root())

	llm, err := NewProvider(cfg.LLMProvider, cfg.LLMAPIKey, cfg.LLMModel, repo)
	if err != nil {
		log.Fatalf("provider: %v", err)
	}

	a := &app{cfg: cfg, llm: llm}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	b, err := bot.New(cfg.TelegramBotToken, bot.WithDefaultHandler(a.handleMessage))
	if err != nil {
		log.Fatalf("telegram: %v", err)
	}

	log.Println("repo-chat-bot started")

	if cfg.SlackAppToken != "" && cfg.SlackBotToken != "" {
		slackBot, err := NewSlackBot(cfg.SlackAppToken, cfg.SlackBotToken, a)
		if err != nil {
			log.Printf("failed to start slack bot: %v", err)
		} else {
			log.Println("slack bot started")
			go slackBot.Run(ctx)
		}
	}

	b.Start(ctx)
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
