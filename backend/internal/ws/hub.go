package ws

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/breezeareasay/durag/backend/internal/game"
	"github.com/breezeareasay/durag/backend/internal/store"
)

// Options configures a Hub.
type Options struct {
	// AllowedOrigins lists acceptable Origin headers; "*" or empty allows any.
	AllowedOrigins []string
	Logger         *slog.Logger
}

// Hub owns every local connection, routes intents to the engine and relays
// published state to the connections of each room.
type Hub struct {
	store    store.Store
	engine   *game.Engine
	auth     Authenticator
	log      *slog.Logger
	upgrader websocket.Upgrader

	ctx    context.Context
	cancel context.CancelFunc

	mu    sync.Mutex
	rooms map[string]*roomSub
}

type roomSub struct {
	clients map[*Client]struct{}
	cancel  context.CancelFunc
}

// pubMessage is what travels through the store's Pub/Sub channel between
// backend instances: either a full (private) state or a transient event.
type pubMessage struct {
	Kind     string           `json:"kind"` // "state" | "reaction"
	State    *game.GameState  `json:"state,omitempty"`
	Reaction *ReactionPayload `json:"reaction,omitempty"`
}

const reactMinInterval = 1200 * time.Millisecond

var roomIDPattern = regexp.MustCompile(`^[A-Z0-9_-]{1,32}$`)

// NewHub creates a hub.
func NewHub(st store.Store, engine *game.Engine, auth Authenticator, opts Options) *Hub {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	h := &Hub{
		store:  st,
		engine: engine,
		auth:   auth,
		log:    logger,
		ctx:    ctx,
		cancel: cancel,
		rooms:  map[string]*roomSub{},
	}
	h.upgrader = websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin:     originChecker(opts.AllowedOrigins),
	}
	return h
}

func originChecker(allowed []string) func(*http.Request) bool {
	if len(allowed) == 0 {
		return func(*http.Request) bool { return true }
	}
	set := map[string]bool{}
	for _, a := range allowed {
		a = strings.TrimSpace(strings.ToLower(a))
		if a == "*" {
			return func(*http.Request) bool { return true }
		}
		set[strings.TrimSuffix(a, "/")] = true
	}
	return func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true // non-browser clients
		}
		u, err := url.Parse(origin)
		if err != nil {
			return false
		}
		if set[strings.ToLower(u.Scheme+"://"+u.Host)] {
			return true
		}
		// same host as the request (frontend served by the same Nginx)
		return strings.EqualFold(u.Host, r.Host)
	}
}

// Close stops every room subscription and disconnects all clients.
func (h *Hub) Close() {
	h.cancel()
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, sub := range h.rooms {
		sub.cancel()
		for c := range sub.clients {
			c.close()
		}
		delete(h.rooms, id)
	}
}

// ServeHTTP upgrades the connection and runs the pumps.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.log.Debug("ws: upgrade failed", "err", err)
		return
	}
	c := newClient(h, conn)
	go c.writePump()
	go c.readPump()
}

// --- message dispatch ----------------------------------------------------

func (h *Hub) handle(c *Client, env Envelope) {
	switch env.Type {
	case TypeJoinRoom:
		h.join(c, env.Payload)
	case TypePing:
		c.enqueue(encode(TypePong, nil))
	case TypeReact, TypeSendEmoji:
		h.react(c, env.Payload)
	case TypePlayCard, TypeTransferTurn, TypeTakeCards, TypePass, TypeResolveBout, TypeTakeStump, TypeReady, TypeLeaveRoom:
		h.intent(c, env)
	default:
		c.sendError("UNKNOWN_TYPE", "unknown message type: "+env.Type, "")
	}
}

