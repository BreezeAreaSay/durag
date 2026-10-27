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

	"github.com/breezeareasay/durag/backend/internal/bot"
	"github.com/breezeareasay/durag/backend/internal/config"
	"github.com/breezeareasay/durag/backend/internal/game"
	"github.com/breezeareasay/durag/backend/internal/monitor"
	"github.com/breezeareasay/durag/backend/internal/store"
	"github.com/breezeareasay/durag/backend/internal/ws"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("configuration", "err", err)
		os.Exit(1)
	}
	recorder := monitor.New()
	logger := slog.New(recorder.Handler(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel(cfg.LogLevel)}), slog.LevelInfo))
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
	engine.MaxAttacks = cfg.MaxAttacks
	ws.FlyChance, ws.FlyDelayMin, ws.FlyDelayMax = cfg.FlyChance, cfg.FlyDelayMin, cfg.FlyDelayMax
	auth := ws.TelegramAuth{BotToken: cfg.BotToken, MaxAge: cfg.InitDataMaxAge, AllowDev: cfg.AllowDevAuth}
	bots := ws.BotOptions{Disabled: !cfg.BotsEnabled, MinDelay: cfg.BotDelayMin, MaxDelay: cfg.BotDelayMax, LLMTimeout: cfg.LLMTimeout, LLMMoves: cfg.LLMMoves, TalkShare: cfg.LLMTalkShare, TalkDaily: cfg.LLMDailyCalls}
	switch {
	case !cfg.BotsEnabled:
		logger.Info("bots are disabled (BOTS=false)")
	case cfg.LLMAPIKey != "":
		llm := bot.NewLLM(cfg.LLMBaseURL, cfg.LLMAPIKey, cfg.LLMModel, cfg.LLMTimeout)
		if cfg.LLMFreeOnly {
			var dropped []string
			llm.Models, dropped = bot.FreeOnly(llm.Models)
			if len(dropped) > 0 {
				logger.Warn("bots: LLM_FREE_ONLY=true drops paid models from LLM_MODEL", "dropped", dropped, "kept", llm.Models)
			}
		}
		for _, m := range strings.Split(cfg.LLMPaidFallback, ",") {
			if m = strings.TrimSpace(m); m != "" {
				llm.Paid = append(llm.Paid, m)
			}
		}
		llm.PaidDaily = cfg.LLMPaidDailyCalls
		llm.Discover = cfg.LLMDiscover && cfg.LLMFreeOnly
		bots.Brain = llm
		logger.Info("bots: LLM brain", "models", llm.Models, "base_url", cfg.LLMBaseURL, "free_only", cfg.LLMFreeOnly, "discover", llm.Discover, "paid_fallback", llm.Paid, "paid_daily", llm.PaidDaily, "moves", cfg.LLMMoves, "role", map[bool]string{true: "moves and lines", false: "lines only, the rules move"}[cfg.LLMMoves])
		go func() { // the catalog's free models, then one request so the logs show whether the key works
			refresh := func() {
				rctx, cancel := context.WithTimeout(ctx, 20*time.Second)
				defer cancel()
				if models, err := llm.RefreshModels(rctx); err != nil {
					logger.Warn("bots: model discovery failed, configured models only", "err", err, "models", models)
				} else {
					logger.Info("bots: models in play", "models", models)
				}
			}
			probe := func() bool {
				probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
				defer cancel()
				model, err := llm.Probe(probeCtx)
				if err != nil {
					logger.Warn("bots: LLM probe failed, bots play by the rules until a model answers", "err", err, "cooling", llm.Status().Cooling)
					return false
				}
				logger.Info("bots: LLM probe ok", "model", model)
				return true
			}
			refresh()
			ok := probe()
			ticker := time.NewTicker(time.Hour)
			defer ticker.Stop()
			retries := 0
			for {
				var retry <-chan time.Time // a failed probe is repeated a few times: free models come and go
				if !ok && retries < 10 {
					retry = time.After(2 * time.Minute)
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					refresh()
				case <-retry:
					retries++
					ok = probe()
				}
			}
		}()
	default:
		logger.Info("bots: rule-based brain only (set LLM_API_KEY for the LLM brain)")
	}
	hub := ws.NewHub(st, engine, auth, ws.Options{AllowedOrigins: cfg.AllowedOrigins, Logger: logger, Bots: bots, Monitor: recorder, AdminIDs: cfg.AdminIDs})
	var llmStatus ws.LLMStatus
	if l, ok := bots.Brain.(*bot.LLM); ok {
		llmStatus = l
	}
	if len(cfg.AdminIDs) > 0 {
		logger.Info("admin panel enabled", "admins", len(cfg.AdminIDs))
	} else {
		logger.Info("admin panel disabled: set ADMIN_IDS to your Telegram user id")
	}

	mux := http.NewServeMux()
	mux.Handle("/ws", hub)
	mux.Handle("/healthz", ws.HealthHandler())
	mux.Handle("/api/config", ws.ConfigHandler(cfg.AllowDevAuth, cfg.BotsEnabled))
	mux.Handle("/api/admin/", hub.AdminHandler(ws.AdminOptions{IDs: cfg.AdminIDs, Auth: auth, LLM: llmStatus, BotsEnabled: cfg.BotsEnabled, FreeOnly: cfg.LLMFreeOnly, Discover: cfg.LLMDiscover}))

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
