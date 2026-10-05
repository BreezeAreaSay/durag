// Package config reads the backend configuration from the environment,
// optionally seeded from a .env file.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds every tunable of the backend.
type Config struct {
	Port             string
	RedisAddr        string // empty = in-memory store
	RedisPassword    string
	RedisDB          int
	BotToken         string
	AllowDevAuth     bool
	AllowedOrigins   []string
	RoomTTL          time.Duration
	InitDataMaxAge   time.Duration
	BoutResolveDelay time.Duration // how long a finished bout stays on the table
	LogLevel         string
}

// LoadDotEnv loads KEY=VALUE pairs from path into the process environment
// without overriding variables that are already set. A missing file is not
// an error.
func LoadDotEnv(path string) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
			value = value[1 : len(value)-1]
		} else if i := strings.Index(value, " #"); i >= 0 {
			value = strings.TrimSpace(value[:i])
		}
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, value)
		}
	}
	return sc.Err()
}

// Load builds the configuration from the environment (after LoadDotEnv(".env")).
func Load() (Config, error) {
	if err := LoadDotEnv(".env"); err != nil {
		return Config{}, fmt.Errorf("config: .env: %w", err)
	}
	return FromEnv()
}

// FromEnv builds the configuration from the current environment only.
func FromEnv() (Config, error) {
	c := Config{
		Port:           getenv("PORT", "8080"),
		RedisAddr:      strings.TrimSpace(os.Getenv("REDIS_ADDR")),
		RedisPassword:  os.Getenv("REDIS_PASSWORD"),
		BotToken:       strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN")),
		AllowDevAuth:   parseBool(getenv("ALLOW_DEV_AUTH", "false")),
		AllowedOrigins: splitList(getenv("ALLOWED_ORIGINS", "*")),
		LogLevel:       getenv("LOG_LEVEL", "info"),
	}
	var err error
	if c.RedisDB, err = strconv.Atoi(getenv("REDIS_DB", "0")); err != nil {
		return c, fmt.Errorf("config: REDIS_DB: %w", err)
	}
	if c.RoomTTL, err = time.ParseDuration(getenv("ROOM_TTL", "24h")); err != nil {
		return c, fmt.Errorf("config: ROOM_TTL: %w", err)
	}
	if c.InitDataMaxAge, err = time.ParseDuration(getenv("TG_INITDATA_MAX_AGE", "24h")); err != nil {
		return c, fmt.Errorf("config: TG_INITDATA_MAX_AGE: %w", err)
	}
	if c.BoutResolveDelay, err = time.ParseDuration(getenv("BOUT_RESOLVE_DELAY", "3500ms")); err != nil {
		return c, fmt.Errorf("config: BOUT_RESOLVE_DELAY: %w", err)
	}
	if c.BotToken == "" && !c.AllowDevAuth {
		return c, errors.New("config: TELEGRAM_BOT_TOKEN is empty and ALLOW_DEV_AUTH is false: nobody could log in")
	}
	return c, nil
}

func getenv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

func parseBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on", "y":
		return true
	}
	return false
}

func splitList(v string) []string {
	var out []string
	for _, part := range strings.Split(v, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}