func (h *Hub) join(c *Client, raw json.RawMessage) {
	var p JoinPayload
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &p); err != nil {
			c.sendError("BAD_PAYLOAD", "malformed JOIN_ROOM payload", "")
			return
		}
	}
	roomID := strings.ToUpper(strings.TrimSpace(p.RoomID))
	if !roomIDPattern.MatchString(roomID) {
		c.sendError("BAD_ROOM_ID", "room id must be 1-32 characters: letters, digits, '-' or '_'", "")
		return
	}
	id, err := h.auth.Authenticate(p)
	if err != nil {
		h.log.Info("ws: auth failed", "err", err)
		c.sendError("UNAUTHORIZED", "authentication failed: "+err.Error(), "")
		return
	}

	// Leave the previous room, if any.
	if prevRoom, prevPlayer := c.room(); prevRoom != "" {
		h.leaveRoom(c, prevRoom, prevPlayer, false)
	}

	c.setRoom(roomID, id.ID)
	replaced := h.addToRoom(c)
	state, err := h.store.Update(h.ctx, roomID, p.Create, func(s *game.GameState) error {
		return h.engine.Join(s, id.ID, id.Name, id.AvatarURL)
	})
	if err != nil {
		h.removeFromRoom(c)
		c.setRoom("", "")
		switch {
		case errors.Is(err, store.ErrNotFound):
			c.sendError("ROOM_NOT_FOUND", "room "+roomID+" does not exist", "")
		default:
			h.sendFailure(c, err, "")
		}
		return
	}
	for _, old := range replaced {
		old.sendError("REPLACED", "you connected from another device", "")
		old.close()
	}
	h.afterUpdate(roomID, state)
}

func (h *Hub) intent(c *Client, env Envelope) {
	roomID, playerID := c.room()
	if roomID == "" {
		c.sendError("NOT_IN_ROOM", "join a room first", "")
		return
	}
	// Overdue timers (bout resolution, stump auto-take, turn timeout) fire
	// first: a safety net in case the instance that scheduled them is gone.
	h.tickDue(roomID)
	var cardID string
	apply := func(s *game.GameState) error {
		switch env.Type {
		case TypePlayCard:
			var p PlayCardPayload
			if err := json.Unmarshal(env.Payload, &p); err != nil {
				return errBadPayload
			}
			cardID = p.CardID
			return h.engine.PlayCard(s, playerID, p.CardID, p.TargetCardID)
		case TypeTransferTurn:
			var p TransferPayload
			if err := json.Unmarshal(env.Payload, &p); err != nil {
				return errBadPayload
			}
			cardID = p.CardID
			return h.engine.TransferTurn(s, playerID, p.CardID)
		case TypeTakeCards:
			return h.engine.TakeCards(s, playerID)
		case TypePass, TypeResolveBout:
			return h.engine.Pass(s, playerID)
		case TypeTakeStump:
			return h.engine.TakeStump(s, playerID)
		case TypeReady:
			p := ReadyPayload{Ready: true}
			if len(env.Payload) > 0 {
				if err := json.Unmarshal(env.Payload, &p); err != nil {
					return errBadPayload
				}
			}
			return h.engine.SetReady(s, playerID, p.Ready)
		case TypeLeaveRoom:
			return h.engine.Leave(s, playerID)
		}
		return errBadPayload
	}

	state, err := h.store.Update(h.ctx, roomID, false, apply)
	if err != nil {
		// Per spec: reply with ERROR and immediately re-send the current state
		// so the client can roll back its optimistic animation.
		h.sendFailure(c, err, cardID)
		h.resendState(c, roomID, playerID)
		return
	}
	if env.Type == TypeLeaveRoom {
		h.removeFromRoom(c)
		c.setRoom("", "")
		if len(state.Players) == 0 {
			_ = h.store.Delete(h.ctx, roomID)
			return
		}
	}
	h.afterUpdate(roomID, state)
}

var (
	errBadPayload = errors.New("malformed payload")
	errNotDue     = errors.New("bout not due for resolution")
)

