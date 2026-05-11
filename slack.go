package main

import (
	"context"
	"log"
	"strings"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

type SlackBot struct {
	api    *slack.Client
	socket *socketmode.Client
	app    *app
}

func NewSlackBot(appToken, botToken string, app *app) (*SlackBot, error) {
	api := slack.New(botToken, slack.OptionAppLevelToken(appToken))
	socket := socketmode.New(api)

	return &SlackBot{
		api:    api,
		socket: socket,
		app:    app,
	}, nil
}

func (s *SlackBot) Run(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case evt := <-s.socket.Events:
				switch evt.Type {
				case socketmode.EventTypeEventsAPI:
					eventsAPI, _ := evt.Data.(slackevents.EventsAPIEvent)
					s.socket.Ack(*evt.Request)

					if eventsAPI.Type == slackevents.CallbackEvent {
						innerEvent := eventsAPI.InnerEvent
						switch ev := innerEvent.Data.(type) {
						case *slackevents.MessageEvent:
							// Ignore bot messages
							if ev.BotID != "" {
								continue
							}
							s.handleMessage(ctx, ev)
						}
					}
				}
			}
		}
	}()

	s.socket.RunContext(ctx)
}

func (s *SlackBot) handleMessage(ctx context.Context, ev *slackevents.MessageEvent) {
	// Simple ID check (Slack IDs look like U12345)
	// You might want to update config to support Slack IDs specifically
	log.Printf("Slack message from %s: %s", ev.User, ev.Text)

	if strings.TrimSpace(ev.Text) == "/reset" {
		s.app.chats.Delete(ev.Channel)
		s.api.PostMessage(ev.Channel, slack.MsgOptionText("Conversation cleared.", false))
		return
	}

	state := s.app.stateFor(ev.Channel)
	state.mu.Lock()
	defer state.mu.Unlock()

	reply, newHistory, err := s.app.llm.Ask(ctx, state.history, ev.Text)
	if err != nil {
		log.Printf("LLM error: %v", err)
		s.api.PostMessage(ev.Channel, slack.MsgOptionText("Sorry, something went wrong.", false))
		return
	}
	state.history = newHistory

	s.api.PostMessage(ev.Channel, slack.MsgOptionText(reply, false))
}
