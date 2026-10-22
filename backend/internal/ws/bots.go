package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/breezeareasay/durag/backend/internal/bot"
	"github.com/breezeareasay/durag/backend/internal/game"
	"github.com/breezeareasay/durag/backend/internal/store"
)

// BotOptions configures the server-driven opponents (spec v10 §2).
type BotOptions struct {
	Disabled   bool          // ADD_BOT is rejected
	Brain      bot.Brain     // optional LLM brain; nil = rule-based only
	MinDelay   time.Duration // human-like pause before a bot acts
	MaxDelay   time.Duration
	LLMTimeout time.Duration // how long a model may think before the rules take over
}

var (
	errBotsDisabled = &game.RuleError{Code: "BOTS_DISABLED", Message: "bots are switched off on this server"}
	errStale        = errors.New("bot: the state moved on")
)

const (
	moodDuration      = 90 * time.Second // a provocation or mockery colours the play this long
	recentEmojiMax    = 8
	adminJokeFromBout = 2 // the fourth-wall line never comes before the second bout
)

// botRoom is what the hub remembers about the bots of one room: moods,
// recent emoji and whether the admin joke was told this game. It lives in
// the memory of the instance that handles the room's traffic.
type botRoom struct {
	mu         sync.Mutex
	moods      map[string]bot.Mood
	moodUntil  map[string]time.Time
	recent     []bot.EmojiEvent
	jokeSaid   map[string]bool
	lastStatus string
	scheduled  map[string]int64       // bot id -> state version a timer is armed for
	turns      map[string]int         // bot id -> moves made this game (banter cadence)
	memory     *bot.Memory            // what the bots remember about the cards
	dossiers   map[string]bot.Dossier // what the table remembers about the people (by player id)
	overheard  bot.Overheard          // the last line a bot said
	promoted   map[string]bot.Level   // bots that lost here and came back stronger, until they say so
}

func newBotRoom() *botRoom {
	return &botRoom{moods: map[string]bot.Mood{}, moodUntil: map[string]time.Time{}, jokeSaid: map[string]bool{}, scheduled: map[string]int64{}, turns: map[string]int{}, memory: bot.NewMemory(), dossiers: map[string]bot.Dossier{}, promoted: map[string]bot.Level{}}
}

func (h *Hub) botRoomFor(roomID string) *botRoom {
	h.botMu.Lock()
	defer h.botMu.Unlock()
	br, ok := h.botRooms[roomID]
	if !ok {
		br = newBotRoom()
		h.botRooms[roomID] = br
	}
	return br
}

func (h *Hub) dropBotRoom(roomID string) {
	h.botMu.Lock()
	delete(h.botRooms, roomID)
	h.botMu.Unlock()
}

func (br *botRoom) view(botID string, now time.Time) (bot.Mood, []bot.EmojiEvent, bool) {
	br.mu.Lock()
	defer br.mu.Unlock()
	mood := br.moods[botID]
	if mood == "" || now.After(br.moodUntil[botID]) {
		mood = bot.MoodNeutral
	}
	return mood, append([]bot.EmojiEvent{}, br.recent...), !br.jokeSaid[botID]
}

func (br *botRoom) markJoke(botID string) {
	br.mu.Lock()
	br.jokeSaid[botID] = true
	br.mu.Unlock()
}

func hasBots(s *game.GameState) bool {
	for i := range s.Players {
		if s.Players[i].IsBot {
			return true
		}
	}
	return false
}

func isBot(s *game.GameState, id string) bool {
	p := s.Player(id)
	return p != nil && p.IsBot
}

func legalMove(moves []game.Move, m game.Move) bool {
	for _, x := range moves {
		if x == m {
			return true
		}
	}
	return false
}

// newBotID returns a fresh seat id for a bot.
func newBotID() string { return fmt.Sprintf("bot:%08x", rand.Uint32()) }

// pickBotName hands out the next free name from bot.Names.
func pickBotName(s *game.GameState) string {
	used := map[string]bool{}
	for _, p := range s.Players {
		used[p.Name] = true
	}
	for _, n := range bot.Names {
		if !used[n] {
			return n
		}
	}
	return fmt.Sprintf("Бот-%d", len(s.Players)+1)
}

