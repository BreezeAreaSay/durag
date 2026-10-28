package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/breezeareasay/durag/backend/internal/bot"
	"github.com/breezeareasay/durag/backend/internal/game"
	"github.com/breezeareasay/durag/backend/internal/monitor"
	"github.com/breezeareasay/durag/backend/internal/store"
)

// Options configures a Hub.
type Options struct {
	// AllowedOrigins lists acceptable Origin headers; "*" or empty allows any.
	AllowedOrigins []string
	Logger         *slog.Logger
	Bots           BotOptions
	// Monitor collects counters, events and log lines for the admin panel
	// (optional: a fresh recorder is created when nil).
	Monitor *monitor.Recorder
	// AdminIDs are the identities (Telegram user ids or "dev:<id>") that get
	// every premium reaction regardless of XP.
	AdminIDs []string
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

	bots     BotOptions
	botMu    sync.Mutex
	botRooms map[string]*botRoom
	mon      *monitor.Recorder
	admins   map[string]bool

	flyMu sync.Mutex
	flies map[string]*fly // room id -> the fly in the air
	talk  talkBudget
}

// isAdmin tells whether a player id is listed in ADMIN_IDS.
func (h *Hub) isAdmin(playerID string) bool { return h.admins[playerID] }

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
	Fly      *FlyPayload      `json:"fly,omitempty"`
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
		store:    st,
		engine:   engine,
		auth:     auth,
		log:      logger,
		ctx:      ctx,
		cancel:   cancel,
		rooms:    map[string]*roomSub{},
		bots:     opts.Bots,
		botRooms: map[string]*botRoom{},
		flies:    map[string]*fly{},
		mon:      opts.Monitor,
	}
	h.upgrader = websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin:     originChecker(opts.AllowedOrigins),
	}
	h.admins = map[string]bool{}
	for _, id := range opts.AdminIDs {
		if id = strings.TrimSpace(id); id != "" {
			h.admins[id] = true
		}
	}
	if h.mon == nil {
		h.mon = monitor.New()
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
	case TypeSay:
		h.say(c, env.Payload)
	case TypeCatchFly:
		h.catchFly(c, env.Payload)
	case TypePlayCard, TypeTransferTurn, TypeTakeCards, TypePass, TypeResolveBout, TypeTakeStump, TypeReady, TypeLeaveRoom, TypeAddBot, TypeRemoveBot, TypeSwapBot:
		h.intent(c, env)
	case TypeKick:
		h.kick(c, env.Payload)
	case TypeWipe:
		h.wipe(c, env.Payload)
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
	stats, _ := h.store.UserStats(h.ctx, id.ID)
	wins, werr := h.store.Wins(h.ctx, id.ID)
	if werr != nil {
		h.log.Warn("ws: wins lookup failed", "player", id.ID, "err", werr)
	}
	prof, perr := h.store.Profile(h.ctx, id.ID)
	if perr != nil {
		h.log.Warn("ws: profile lookup failed", "player", id.ID, "err", perr)
	}
	// the name at the table is the one typed in the lobby; a dev identity
	// may fall back on its own name (tests, scripts), a Telegram one never
	// shows its Telegram name or photo
	name := nickname(p.Nickname)
	if name == "" && strings.HasPrefix(id.ID, "dev:") {
		name = nickname(id.Name)
	}
	if name == "" {
		c.sendError("NICKNAME_REQUIRED", "pick a name for the table: 2 to 20 letters or digits", "")
		h.removeFromRoom(c)
		c.setRoom("", "")
		return
	}
	state, err := h.store.Update(h.ctx, roomID, p.Create, func(s *game.GameState) error {
		if p.Create && len(s.Players) == 0 && s.Status == game.StatusWaiting {
			s.NoTimer = p.NoTimer // the table's own setting, chosen by whoever opens it
		}
		if err := h.engine.Join(s, id.ID, name, ""); err != nil {
			return err
		}
		if pl := s.Player(id.ID); pl != nil {
			pl.AvatarURL = ""
			pl.Avatar = foodAvatar(id.ID)
			pl.XP = stats.XP
			pl.Admin = h.isAdmin(id.ID)
			pl.Wins = wins
			pl.Scar = prof.Scar
		}
		return nil
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
	h.mon.Inc("joins")
	h.afterUpdate(roomID, state)
	if len(replaced) == 0 && hasBots(state) {
		h.botGreet(roomID, state, []string{id.ID}) // the bots know who just sat down
	}
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
		case TypeAddBot:
			if h.bots.Disabled {
				return errBotsDisabled
			}
			var p AddBotPayload
			if len(env.Payload) > 0 {
				_ = json.Unmarshal(env.Payload, &p) // a bad payload is just a normal bot
			}
			id := newBotID()
			ch := pickCharacter(s)
			if err := h.engine.AddBot(s, id, ch.Name, ch.Avatar); err != nil {
				return err
			}
			if seat := s.Player(id); seat != nil {
				seat.BotLevel = string(bot.ParseLevel(p.Level))
			}
			return nil
		case TypeRemoveBot:
			var p RemoveBotPayload
			if err := json.Unmarshal(env.Payload, &p); err != nil {
				return errBadPayload
			}
			return h.engine.RemoveBot(s, p.PlayerID)
		case TypeSwapBot:
			var p KickPayload
			if err := json.Unmarshal(env.Payload, &p); err != nil {
				return errBadPayload
			}
			return swapBot(s, p.PlayerID)
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
	h.mon.Inc("intents")
	if env.Type == TypeLeaveRoom {
		h.removeFromRoom(c)
		c.setRoom("", "")
		if state.HumanCount() == 0 { // nobody or only bots left: the table is gone
			_ = h.store.Delete(h.ctx, roomID)
			h.dropBotRoom(roomID)
			return
		}
	}
	h.afterUpdate(roomID, state)
	if env.Type == TypeAddBot {
		var humans []string
		for _, p := range state.Players {
			if !p.IsBot && p.Connected {
				humans = append(humans, p.ID)
			}
		}
		h.botGreet(roomID, state, humans) // the new bot looks around the table
	}
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
	payload := ReactionPayload{PlayerID: playerID, Emoji: p.Emoji, TS: now.UnixMilli()}
	if prem, ok := PremiumEmoji[p.Emoji]; ok {
		xp, wins := h.standing(roomID, playerID)
		if !prem.Unlocked(xp, wins, h.isAdmin(playerID)) {
			c.sendError("LOCKED_EMOJI", prem.Locked(), "")
			return
		}
		payload.Effect = prem.Effect
	}
	raw2, err := json.Marshal(pubMessage{Kind: "reaction", Reaction: &payload})
	if err != nil {
		return
	}
	if err := h.store.Publish(h.ctx, roomID, raw2); err != nil {
		h.log.Error("ws: publish reaction", "err", err)
	}
	h.botHearEmoji(roomID, playerID, p.Emoji)
}

// wipe records a stain rubbed off the cloth, for everybody at the table.
func (h *Hub) wipe(c *Client, raw json.RawMessage) {
	roomID, _ := c.room()
	if roomID == "" {
		c.sendError("NOT_IN_ROOM", "join a room first", "")
		return
	}
	var p WipePayload
	if err := json.Unmarshal(raw, &p); err != nil || p.Index < 0 || p.Index > 200 {
		c.sendError("BAD_PAYLOAD", "malformed WIPE payload", "")
		return
	}
	state, err := h.store.Update(h.ctx, roomID, false, func(s *game.GameState) error {
		for _, i := range s.WipedStains {
			if i == p.Index {
				return errStale // already clean
			}
		}
		if len(s.WipedStains) >= 200 {
			return errStale
		}
		s.WipedStains = append(s.WipedStains, p.Index)
		s.Touch()
		return nil
	})
	if err != nil {
		return
	}
	h.publish(roomID, state)
}

// kick removes a player from the lobby: the host's and the admins' right.
// A bot's seat is freed; a person is shown the door (KICKED) and their
// connection leaves the room.
func (h *Hub) kick(c *Client, raw json.RawMessage) {
	roomID, playerID := c.room()
	if roomID == "" {
		c.sendError("NOT_IN_ROOM", "join a room first", "")
		return
	}
	var p KickPayload
	if err := json.Unmarshal(raw, &p); err != nil || p.PlayerID == "" || p.PlayerID == playerID {
		c.sendError("BAD_PAYLOAD", "malformed KICK payload", "")
		return
	}
	var victim *game.Player
	state, err := h.store.Update(h.ctx, roomID, false, func(s *game.GameState) error {
		if s.Status != game.StatusWaiting {
			return game.ErrNotWaiting
		}
		if s.HostID != playerID && !h.isAdmin(playerID) {
			return &game.RuleError{Code: "NOT_HOST", Message: "only the host or an admin can remove players"}
		}
		pl := s.Player(p.PlayerID)
		if pl == nil {
			return game.ErrUnknownPlayer
		}
		cp := *pl
		victim = &cp
		if pl.IsBot {
			return h.engine.RemoveBot(s, p.PlayerID)
		}
		return h.engine.Leave(s, p.PlayerID)
	})
	if err != nil {
		h.sendFailure(c, err, "")
		return
	}
	if victim != nil && !victim.IsBot {
		h.mu.Lock()
		var doors []*Client
		if sub, ok := h.rooms[roomID]; ok {
			for other := range sub.clients {
				if _, pid := other.room(); pid == p.PlayerID {
					doors = append(doors, other)
				}
			}
		}
		h.mu.Unlock()
		for _, other := range doors {
			other.sendError("KICKED", "the host showed you the door", "")
			h.removeFromRoom(other)
			other.setRoom("", "")
		}
	}
	h.afterUpdate(roomID, state)
}

// swapBot gives a bot seat the next free character (a tap on its face).
func swapBot(s *game.GameState, id string) error {
	if s.Status != game.StatusWaiting {
		return game.ErrNotWaiting
	}
	pl := s.Player(id)
	if pl == nil {
		return game.ErrUnknownPlayer
	}
	if !pl.IsBot {
		return game.ErrNotABot
	}
	used := map[string]bool{}
	for _, q := range s.Players {
		used[q.Name] = true
	}
	cur := -1
	for i, ch := range bot.Characters {
		if ch.Name == pl.Name {
			cur = i
		}
	}
	for step := 1; step <= len(bot.Characters); step++ {
		ch := bot.Characters[(cur+step)%len(bot.Characters)]
		if !used[ch.Name] {
			pl.Name, pl.Avatar = ch.Name, ch.Avatar
			s.Touch()
			return nil
		}
	}
	return &game.RuleError{Code: "NO_FREE_CHARACTER", Message: "every character is already at the table"}
}

// standing reads a player's XP and wins off the room state.
func (h *Hub) standing(roomID, playerID string) (xp, wins int) {
	if state, err := h.store.Load(h.ctx, roomID); err == nil {
		if pl := state.Player(playerID); pl != nil {
			return pl.XP, pl.Wins
		}
	}
	return 0, 0
}

// say drops one of the quick phrases on the table: it travels as a
// reaction with text, like a bot's line, and needs PhraseWins wins.
func (h *Hub) say(c *Client, raw json.RawMessage) {
	roomID, playerID := c.room()
	if roomID == "" {
		c.sendError("NOT_IN_ROOM", "join a room first", "")
		return
	}
	var p SayPayload
	if err := json.Unmarshal(raw, &p); err != nil || !allowedPhraseSet[p.Text] {
		c.sendError("BAD_PHRASE", "unknown phrase", "")
		return
	}
	_, wins := h.standing(roomID, playerID)
	if wins < PhraseWins && !h.isAdmin(playerID) {
		c.sendError("LOCKED_PHRASE", fmt.Sprintf("quick phrases unlock at %d wins", PhraseWins), "")
		return
	}
	now := time.Now()
	if !c.allowReact(now, reactMinInterval) {
		return
	}
	raw2, err := json.Marshal(pubMessage{Kind: "reaction", Reaction: &ReactionPayload{PlayerID: playerID, Text: p.Text, TS: now.UnixMilli()}})
	if err != nil {
		return
	}
	if err := h.store.Publish(h.ctx, roomID, raw2); err != nil {
		h.log.Error("ws: publish phrase", "err", err)
	}
	h.botHearText(roomID, playerID, p.Text)
}

// afterUpdate broadcasts the new state and schedules the next server-side
// deadline of the room: the end of the resolving pause, the automatic stump
// pickup or the turn timer, whichever comes first.
func (h *Hub) afterUpdate(roomID string, state *game.GameState) {
	if state.Status == game.StatusFinished && !state.WinAwarded {
		state = h.awardWin(roomID, state)
	}
	h.publish(roomID, state)
	h.scheduleTick(roomID, state)
	h.scheduleBots(roomID, state)
	if last := lastLogType(state); last == "start" {
		h.scheduleFly(roomID)
		if hasBots(state) {
			h.botRoomFor(roomID).freshGame()
		}
		h.mon.Inc("games.started")
		h.mon.Event("game.start", roomID, "players", len(state.Players), "humans", state.HumanCount())
	} else if last == "finish" && state.Status == game.StatusFinished {
		go h.recordFinish(roomID, state)
	}
}

func lastLogType(state *game.GameState) string {
	if len(state.Log) == 0 {
		return ""
	}
	return state.Log[len(state.Log)-1].Type
}

// recordFinish credits the players' lifetime stats exactly once per finished
// game (keyed by room and state version) and pushes the new XP into the room.
func (h *Hub) recordFinish(roomID string, state *game.GameState) {
	var results []store.GameResult
	for _, p := range state.Players {
		if p.IsBot {
			continue
		}
		results = append(results, store.GameResult{ID: p.ID, Name: p.Name, Won: p.ID != state.LoserID})
	}
	if len(results) == 0 {
		return
	}
	key := roomID + ":" + strconv.FormatInt(state.Version, 10)
	applied, err := h.store.RecordGame(h.ctx, key, results)
	if err != nil {
		h.log.Warn("stats: record game", "room", roomID, "err", err)
		return
	}
	if !applied {
		return
	}
	h.mon.Inc("games.finished")
	loser := state.LoserID
	if p := state.Player(loser); p != nil {
		loser = p.Name
	}
	h.mon.Event("game.finish", roomID, "loser", loser, "bout", state.BoutNumber, "players", len(state.Players))
	xp := map[string]int{}
	for _, r := range results {
		if st, err := h.store.UserStats(h.ctx, r.ID); err == nil {
			xp[r.ID] = st.XP
		}
	}
	fresh, err := h.store.Update(h.ctx, roomID, false, func(s *game.GameState) error {
		for i := range s.Players {
			if v, ok := xp[s.Players[i].ID]; ok {
				s.Players[i].XP = v
			}
		}
		return nil
	})
	if err == nil {
		h.publish(roomID, fresh)
	}
	// the victory joke: winners who earned the fanfare tier get it with a line
	fanfare := PremiumEmoji["🏆"]
	for _, r := range results {
		if r.Won && (xp[r.ID] >= fanfare.XP || h.isAdmin(r.ID)) {
			line := VictoryLines[rand.IntN(len(VictoryLines))]
			raw, err := json.Marshal(pubMessage{Kind: "reaction", Reaction: &ReactionPayload{PlayerID: r.ID, Emoji: "🏆", Text: line, Effect: fanfare.Effect, TS: time.Now().UnixMilli()}})
			if err == nil {
				_ = h.store.Publish(h.ctx, roomID, raw)
			}
		}
	}
}

// awardWin counts the win of a finished game exactly once: the first player
// out of the game gets +1 on their lifetime counter (bots and draws get
// nothing). The flag and the counter move together under the room lock, so
// two backend instances cannot both count the same game.
func (h *Hub) awardWin(roomID string, state *game.GameState) *game.GameState {
	next, err := h.store.Update(h.ctx, roomID, false, func(s *game.GameState) error {
		if s.Status != game.StatusFinished || s.WinAwarded {
			return errStale
		}
		s.WinAwarded = true
		now := time.Now().Unix()
		winnerID := s.Winner()
		for i := range s.Players {
			p := &s.Players[i]
			if p.IsBot {
				// a bot that lost at this table comes back a level stronger
				if p.ID == s.LoserID {
					next := bot.Promote(bot.ParseLevel(p.BotLevel))
					if string(next) != p.BotLevel {
						p.BotLevel = string(next)
						h.botPromote(roomID, p.ID, next)
					}
				}
				continue
			}
			prof, err := h.store.Profile(h.ctx, p.ID)
			if err != nil {
				return err
			}
			prof.Fold(p.Stats, p.ID == s.LoserID, p.ID == winnerID, now)
			if err := h.store.SaveProfile(h.ctx, p.ID, prof); err != nil {
				return err
			}
			p.Scar = prof.Scar
			h.dossier(roomID, p) // refresh what the bots know
		}
		winner := s.Player(winnerID)
		if winner == nil || winner.IsBot {
			return nil
		}
		n, err := h.store.AddWin(h.ctx, winner.ID)
		if err != nil {
			return err
		}
		winner.Wins = n
		return nil
	})
	if err != nil {
		if !errors.Is(err, errStale) {
			h.log.Warn("ws: award win failed", "room", roomID, "err", err)
		}
		return state
	}
	return next
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
	roomID, playerID := c.room()
	switch {
	case errors.As(err, &rule):
		h.mon.Inc("errors.rule")
		h.mon.Event("rule_error", rule.Code, "room", roomID, "player", playerID, "card", cardID)
		c.sendError(rule.Code, rule.Message, cardID)
	case errors.Is(err, errBadPayload):
		c.sendError("BAD_PAYLOAD", "malformed payload", cardID)
	case errors.Is(err, store.ErrNotFound):
		c.sendError("ROOM_NOT_FOUND", "the room no longer exists", cardID)
	default:
		h.mon.Inc("errors.internal")
		h.mon.Event("internal_error", err.Error(), "room", roomID, "player", playerID)
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
		case msg.Kind == "fly" && msg.Fly != nil:
			frame := encode(TypeFly, msg.Fly)
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
	if state.HumanCount() == 0 {
		_ = h.store.Delete(h.ctx, roomID)
		h.dropBotRoom(roomID)
		return
	}
	h.afterUpdate(roomID, state)
}

// LocalRooms returns the number of rooms with local connections (metrics/tests).
func (h *Hub) LocalRooms() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.rooms)
}
