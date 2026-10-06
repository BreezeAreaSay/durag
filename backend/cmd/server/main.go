// Command server runs the Durag authoritative game server.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/breezeareasay/durag/backend/internal/config"
	"github.com/breezeareasay/durag/backend/internal/game"
	"github.com/breezeareasay/durag/backend/internal/store"
	"github.com/breezeareasay/durag/backend/internal/ws"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("configuration", "err", err)
		os.Exit(1)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel(cfg.LogLevel)}))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var st store.Store
	if cfg.RedisAddr != "" {
		rs, err := store.NewRedis(ctx, store.RedisOptions{
			Addr:     cfg.RedisAddr,
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
			RoomTTL:  cfg.RoomTTL,
		})
		if err != nil {
			logger.Error("redis", "err", err)
			os.Exit(1)
		}
		st = rs
		logger.Info("redis connected", "addr", cfg.RedisAddr)
	} else {
		st = store.NewMemory()
		logger.Warn("REDIS_ADDR is empty: using the in-memory store (single instance, state is lost on restart)")
	}
	defer st.Close()

	if cfg.AllowDevAuth {
		logger.Warn("ALLOW_DEV_AUTH is enabled: anybody can log in without Telegram. Never use in production.")
	}
	if cfg.BotToken == "" {
		logger.Warn("TELEGRAM_BOT_TOKEN is empty: Telegram logins will be rejected")
	}

	engine := game.NewEngine(nil)
	engine.ResolveDelay = cfg.BoutResolveDelay
	engine.StumpAutoDelay = cfg.StumpAutoDelay
	engine.TurnTimeout = cfg.TurnTimeout
	auth := ws.TelegramAuth{BotToken: cfg.BotToken, MaxAge: cfg.InitDataMaxAge, AllowDev: cfg.AllowDevAuth}
	hub := ws.NewHub(st, engine, auth, ws.Options{AllowedOrigins: cfg.AllowedOrigins, Logger: logger})

	mux := http.NewServeMux()
	mux.Handle("/ws", hub)
	mux.Handle("/healthz", ws.HealthHandler())
	mux.Handle("/api/config", ws.ConfigHandler(cfg.AllowDevAuth))

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		hub.Close()
	}()

	logger.Info("durag server listening", "addr", srv.Addr, "origins", strings.Join(cfg.AllowedOrigins, ","))
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server", "err", err)
		os.Exit(1)
	}
}

func logLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	}
	return slog.LevelInfo
}