// react broadcasts an emoji reaction to the room. Reactions are not part of
// the game state: they are transient and never persisted.
func (h *Hub) react(c *Client, raw json.RawMessage) {
	roomID, playerID := c.room()
	if roomID == "" {
		c.sendError("NOT_IN_ROOM", "join a room first", "")
		return
	}
	var p ReactPayload
	if err := json.Unmarshal(raw, &p); err != nil || !allowedEmojiSet[p.Emoji] {
		c.sendError("BAD_EMOJI", "unknown reaction", "")
		return
	}
	now := time.Now()
	if !c.allowReact(now, reactMinInterval) {
		return // silently dropped: spam protection
	}
	raw2, err := json.Marshal(pubMessage{Kind: "reaction", Reaction: &ReactionPayload{PlayerID: playerID, Emoji: p.Emoji, TS: now.UnixMilli()}})
	if err != nil {
		return
	}
	if err := h.store.Publish(h.ctx, roomID, raw2); err != nil {
		h.log.Error("ws: publish reaction", "err", err)
	}
}

// afterUpdate broadcasts the new state and schedules the next server-side
// deadline of the room: the end of the resolving pause, the automatic stump
// pickup or the turn timer, whichever comes first.
func (h *Hub) afterUpdate(roomID string, state *game.GameState) {
	h.publish(roomID, state)
	h.scheduleTick(roomID, state)
}

// nextDeadline returns the earliest pending server deadline of a state (unix ms) or 0.
func nextDeadline(state *game.GameState) int64 {
	var next int64
	consider := func(ms int64) {
		if ms > 0 && (next == 0 || ms < next) {
			next = ms
		}
	}
	if state.Phase == game.PhaseResolving {
		consider(state.ResolveAt)
	}
	if state.StumpsPending() {
		consider(state.StumpDeadline)
	}
	consider(state.TurnDeadline)
	return next
}

func (h *Hub) scheduleTick(roomID string, state *game.GameState) {
	at := nextDeadline(state)
	if at == 0 {
		return
	}
	delay := time.Until(time.UnixMilli(at)) + 20*time.Millisecond
	if delay < 0 {
		delay = 0
	}
	time.AfterFunc(delay, func() {
		select {
		case <-h.ctx.Done():
			return
		default:
		}
		h.tickDue(roomID)
	})
}

// tickDue fires every overdue deadline of the room (resolution, stumps, turn
// timer). It is idempotent: other instances' timers and incoming intents may
// race, and the engine only acts when a deadline really passed.
func (h *Hub) tickDue(roomID string) {
	state, err := h.store.Update(h.ctx, roomID, false, func(s *game.GameState) error {
		now := time.Now()
		changed := h.engine.ResolveIfDue(s, now)
		changed = h.engine.StumpsIfDue(s, now) || changed
		changed = h.engine.ExpireTurn(s, now) || changed
		if !changed {
			return errNotDue
		}
		return nil
	})
	if err != nil {
		if !errors.Is(err, errNotDue) && !errors.Is(err, store.ErrNotFound) {
			h.log.Error("ws: timers", "room", roomID, "err", err)
		}
		return
	}
	h.afterUpdate(roomID, state)
}

func (h *Hub) sendFailure(c *Client, err error, cardID string) {
	var rule *game.RuleError
	switch {
	case errors.As(err, &rule):
		c.sendError(rule.Code, rule.Message, cardID)
	case errors.Is(err, errBadPayload):
		c.sendError("BAD_PAYLOAD", "malformed payload", cardID)
	case errors.Is(err, store.ErrNotFound):
		c.sendError("ROOM_NOT_FOUND", "the room no longer exists", cardID)
	default:
		h.log.Error("ws: intent failed", "err", err)
		c.sendError("INTERNAL", "internal error", cardID)
	}
}

func (h *Hub) resendState(c *Client, roomID, playerID string) {
	state, err := h.store.Load(h.ctx, roomID)
	if err != nil {
		return
	}
	c.enqueue(encode(TypeStateUpdate, state.Sanitized(playerID)))
}

// publish broadcasts the (private) state through the store so every backend
// instance delivers a sanitized view to its own clients.
func (h *Hub) publish(roomID string, state *game.GameState) {
	raw, err := json.Marshal(pubMessage{Kind: "state", State: state})
	if err != nil {
		h.log.Error("ws: marshal state", "err", err)
		return
	}
	if err := h.store.Publish(h.ctx, roomID, raw); err != nil {
		h.log.Error("ws: publish", "err", err)
	}
}

