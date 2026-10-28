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
	Disabled bool          // ADD_BOT is rejected
	Brain    bot.Brain     // optional LLM brain; nil = rule-based only
	MinDelay time.Duration // human-like pause before a bot acts
	MaxDelay time.Duration
	// LLMMoves lets the model choose moves (with the rules as a hint). Off by
	// default: the rules move, the model only talks (bot.Talker), after the
	// move and without holding the game up.
	LLMMoves   bool
	LLMTimeout time.Duration // how long a model may think before the rules take over
	// TalkShare is the share of talk occasions that go to the model (the
	// rest use the character's own lines); TalkDaily caps model calls per
	// day. Free models have small quotas: this keeps a trickle going all day.
	TalkShare float64
	TalkDaily int
}

// talkBudget meters the model's lines: a share of the occasions and a cap
// per day, so a free quota lasts the evening instead of the first hour.
type talkBudget struct {
	mu   sync.Mutex
	day  int
	used int
}

// allow spends one call when the share and the day's cap permit.
func (b *talkBudget) allow(o BotOptions, now time.Time) bool {
	share := o.TalkShare
	if share <= 0 {
		share = 1
	}
	if share < 1 && rand.Float64() >= share {
		return false
	}
	if o.TalkDaily <= 0 {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if d := now.YearDay() + now.Year()*400; d != b.day {
		b.day, b.used = d, 0
	}
	if b.used >= o.TalkDaily {
		return false
	}
	b.used++
	return true
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
	talking    map[string]bool        // bots with a model request for a line in flight
	lastLineAt time.Time              // when any bot last said a line: the room talks one at a time
	story      *bot.Chronicle         // what happened this game, in words
	plot       *bot.Storyline         // the exchange between two characters that unfolds over the game
	plotStep   int
	plotBout   int // the bout the last step was said on
}

// lineCooldown is the least time between two bot lines in one room, so the
// people at the table can read one before the next appears.
const lineCooldown = 4 * time.Second

// mayTalk reserves the floor for a line now, or says no while the last line
// is still fresh.
func (br *botRoom) mayTalk(now time.Time) bool {
	br.mu.Lock()
	defer br.mu.Unlock()
	if now.Sub(br.lastLineAt) < lineCooldown {
		return false
	}
	br.lastLineAt = now
	return true
}

// freshGame clears the room's talk cooldown: the greeting in the lobby must
// not silence the first line at the table.
func (br *botRoom) freshGame() {
	br.mu.Lock()
	br.lastLineAt = time.Time{}
	br.mu.Unlock()
}

func newBotRoom() *botRoom {
	return &botRoom{moods: map[string]bot.Mood{}, moodUntil: map[string]time.Time{}, jokeSaid: map[string]bool{}, scheduled: map[string]int64{}, turns: map[string]int{}, memory: bot.NewMemory(), story: bot.NewChronicle(), dossiers: map[string]bot.Dossier{}, promoted: map[string]bot.Level{}, talking: map[string]bool{}}
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

// pickCharacter hands out the next free character of the communal flat.
func pickCharacter(s *game.GameState) bot.Character {
	used := map[string]bool{}
	for _, p := range s.Players {
		used[p.Name] = true
	}
	var free []bot.Character
	for _, c := range bot.Characters {
		if !used[c.Name] {
			free = append(free, c)
		}
	}
	if len(free) > 0 {
		return free[rand.IntN(len(free))] // whoever is around: a random one of the free characters
	}
	c := bot.Characters[len(s.Players)%len(bot.Characters)]
	c.Name = fmt.Sprintf("%s-%d", c.Name, len(s.Players)+1)
	return c
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

// comboDelay is the pause between two cards of the same bot in one bout:
// the second card of a pair, the next card of a pack thrown at a taker. A
// quick flick, not a whole think.
func (h *Hub) comboDelay() time.Duration {
	if h.bots.MaxDelay <= 0 {
		return 0
	}
	return 160*time.Millisecond + time.Duration(rand.Int64N(int64(220*time.Millisecond)))
}

// continuing reports whether the bot's last move was a card into the open
// bout (an attack or a throw-in): its next card comes as part of the pack.
func continuing(state *game.GameState, botID string) bool {
	if len(state.Log) == 0 || len(state.TableOrder) == 0 {
		return false
	}
	last := state.Log[len(state.Log)-1]
	return last.PlayerID == botID && (last.Type == "attack" || last.Type == "throw_in")
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
		br.story.Reset(nil)
		var names []string
		for _, p := range state.Players {
			if p.IsBot {
				names = append(names, p.Name)
			}
		}
		br.plot = bot.PickStoryline(rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())), names)
		br.plotStep, br.plotBout = 0, state.BoutNumber
	}
	br.lastStatus = state.Status
	br.memory.Observe(state)
	br.story.Observe(state)
	if state.Status != game.StatusPlaying {
		return
	}
	// the storyline: one line every three bouts, by the character whose turn it is to speak
	if br.plot != nil && br.plotStep < len(br.plot.Steps) && state.BoutNumber-br.plotBout >= 3 {
		step := br.plot.Steps[br.plotStep]
		for _, p := range state.Players {
			if p.IsBot && !p.Out && p.Name == step.Who {
				br.plotStep++
				br.plotBout = state.BoutNumber
				botID := p.ID
				line := step.Line
				time.AfterFunc(700*time.Millisecond+time.Duration(rand.Int64N(int64(900*time.Millisecond))), func() {
					select {
					case <-h.ctx.Done():
						return
					default:
					}
					h.botSay(roomID, botID, line, "")
				})
				break
			}
		}
	}
	due := append([]string{}, state.Actors()...)
	due = append(due, state.StumpPending...)
	for _, id := range due {
		if !isBot(state, id) || br.scheduled[id] == state.Version {
			continue
		}
		br.scheduled[id] = state.Version
		botID, version := id, state.Version
		delay := h.botDelay()
		if continuing(state, id) {
			delay = h.comboDelay()
		}
		time.AfterFunc(delay, func() {
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
	story := br.story.Snapshot()
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
		Story:        story,
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
	// With a model at the table it does the talking: on the occasions the
	// rules would have used a stock line, and on the cadence. Without one
	// the character's own lines go. The emoji is the rules' either way.
	if _, talker := h.bots.Brain.(bot.Talker); talker && !h.bots.LLMMoves && (d.Say != "" || in.SpeakDue() || in.Event.Notable() || d.Move.Type == game.MoveTake) && h.talk.allow(h.bots, time.Now()) {
		if d.Emoji != "" {
			h.botSayRaw(roomID, botID, "", d.Emoji)
		}
		go h.botTalk(roomID, botID, in, d.Move) // the model speaks after the move, never holding it up
	} else if d.Say != "" || d.Emoji != "" {
		h.botSay(roomID, botID, d.Say, d.Emoji)
	}
	h.afterUpdate(roomID, newState)
}

// botTalk asks the model for a line about a move the rules just made and
// publishes it when it arrives. One request per bot at a time; a line that
// comes after the game ended is dropped.
func (h *Hub) botTalk(roomID, botID string, in bot.Context, move game.Move) {
	talker, ok := h.bots.Brain.(bot.Talker)
	if !ok {
		return
	}
	br := h.botRoomFor(roomID)
	br.mu.Lock()
	if br.talking[botID] {
		br.mu.Unlock()
		return
	}
	br.talking[botID] = true
	br.mu.Unlock()
	defer func() {
		br.mu.Lock()
		delete(br.talking, botID)
		br.mu.Unlock()
	}()
	timeout := h.bots.LLMTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(h.ctx, timeout)
	line, err := talker.Talk(ctx, in, move)
	cancel()
	if err != nil {
		if errors.Is(err, bot.ErrPaused) {
			h.mon.Inc("bots.model_paused")
			return
		}
		h.mon.Inc("bots.model_failures")
		h.mon.Event("llm_error", fmt.Sprint(err), "bot", botID)
		h.log.Warn("bot: model line failed", "bot", botID, "err", err)
		return
	}
	if line.Emoji != "" && !allowedEmojiSet[line.Emoji] {
		line.Emoji = ""
	}
	if line.Say == "" && line.Emoji == "" {
		return
	}
	state, err := h.store.Load(h.ctx, roomID)
	if err != nil || state.Status != game.StatusPlaying {
		return
	}
	if in.AdminJokeDue && bot.MentionsAdmin(line.Say) {
		br.markJoke(botID)
	}
	h.mon.Inc("bots.model_lines")
	h.log.Info("bot: model line", "bot", botID, "model", line.Model, "say", line.Say)
	h.botSay(roomID, botID, line.Say, line.Emoji)
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
	if h.bots.Brain != nil && h.bots.LLMMoves && worthAsking(in.Moves) {
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
	if text != "" && !h.botRoomFor(roomID).mayTalk(time.Now()) {
		text = "" // somebody just spoke: the emoji may still go, the words wait for another time
	}
	if text == "" && emoji == "" {
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
	text = bot.CharacterOf(speaker.Name).Voice(rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())), text) // a dog barks whatever the model wrote
	h.botSayRaw(roomID, botID, text, emoji)
	if text == "" || h.bots.Disabled {
		return
	}
	br := h.botRoomFor(roomID)
	br.mu.Lock()
	br.overheard = bot.Overheard{BotID: botID, Name: speaker.Name, Text: text}
	br.story.Said(speaker.Name, text)
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
	replierName := ""
	if p := state.Player(replier); p != nil {
		replierName = p.Name
	}
	replierCh := bot.CharacterOf(replierName)
	rng := rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	line := replierCh.Voice(rng, bot.RetortAs(replierCh, rng, speaker.Name, len(speaker.Hand)))
	time.AfterFunc(lineCooldown+time.Duration(rand.Int64N(int64(2*time.Second))), func() {
		select {
		case <-h.ctx.Done():
			return
		default:
		}
		if !br.mayTalk(time.Now()) {
			return
		}
		h.botSayRaw(roomID, replier, line, "")
	})
}

// retortChance is how often a bot's line gets an answer from another bot.
const retortChance = 0.45

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
	br := h.botRoomFor(roomID)
	for i, id := range humanIDs {
		p := state.Player(id)
		if p == nil || p.IsBot {
			continue
		}
		d := h.dossier(roomID, p)
		speaker := bots[rand.IntN(len(bots))]
		speakerName := ""
		if sp := state.Player(speaker); sp != nil {
			speakerName = sp.Name
		}
		greetCh := bot.CharacterOf(speakerName)
		greetRng := rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
		line := greetCh.Voice(greetRng, bot.GreetAs(greetCh, greetRng, d))
		delay := 1200*time.Millisecond + time.Duration(i)*lineCooldown + time.Duration(rand.Int64N(int64(800*time.Millisecond)))
		time.AfterFunc(delay, func() {
			select {
			case <-h.ctx.Done():
				return
			default:
			}
			if !br.mayTalk(time.Now()) {
				return
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

// botHearText files a human's quick phrase with the bots: the model reads
// it in the recent reactions, as it reads an emoji.
func (h *Hub) botHearText(roomID, from, text string) {
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
	br := h.botRoomFor(roomID)
	br.mu.Lock()
	br.story.Said(name, text)
	br.recent = append(br.recent, bot.EmojiEvent{From: from, FromName: name, Emoji: "«" + text + "»", At: time.Now()})
	if len(br.recent) > recentEmojiMax {
		br.recent = br.recent[len(br.recent)-recentEmojiMax:]
	}
	br.mu.Unlock()
}