func (h *Hub) botDelay() time.Duration {
	lo, hi := h.bots.MinDelay, h.bots.MaxDelay
	if hi < lo {
		hi = lo
	}
	if hi <= 0 {
		return 0
	}
	return lo + time.Duration(rand.Int64N(int64(hi-lo)+1))
}

// scheduleBots arms a timer for every bot that has to act in this state.
// The timer carries the state version, so a bot acts at most once per
// state and never on a state it has not seen.
func (h *Hub) scheduleBots(roomID string, state *game.GameState) {
	if h.bots.Disabled || !hasBots(state) {
		return
	}
	br := h.botRoomFor(roomID)
	br.mu.Lock()
	defer br.mu.Unlock()
	if state.Status == game.StatusPlaying && br.lastStatus != game.StatusPlaying {
		br.jokeSaid = map[string]bool{} // a new game: the fourth wall is whole again
		br.turns = map[string]int{}
		br.memory.Reset()
	}
	br.lastStatus = state.Status
	br.memory.Observe(state)
	if state.Status != game.StatusPlaying {
		return
	}
	due := append([]string{}, state.Actors()...)
	due = append(due, state.StumpPending...)
	for _, id := range due {
		if !isBot(state, id) || br.scheduled[id] == state.Version {
			continue
		}
		br.scheduled[id] = state.Version
		botID, version := id, state.Version
		time.AfterFunc(h.botDelay(), func() {
			select {
			case <-h.ctx.Done():
				return
			default:
			}
			h.botAct(roomID, botID, version)
		})
	}
}

// botAct lets one bot make one move on the state version it was scheduled
// for. The move is decided outside the room lock (the model may take a
// while) and applied only if nobody moved in the meantime.
func (h *Hub) botAct(roomID, botID string, version int64) {
	state, err := h.store.Load(h.ctx, roomID)
	if err != nil || state.Version != version || state.Status != game.StatusPlaying {
		return
	}
	moves := state.LegalMoves(botID)
	if len(moves) == 0 {
		return
	}
	br := h.botRoomFor(roomID)
	mood, recent, jokeDue := br.view(botID, time.Now())
	br.mu.Lock()
	br.turns[botID]++
	turn := br.turns[botID]
	memory := br.memory.Snapshot()
	dossiers := make(map[string]bot.Dossier, len(br.dossiers))
	for k, v := range br.dossiers {
		dossiers[k] = v
	}
	overheard := br.overheard
	promoted := br.promoted[botID]
	delete(br.promoted, botID)
	br.mu.Unlock()
	view := state.Sanitized(botID)
	level := bot.LevelNormal
	if p := state.Player(botID); p != nil && p.BotLevel != "" {
		level = bot.ParseLevel(p.BotLevel)
	}
	in := bot.Context{
		State:        view,
		BotID:        botID,
		Moves:        moves,
		Mood:         mood,
		Recent:       recent,
		AdminJokeDue: jokeDue && state.BoutNumber >= adminJokeFromBout,
		Turn:         turn,
		Event:        bot.LastEvent(view, botID),
		Level:        level,
		Memory:       memory,
		Plan:         bot.ChoosePlan(view, botID),
		Dossiers:     dossiers,
		Overheard:    overheard,
		Promoted:     promoted,
	}
	d, ok := h.decide(in)
	if !ok {
		return
	}
	br.mu.Lock()
	br.memory.SetPlan(botID, in.Plan)
	br.mu.Unlock()
	if d.Emoji != "" && !allowedEmojiSet[d.Emoji] {
		d.Emoji = ""
	}
	newState, err := h.store.Update(h.ctx, roomID, false, func(s *game.GameState) error {
		if s.Version != version {
			return errStale
		}
		return h.engine.ApplyMove(s, botID, d.Move)
	})
	if err != nil {
		if !errors.Is(err, errStale) && !errors.Is(err, store.ErrNotFound) {
			h.log.Warn("bot: move rejected", "room", roomID, "bot", botID, "move", d.Move.String(), "err", err)
		}
		return
	}
	if d.AdminJoke {
		br.markJoke(botID)
	}
	if d.Say != "" || d.Emoji != "" {
		h.botSay(roomID, botID, d.Say, d.Emoji)
	}
	h.afterUpdate(roomID, newState)
}

