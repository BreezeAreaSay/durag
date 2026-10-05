package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadDotEnvAndFromEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := "# comment\nTELEGRAM_BOT_TOKEN=\"123:abc\"\nexport PORT=9090\nALLOW_DEV_AUTH=yes # trailing comment\nROOM_TTL=2h\nALLOWED_ORIGINS=https://a.example, https://b.example\nBROKEN LINE\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"TELEGRAM_BOT_TOKEN", "PORT", "ALLOW_DEV_AUTH", "ROOM_TTL", "ALLOWED_ORIGINS", "REDIS_ADDR"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	t.Setenv("PORT", "7070") // real environment wins over .env
	if err := LoadDotEnv(path); err != nil {
		t.Fatal(err)
	}
	c, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if c.BotToken != "123:abc" || c.Port != "7070" || !c.AllowDevAuth || c.RoomTTL != 2*time.Hour {
		t.Fatalf("%+v", c)
	}
	if len(c.AllowedOrigins) != 2 || c.AllowedOrigins[1] != "https://b.example" {
		t.Fatalf("origins %v", c.AllowedOrigins)
	}
	if c.RedisAddr != "" || c.InitDataMaxAge != 24*time.Hour || c.BoutResolveDelay != 2500*time.Millisecond {
		t.Fatalf("%+v", c)
	}
	if err := LoadDotEnv(filepath.Join(dir, "missing.env")); err != nil {
		t.Fatalf("missing file must be ignored: %v", err)
	}
}

func TestFromEnvRejectsNoLoginMethod(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	t.Setenv("ALLOW_DEV_AUTH", "false")
	if _, err := FromEnv(); err == nil {
		t.Fatal("expected an error when nobody could log in")
	}
}
