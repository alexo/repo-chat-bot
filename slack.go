package main

import (
	"context"
	"log"
	"regexp"
	"strings"
	"sync"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

var (
	mdLinkRE   = regexp.MustCompile(`\[([^]]+)]\((https?://[^)]+)\)`)
	mdBoldRE   = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	mdStrikeRE = regexp.MustCompile(`~~([^~]+)~~`)
)

type SlackBot struct {
	api     *slack.Client
	socket  *socketmode.Client
	app     *app
	debug   bool
	mu      sync.RWMutex
	threads map[string]bool
}

func NewSlackBot(appToken, botToken string, debug bool, app *app) (*SlackBot, error) {
	api := slack.New(botToken, slack.OptionAppLevelToken(appToken))
	socket := socketmode.New(api)

	return &SlackBot{
		api:     api,
		socket:  socket,
		app:     app,
		debug:   debug,
		threads: map[string]bool{},
	}, nil
}

func (s *SlackBot) debugf(format string, args ...any) {
	if s.debug {
		log.Printf("Slack bot [debug]: "+format, args...)
	}
}

func (s *SlackBot) Run(ctx context.Context) {
	log.Println("Slack bot: starting event loop...")
	if s.debug {
		log.Println("Slack bot: debug mode enabled")
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				log.Println("Slack bot: context cancelled, stopping...")
				return
			case evt := <-s.socket.Events:
				s.debugf("received event type: %v", evt.Type)

				switch evt.Type {
				case socketmode.EventTypeHello:
					log.Println("Slack bot: connected (hello received)")
				case socketmode.EventTypeEventsAPI:
					eventsAPI, _ := evt.Data.(slackevents.EventsAPIEvent)
					if evt.Request != nil {
						if err := s.socket.Ack(*evt.Request); err != nil {
							log.Printf("Slack bot: failed to ack event: %v", err)
						}
					}

					s.debugf("inner event type: %v", eventsAPI.InnerEvent.Type)

					if eventsAPI.Type == slackevents.CallbackEvent {
						innerEvent := eventsAPI.InnerEvent
						switch ev := innerEvent.Data.(type) {
						case *slackevents.MessageEvent:
							msgText, threadTS := extractMessageTextAndThread(ev)
							s.debugf("message from user=%s channel=%s subtype=%q thread=%q text=%q", ev.User, ev.Channel, ev.SubType, threadTS, msgText)
							if ev.BotID != "" {
								s.debugf("ignoring bot message")
								continue
							}
							if threadTS == "" || threadTS == ev.TimeStamp {
								// Avoid replying to every top-level channel message.
								s.debugf("ignoring non-thread message without mention")
								continue
							}
							if !s.isTrackedThread(ev.Channel, threadTS) {
								s.debugf("thread not tracked yet (channel=%s thread=%s)", ev.Channel, threadTS)
								continue
							}
							s.handleMessage(ctx, ev.Channel, ev.User, msgText, threadTS)
						case *slackevents.AppMentionEvent:
							s.debugf("mention from user=%s channel=%s text=%q", ev.User, ev.Channel, ev.Text)
							threadTS := pickThreadTS(ev.ThreadTimeStamp, ev.TimeStamp)
							s.trackThread(ev.Channel, threadTS)
							s.handleMessage(ctx, ev.Channel, ev.User, ev.Text, threadTS)
						default:
							s.debugf("unhandled inner event type: %T", ev)
						}
					}
				case socketmode.EventTypeConnected:
					log.Println("Slack bot: socket connected")
				case socketmode.EventTypeDisconnect:
					log.Println("Slack bot: socket disconnected")
				case socketmode.EventTypeConnectionError:
					log.Printf("Slack bot: connection error: %v", evt.Data)
				}
			}
		}
	}()

	if err := s.socket.RunContext(ctx); err != nil {
		log.Printf("Slack bot: run context ended with error: %v", err)
	}
}