// worthAsking reports whether a decision has a real choice in it. Forced
// moves (the only legal move, or nothing but take/pass/stump) are left to
// the rules: model calls are rate-limited and cost latency.
func worthAsking(moves []game.Move) bool {
	if len(moves) < 2 {
		return false
	}
	for _, m := range moves {
		switch m.Type {
		case game.MoveAttack, game.MoveThrowIn, game.MoveDefend, game.MoveTransfer:
			return true
		}
	}
	return false
}

// decide asks the model when there is one and falls back to the rules.
func (h *Hub) decide(in bot.Context) (bot.Decision, bool) {
	rules, err := bot.NewHeuristic(rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))).Decide(h.ctx, in)
	if err != nil {
		return rules, false
	}
	if h.bots.Brain != nil && worthAsking(in.Moves) {
		hint := rules.Move
		in.Hint = &hint
		timeout := h.bots.LLMTimeout
		if timeout <= 0 {
			timeout = 10 * time.Second
		}
		ctx, cancel := context.WithTimeout(h.ctx, timeout)
		d, err := h.bots.Brain.Decide(ctx, in)
		cancel()
		if err == nil && legalMove(in.Moves, d.Move) {
			d.Plan = in.Plan
			h.mon.Inc("bots.model_moves")
			h.log.Info("bot: model move", "bot", in.BotID, "model", d.Model, "move", d.Move.String(), "say", d.Say)
			return d, true
		}
		if errors.Is(err, bot.ErrPaused) {
			h.mon.Inc("bots.model_paused")
			h.log.Debug("bot: model paused, rule-based move", "bot", in.BotID, "err", err)
		} else {
			h.mon.Inc("bots.model_failures")
			h.mon.Event("llm_error", fmt.Sprint(err), "bot", in.BotID)
			h.log.Warn("bot: model failed, rule-based fallback", "bot", in.BotID, "err", err)
		}
	}
	h.mon.Inc("bots.rule_moves")
	return rules, true
}

// botHearEmoji is called for every reaction a person sends: it colours the
// bots' mood (spec: aggressive on provocation, careful on mockery) and
// sometimes one of them answers with an emoji of its own.
func (h *Hub) botHearEmoji(roomID, from, emoji string) {
	if h.bots.Disabled {
		return
	}
	state, err := h.store.Load(h.ctx, roomID)
	if err != nil || !hasBots(state) {
		return
	}
	name := from
	if p := state.Player(from); p != nil {
		if p.IsBot {
			return
		}
		name = p.Name
	}
	var bots []string
	for _, p := range state.Players {
		if p.IsBot && !p.Out {
			bots = append(bots, p.ID)
		}
	}
	br := h.botRoomFor(roomID)
	now := time.Now()
	br.mu.Lock()
	br.recent = append(br.recent, bot.EmojiEvent{From: from, FromName: name, Emoji: emoji, At: now})
	if len(br.recent) > recentEmojiMax {
		br.recent = br.recent[len(br.recent)-recentEmojiMax:]
	}
	mood := bot.MoodFor(emoji)
	for _, id := range bots {
		if mood == bot.MoodNeutral {
			delete(br.moods, id)
			delete(br.moodUntil, id)
			continue
		}
		br.moods[id] = mood
		br.moodUntil[id] = now.Add(moodDuration)
	}
	br.mu.Unlock()
	if len(bots) == 0 {
		return
	}
	reply := bot.Reply(rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())), emoji)
	if reply == "" {
		return
	}
	speaker := bots[rand.IntN(len(bots))]
	time.AfterFunc(600*time.Millisecond+time.Duration(rand.Int64N(int64(1200*time.Millisecond))), func() {
		select {
		case <-h.ctx.Done():
			return
		default:
		}
		h.botSay(roomID, speaker, "", reply)
	})
}

