package main

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"
)

func startHealthcheck(ctx context.Context, cfg *Config) {
	srv := &http.Server{
		Addr:              ":" + cfg.Healthcheck.Port,
		Handler:           healthcheckMux(cfg),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("healthcheck listening on :%s (/healthz, /featurez)", cfg.Healthcheck.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("healthcheck server error: %v", err)
		}
	}()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
}

func healthcheckMux(cfg *Config) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	})
	mux.HandleFunc("/featurez", func(w http.ResponseWriter, r *http.Request) {
		// Booleans only — never expose tokens, paths, model names, or ports.
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{
			"telegram":    cfg.Telegram.Enabled,
			"slack":       cfg.Slack.Enabled,
			"ai":          cfg.AI.Enabled,
			"kbsync":      cfg.KBSync.Enabled,
			"healthcheck": cfg.Healthcheck.Enabled,
		})
	})
	return mux
}