func (s *SlackBot) handleMessage(ctx context.Context, channelID, userID, text, threadTS string) {
	s.debugf("handle message from user=%s channel=%s thread=%s", userID, channelID, threadTS)

	// Clean up mentions from the text if it's an app_mention
	// e.g. "<@U12345> what is this?" -> "what is this?"
	cleanText := text
	if strings.Contains(text, "<@") {
		// Basic removal of the first mention if it exists
		parts := strings.SplitN(text, ">", 2)
		if len(parts) > 1 {
			cleanText = strings.TrimSpace(parts[1])
		}
	}

	if strings.TrimSpace(cleanText) == "/reset" {
		s.app.chats.Delete(channelID)
		if _, _, err := s.postReply(channelID, threadTS, "Conversation cleared."); err != nil {
			log.Printf("Slack bot: failed to send reset reply: %v", err)
		}
		return
	}

	state := s.app.stateFor(channelID)
	state.mu.Lock()
	defer state.mu.Unlock()

	reply, newHistory, err := s.app.llm.Ask(ctx, state.history, cleanText)
	if err != nil {
		log.Printf("LLM error: %v", err)
		if _, _, sendErr := s.postReply(channelID, threadTS, "Sorry, something went wrong."); sendErr != nil {
			log.Printf("Slack bot: failed to send error reply: %v", sendErr)
		}
		return
	}
	state.history = newHistory

	formatted := markdownToSlack(reply)
	if _, _, err := s.postReply(channelID, threadTS, formatted); err != nil {
		log.Printf("Slack bot: failed to send reply: %v", err)
	}
}

func pickThreadTS(threadTS, eventTS string) string {
	if strings.TrimSpace(threadTS) != "" {
		return threadTS
	}
	return eventTS
}

func extractMessageTextAndThread(ev *slackevents.MessageEvent) (text string, threadTS string) {
	text = ev.Text
	threadTS = pickThreadTS(ev.ThreadTimeStamp, ev.TimeStamp)

	if ev.Message != nil {
		if strings.TrimSpace(text) == "" {
			text = ev.Message.Text
		}
		if strings.TrimSpace(ev.Message.ThreadTimestamp) != "" {
			threadTS = ev.Message.ThreadTimestamp
		}
		if strings.TrimSpace(ev.Message.Timestamp) != "" && strings.TrimSpace(threadTS) == "" {
			threadTS = ev.Message.Timestamp
		}
	}

	if ev.Root != nil && strings.TrimSpace(threadTS) == "" {
		threadTS = ev.Root.Timestamp
	}

	return strings.TrimSpace(text), strings.TrimSpace(threadTS)
}

func (s *SlackBot) postReply(channelID, threadTS, text string) (string, string, error) {
	if strings.TrimSpace(threadTS) == "" {
		return s.api.PostMessage(channelID, slack.MsgOptionText(text, false))
	}
	return s.api.PostMessage(channelID,
		slack.MsgOptionText(text, false),
		slack.MsgOptionTS(threadTS),
	)
}

func (s *SlackBot) trackThread(channelID, threadTS string) {
	if strings.TrimSpace(channelID) == "" || strings.TrimSpace(threadTS) == "" {
		return
	}
	key := channelID + ":" + threadTS
	s.mu.Lock()
	s.threads[key] = true
	s.mu.Unlock()
	s.debugf("tracking thread key=%s", key)
}

func (s *SlackBot) isTrackedThread(channelID, threadTS string) bool {
	if strings.TrimSpace(channelID) == "" || strings.TrimSpace(threadTS) == "" {
		return false
	}
	key := channelID + ":" + threadTS
	s.mu.RLock()
	ok := s.threads[key]
	s.mu.RUnlock()
	return ok
}

func markdownToSlack(s string) string {
	parts := strings.Split(s, "```")
	for i := 0; i < len(parts); i++ {
		if i%2 == 1 {
			// Preserve code block content verbatim.
			continue
		}
		parts[i] = convertMarkdownSegment(parts[i])
	}
	return strings.Join(parts, "```")
}

func convertMarkdownSegment(s string) string {
	s = mdLinkRE.ReplaceAllString(s, `<$2|$1>`)
	s = mdBoldRE.ReplaceAllString(s, `*$1*`)
	s = mdStrikeRE.ReplaceAllString(s, `~$1~`)

	lines := strings.Split(s, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			headline := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			if headline != "" {
				lines[i] = "*" + headline + "*"
				continue
			}
		}
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			lines[i] = strings.Replace(line, "- ", "• ", 1)
			lines[i] = strings.Replace(lines[i], "* ", "• ", 1)
		}
	}
	return strings.Join(lines, "\n")
}