// --- room membership -----------------------------------------------------

// addToRoom registers the client locally and starts the room subscription if
// it is the first local member. It returns other local connections of the
// same player, which the caller should close.
func (h *Hub) addToRoom(c *Client) []*Client {
	roomID, playerID := c.room()
	h.mu.Lock()
	defer h.mu.Unlock()
	sub, ok := h.rooms[roomID]
	if !ok {
		ctx, cancel := context.WithCancel(h.ctx)
		ch, err := h.store.Subscribe(ctx, roomID)
		if err != nil {
			cancel()
			h.log.Error("ws: subscribe", "room", roomID, "err", err)
			return nil
		}
		sub = &roomSub{clients: map[*Client]struct{}{}, cancel: cancel}
		h.rooms[roomID] = sub
		go h.relay(roomID, ch)
	}
	var replaced []*Client
	for other := range sub.clients {
		if _, pid := other.room(); pid == playerID && other != c {
			replaced = append(replaced, other)
			delete(sub.clients, other)
		}
	}
	sub.clients[c] = struct{}{}
	return replaced
}

func (h *Hub) removeFromRoom(c *Client) {
	roomID, _ := c.room()
	h.mu.Lock()
	defer h.mu.Unlock()
	sub, ok := h.rooms[roomID]
	if !ok {
		return
	}
	delete(sub.clients, c)
	if len(sub.clients) == 0 {
		sub.cancel()
		delete(h.rooms, roomID)
	}
}

// relay delivers every published message of a room to the local clients:
// states are sanitized per viewer, reactions go out verbatim.
func (h *Hub) relay(roomID string, ch <-chan []byte) {
	for raw := range ch {
		var msg pubMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			h.log.Error("ws: bad published message", "room", roomID, "err", err)
			continue
		}
		h.mu.Lock()
		sub, ok := h.rooms[roomID]
		var targets []*Client
		if ok {
			for c := range sub.clients {
				targets = append(targets, c)
			}
		}
		h.mu.Unlock()
		switch {
		case msg.Kind == "state" && msg.State != nil:
			for _, c := range targets {
				_, playerID := c.room()
				c.enqueue(encode(TypeStateUpdate, msg.State.Sanitized(playerID)))
			}
		case msg.Kind == "reaction" && msg.Reaction != nil:
			frame := encode(TypeReaction, msg.Reaction)
			for _, c := range targets {
				c.enqueue(frame)
			}
		}
	}
}

// onDisconnect is called when a connection's read loop ends.
func (h *Hub) onDisconnect(c *Client) {
	roomID, playerID := c.room()
	if roomID == "" {
		return
	}
	h.leaveRoom(c, roomID, playerID, true)
	c.setRoom("", "")
}

// leaveRoom removes the client locally and, unless another local connection
// of the same player is still in the room, tells the engine the player went
// away.
func (h *Hub) leaveRoom(c *Client, roomID, playerID string, dropped bool) {
	h.mu.Lock()
	sub, ok := h.rooms[roomID]
	stillThere := false
	if ok {
		delete(sub.clients, c)
		for other := range sub.clients {
			if _, pid := other.room(); pid == playerID {
				stillThere = true
				break
			}
		}
		if len(sub.clients) == 0 {
			sub.cancel()
			delete(h.rooms, roomID)
		}
	}
	h.mu.Unlock()
	if stillThere {
		return
	}
	state, err := h.store.Update(h.ctx, roomID, false, func(s *game.GameState) error {
		if dropped {
			return h.engine.Disconnect(s, playerID)
		}
		return h.engine.Leave(s, playerID)
	})
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			h.log.Error("ws: leave", "err", err)
		}
		return
	}
	if len(state.Players) == 0 {
		_ = h.store.Delete(h.ctx, roomID)
		return
	}
	h.publish(roomID, state)
}

// LocalRooms returns the number of rooms with local connections (metrics/tests).
func (h *Hub) LocalRooms() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.rooms)
}