// botSay publishes a bot's line and/or emoji as a transient reaction. A
// line (not a retort itself) may draw a retort from another bot at the
// table a couple of seconds later; retorts never chain.
func (h *Hub) botSay(roomID, botID, text, emoji string) {
	h.botSayRaw(roomID, botID, text, emoji)
	if text == "" || h.bots.Disabled {
		return
	}
	state, err := h.store.Load(h.ctx, roomID)
	if err != nil {
		return
	}
	speaker := state.Player(botID)
	if speaker == nil {
		return
	}
	br := h.botRoomFor(roomID)
	br.mu.Lock()
	br.overheard = bot.Overheard{BotID: botID, Name: speaker.Name, Text: text}
	br.mu.Unlock()
	var others []string
	for _, p := range state.Players {
		if p.IsBot && !p.Out && p.ID != botID {
			others = append(others, p.ID)
		}
	}
	if len(others) == 0 || rand.Float64() > retortChance {
		return
	}
	replier := others[rand.IntN(len(others))]
	line := bot.Retort(rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())), speaker.Name, len(speaker.Hand))
	time.AfterFunc(1500*time.Millisecond+time.Duration(rand.Int64N(int64(2*time.Second))), func() {
		select {
		case <-h.ctx.Done():
			return
		default:
		}
		h.botSayRaw(roomID, replier, line, "")
	})
}

// retortChance is how often a bot's line gets an answer from another bot.
const retortChance = 0.35

func (h *Hub) botSayRaw(roomID, botID, text, emoji string) {
	raw, err := json.Marshal(pubMessage{Kind: "reaction", Reaction: &ReactionPayload{PlayerID: botID, Emoji: emoji, Text: text, TS: time.Now().UnixMilli()}})
	if err != nil {
		return
	}
	if err := h.store.Publish(h.ctx, roomID, raw); err != nil {
		h.log.Error("bot: publish", "room", roomID, "err", err)
	}
}

// dossier builds what the bots know about a person from the store and caches it for the room.
func (h *Hub) dossier(roomID string, p *game.Player) bot.Dossier {
	prof, err := h.store.Profile(h.ctx, p.ID)
	if err != nil {
		h.log.Warn("bot: profile lookup failed", "player", p.ID, "err", err)
	}
	d := bot.Dossier{Name: p.Name, Games: prof.Games, Losses: prof.Losses, WorstTake: prof.WorstTake, FavSuit: prof.FavouriteSuit(), Scar: prof.Scar}
	br := h.botRoomFor(roomID)
	br.mu.Lock()
	br.dossiers[p.ID] = d
	br.mu.Unlock()
	return d
}

// botGreet has one bot greet a person who just sat down (or the people a
// freshly seated bot finds at the table), from what the table remembers.
func (h *Hub) botGreet(roomID string, state *game.GameState, humanIDs []string) {
	if h.bots.Disabled || state.Status != game.StatusWaiting {
		return
	}
	var bots []string
	for _, p := range state.Players {
		if p.IsBot {
			bots = append(bots, p.ID)
		}
	}
	if len(bots) == 0 {
		return
	}
	for i, id := range humanIDs {
		p := state.Player(id)
		if p == nil || p.IsBot {
			continue
		}
		d := h.dossier(roomID, p)
		speaker := bots[rand.IntN(len(bots))]
		line := bot.Greet(rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())), d)
		delay := 1200*time.Millisecond + time.Duration(i)*1800*time.Millisecond + time.Duration(rand.Int64N(int64(800*time.Millisecond)))
		time.AfterFunc(delay, func() {
			select {
			case <-h.ctx.Done():
				return
			default:
			}
			h.botSayRaw(roomID, speaker, line, "")
		})
	}
}

// botPromote notes that a bot lost the game: it comes back a level stronger and says so.
func (h *Hub) botPromote(roomID, botID string, to bot.Level) {
	br := h.botRoomFor(roomID)
	br.mu.Lock()
	br.promoted[botID] = to
	br.mu.Unlock()
}
