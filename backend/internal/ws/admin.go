package ws

import (
	"encoding/json"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/breezeareasay/durag/backend/internal/bot"
	"github.com/breezeareasay/durag/backend/internal/game"
	"github.com/breezeareasay/durag/backend/internal/monitor"
	"github.com/breezeareasay/durag/backend/internal/store"
)

// LLMStatus is what the admin panel asks a model client for.
type LLMStatus interface {
	Status() bot.Status
}

// AdminOptions configures the monitoring endpoints.
type AdminOptions struct {
	IDs         []string // admin identities: Telegram user ids or "dev:<id>"
	Auth        TelegramAuth
	LLM         LLMStatus // nil when no model is configured
	BotsEnabled bool
	FreeOnly    bool
	Discover    bool
}

type adminRoom struct {
	ID        string        `json:"id"`
	Status    string        `json:"status"`
	Bout      int           `json:"bout"`
	Players   []adminPlayer `json:"players"`
	UpdatedAt int64         `json:"updated_at"`
	Version   int64         `json:"version"`
	Humans    int           `json:"humans"`
}

type adminPlayer struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Bot       bool   `json:"bot"`
	Connected bool   `json:"connected"`
	Hand      int    `json:"hand"`
	Stump     int    `json:"stump"`
	XP        int    `json:"xp"`
	Out       bool   `json:"out"`
}

type adminOverview struct {
	Server struct {
		Started       time.Time `json:"started"`
		UptimeSeconds int64     `json:"uptime_seconds"`
		Go            string    `json:"go"`
		LocalRooms    int       `json:"local_rooms"`
		LocalClients  int       `json:"local_clients"`
	} `json:"server"`
	Counters map[string]int64  `json:"counters"`
	LLM      *bot.Status       `json:"llm"`
	Bots     map[string]any    `json:"bots"`
	Rooms    []adminRoom       `json:"rooms"`
	Users    []store.UserStats `json:"users"`
	Events   []monitor.Event   `json:"events"`
	Logs     []monitor.LogLine `json:"logs"`
}

// AdminHandler serves /api/admin/*: a yes/no on the caller's admin status,
// an overview of the server and the full (unsanitized) state of one room.
// The caller proves who they are with Telegram init data (query init_data
// or header X-Init-Data) or, with ALLOW_DEV_AUTH, a dev id.
func (h *Hub) AdminHandler(opts AdminOptions) http.Handler {
	admins := map[string]bool{}
	for _, id := range opts.IDs {
		if id = strings.TrimSpace(id); id != "" {
			admins[id] = true
		}
	}
	identify := func(r *http.Request) (Identity, bool) {
		q := r.URL.Query()
		init := strings.TrimSpace(q.Get("init_data"))
		if init == "" {
			init = strings.TrimSpace(r.Header.Get("X-Init-Data"))
		}
		p := JoinPayload{TgInitData: init}
		if init == "" {
			dev := strings.TrimSpace(q.Get("dev_id"))
			if dev == "" {
				return Identity{}, false
			}
			p.DevUser = &DevUser{ID: dev, Name: "admin"}
		}
		id, err := opts.Auth.Authenticate(p)
		if err != nil {
			return Identity{}, false
		}
		return id, admins[id.ID]
	}
	writeJSON := func(w http.ResponseWriter, status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, isAdmin := identify(r)
		switch r.URL.Path {
		case "/api/admin/me":
			writeJSON(w, http.StatusOK, map[string]any{"admin": isAdmin, "id": id.ID})
			return
		}
		if !isAdmin {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "not an admin"})
			return
		}
		switch r.URL.Path {
		case "/api/admin/overview":
			writeJSON(w, http.StatusOK, h.adminOverview(r, opts))
		case "/api/admin/room":
			state, err := h.store.Load(r.Context(), strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("id"))))
			if err != nil {
				writeJSON(w, http.StatusNotFound, map[string]any{"error": "room not found"})
				return
			}
			writeJSON(w, http.StatusOK, state)
		default:
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "unknown admin endpoint"})
		}
	})
}

func (h *Hub) adminOverview(r *http.Request, opts AdminOptions) adminOverview {
	var o adminOverview
	snap := h.mon.Snapshot(150, 200)
	o.Server.Started = snap.Started
	o.Server.UptimeSeconds = snap.UptimeSeconds
	o.Server.Go = runtime.Version()
	o.Server.LocalRooms, o.Server.LocalClients = h.localCounts()
	o.Counters = snap.Counters
	o.Events = snap.Events
	o.Logs = snap.Logs
	if opts.LLM != nil {
		st := opts.LLM.Status()
		o.LLM = &st
	}
	o.Bots = map[string]any{"enabled": opts.BotsEnabled, "free_only": opts.FreeOnly, "discover": opts.Discover, "llm": opts.LLM != nil}
	rooms, err := h.store.ListRooms(r.Context())
	if err != nil {
		h.log.Warn("admin: list rooms", "err", err)
	}
	o.Rooms = make([]adminRoom, 0, len(rooms))
	for _, s := range rooms {
		o.Rooms = append(o.Rooms, summarizeRoom(s))
	}
	users, err := h.store.TopUsers(r.Context(), 50)
	if err != nil {
		h.log.Warn("admin: top users", "err", err)
	}
	o.Users = users
	if o.Users == nil {
		o.Users = []store.UserStats{}
	}
	return o
}

func summarizeRoom(s *game.GameState) adminRoom {
	room := adminRoom{ID: s.RoomID, Status: s.Status, Bout: s.BoutNumber, UpdatedAt: s.UpdatedAt, Version: s.Version, Humans: s.HumanCount()}
	for _, p := range s.Players {
		room.Players = append(room.Players, adminPlayer{ID: p.ID, Name: p.Name, Bot: p.IsBot, Connected: p.Connected, Hand: len(p.Hand), Stump: len(p.Stump), XP: p.XP, Out: p.Out})
	}
	if room.Players == nil {
		room.Players = []adminPlayer{}
	}
	return room
}

// localCounts reports the rooms and connections this instance serves.
func (h *Hub) localCounts() (rooms, clients int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, sub := range h.rooms {
		rooms++
		clients += len(sub.clients)
	}
	return rooms, clients
}
