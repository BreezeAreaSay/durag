package ws

import (
	"encoding/json"
	"net/http"

	"github.com/breezeareasay/durag/backend/internal/game"
)

// ClientConfig is what GET /api/config tells the frontend.
type ClientConfig struct {
	DevAuth    bool `json:"dev_auth"`
	MaxPlayers int  `json:"max_players"`
	MinPlayers int  `json:"min_players"`
}

// ConfigHandler serves the public client configuration.
func ConfigHandler(devAuth bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(ClientConfig{DevAuth: devAuth, MaxPlayers: game.MaxPlayers, MinPlayers: game.MinPlayers})
	})
}

// HealthHandler answers 200 OK.
func HealthHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok\n"))
	})
}
