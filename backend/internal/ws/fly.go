package ws

import (
	"encoding/json"
	"math/rand/v2"
	"strconv"
	"time"

	"github.com/breezeareasay/durag/backend/internal/game"
)

// A fly now and then crosses the table during a game; the first human to
// slap it gets a point of experience. The server decides when it appears
// and who was first, so the point is earned at the table, not in the
// browser. The fly lives on the instance that launched it: a slap that
// reaches another instance misses, which is what a fly does.
var (
	FlyChance   = 0.35             // games that get a fly
	FlyDelayMin = 20 * time.Second // after the start
	FlyDelayMax = 90 * time.Second
	FlyLifetime = 9 * time.Second // how long it stays catchable
)

type fly struct {
	id     string
	caught bool
}

// scheduleFly maybe launches a fly into the room some time after the start.
func (h *Hub) scheduleFly(roomID string) {
	if rand.Float64() >= FlyChance {
		return
	}
	delay := FlyDelayMin
	if span := FlyDelayMax - FlyDelayMin; span > 0 {
		delay += time.Duration(rand.Int64N(int64(span)))
	}
	time.AfterFunc(delay, func() {
		select {
		case <-h.ctx.Done():
			return
		default:
		}
		h.launchFly(roomID)
	})
}

// launchFly publishes the fly when the game is still on and no fly is in the air.
func (h *Hub) launchFly(roomID string) {
	state, err := h.store.Load(h.ctx, roomID)
	if err != nil || state.Status != game.StatusPlaying || state.HumanCount() == 0 {
		return
	}
	f := &fly{id: strconv.FormatUint(rand.Uint64(), 36)}
	h.flyMu.Lock()
	if _, busy := h.flies[roomID]; busy {
		h.flyMu.Unlock()
		return
	}
	h.flies[roomID] = f
	h.flyMu.Unlock()
	payload := FlyPayload{ID: f.id, Seed: rand.Int64N(1 << 31), TS: time.Now().UnixMilli()}
	if raw, err := json.Marshal(pubMessage{Kind: "fly", Fly: &payload}); err == nil {
		if err := h.store.Publish(h.ctx, roomID, raw); err != nil {
			h.log.Error("ws: publish fly", "err", err)
		}
	}
	time.AfterFunc(FlyLifetime, func() {
		h.flyMu.Lock()
		if h.flies[roomID] == f {
			delete(h.flies, roomID)
		}
		h.flyMu.Unlock()
	})
}

// catchFly credits the first human who slaps the fly in time.
func (h *Hub) catchFly(c *Client, raw json.RawMessage) {
	roomID, playerID := c.room()
	if roomID == "" {
		c.sendError("NOT_IN_ROOM", "join a room first", "")
		return
	}
	var p CatchFlyPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		c.sendError("BAD_PAYLOAD", "malformed CATCH_FLY payload", "")
		return
	}
	h.flyMu.Lock()
	f := h.flies[roomID]
	first := f != nil && f.id == p.ID && !f.caught
	if first {
		f.caught = true
	}
	h.flyMu.Unlock()
	if !first {
		return // too late: somebody was quicker, or the fly flew off
	}
	xp, err := h.store.AddXP(h.ctx, playerID, 1)
	if err != nil {
		h.log.Warn("ws: fly xp", "player", playerID, "err", err)
		return
	}
	h.mon.Inc("flies.caught")
	state, err := h.store.Update(h.ctx, roomID, false, func(s *game.GameState) error {
		if pl := s.Player(playerID); pl != nil {
			pl.XP = xp
		}
		s.FliesDead++ // it stays on the cloth
		return nil
	})
	if err == nil {
		h.afterUpdate(roomID, state) // a new version: the bots' timers are re-armed for it
	}
	payload := FlyPayload{ID: p.ID, TS: time.Now().UnixMilli(), CaughtBy: playerID, XP: xp}
	if raw, err := json.Marshal(pubMessage{Kind: "fly", Fly: &payload}); err == nil {
		_ = h.store.Publish(h.ctx, roomID, raw)
	}
}
