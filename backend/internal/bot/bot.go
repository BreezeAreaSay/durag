// Package bot drives server-side opponents (spec v10 §2): a rule-based brain
// that always works, an optional LLM brain (DeepSeek/Qwen through the
// OpenAI-compatible chat API) that picks a move and may say a short line,
// moods driven by the emoji people send, and the once-per-game fourth-wall
// "joke from the admin".
package bot

import (
	"context"
	"math"
	"math/rand/v2"
	"sort"
	"time"

	"github.com/breezeareasay/durag/backend/internal/game"
)

// Mood is how a bot currently plays. Provocations make it aggressive,
// mockery makes it careful.
type Mood string

const (
	MoodNeutral    Mood = "neutral"
	MoodAggressive Mood = "aggressive"
	MoodDefensive  Mood = "defensive"
)

// EmojiEvent is a reaction somebody sent in the room, as the bot remembers it.
type EmojiEvent struct {
	From     string
	FromName string
	Emoji    string
	At       time.Time
}

// Context is everything a brain may look at: the state as the bot sees it
// (its own hand, counts for everybody else), the legal moves, its mood and
// the recent reactions.
type Context struct {
	State  *game.GameState // sanitized for BotID
	BotID  string
	Moves  []game.Move
	Mood   Mood
	Recent []EmojiEvent
	// AdminJokeDue is true while the bot still owes its once-per-game
	// fourth-wall line.
	AdminJokeDue bool
	// Turn counts the bot's own moves this game; the banter cadence runs on it.
	Turn int
	// Event is the last notable thing a human did: the thing to tease about.
	Event Event
	// Level is how well the bot plays; the zero value is normal.
	Level Level
	// Memory is the room's card memory (a snapshot; nil = no memory).
	Memory *Memory
	// Plan is the line chosen for this situation (filled by Decide).
	Plan Plan
	// Hint is the move the rules would make, offered to the model; nil
	// when the rules have not been asked.
	Hint *game.Move
	// Dossiers is what the table remembers about the people at it, by player id.
	Dossiers map[string]Dossier
	// Overheard is the last line another bot said (and who), for a retort.
	Overheard Overheard
	// Story is what happened at the table this game (counts, notes, last lines, the evening's topic).
	Story Chronicle
	// Promoted is set on the first move after the bot came back a level stronger.
	Promoted Level
}

// Overheard is another bot's last line.
type Overheard struct {
	BotID string
	Name  string
	Text  string
}

// level returns the effective level.
func (c Context) level() Level {
	if c.Level == "" {
		return LevelNormal
	}
	return c.Level
}

// SpeakDue reports whether the bot is allowed a line on this move.
func (c Context) SpeakDue() bool {
	return Due(c.Turn, SpeakEvery)
}

// Decision is a legal move plus an optional short line and an optional
// emoji reaction shown next to the bot's avatar.
type Decision struct {
	Move      game.Move
	Say       string
	Emoji     string
	AdminJoke bool   // Say is the fourth-wall line: do not repeat it this game
	Model     string // which model answered (empty for the rules)
	Plan      Plan   // the plan the move follows
}

// sloppyMove is the mistake a careless player makes: taking a table they
// could have covered, saying "bito" with a card to throw in, or a random
// card when neither applies. It never repeats the planned move.
func sloppyMove(rng *rand.Rand, moves []game.Move, planned game.Move) (game.Move, bool) {
	var take, pass *game.Move
	cards := 0
	for i := range moves {
		switch moves[i].Type {
		case game.MoveTake:
			take = &moves[i]
		case game.MovePass:
			pass = &moves[i]
		case game.MoveAttack, game.MoveThrowIn, game.MoveDefend, game.MoveTransfer:
			cards++
		}
	}
	switch {
	case take != nil && planned.Type != game.MoveTake && cards > 0:
		return *take, true
	case pass != nil && planned.Type != game.MovePass && cards > 0:
		return *pass, true
	}
	if m, ok := randomCardMove(rng, moves); ok && m != planned {
		return m, true
	}
	return game.Move{}, false
}

// randomCardMove picks any attack, throw-in, defence or transfer.
func randomCardMove(rng *rand.Rand, moves []game.Move) (game.Move, bool) {
	var cards []game.Move
	for _, m := range moves {
		switch m.Type {
		case game.MoveAttack, game.MoveThrowIn, game.MoveDefend, game.MoveTransfer:
			cards = append(cards, m)
		}
	}
	if len(cards) == 0 {
		return game.Move{}, false
	}
	return cards[rng.IntN(len(cards))], true
}

// Brain picks a move. Implementations must return one of in.Moves.
type Brain interface {
	Decide(ctx context.Context, in Context) (Decision, error)
}

// Line is what a model says when the rules have already moved: a short
// line and an optional emoji.
type Line struct {
	Say   string
	Emoji string
	Model string
}

// Talker is a brain that can be asked for a line only, after the move was
// made by the rules (the default use of the model: the rules play, the
// model talks).
type Talker interface {
	Talk(ctx context.Context, in Context, move game.Move) (Line, error)
}

// AdminJokes are the fourth-wall lines (spec v10 §2): the AI admits what it
// is, exactly once per game.
var AdminJokes = []string{
	"Админ просил передать: я всего лишь скрипт на Go, но козыри у меня настоящие.",
	"Между нами: админ забыл выдать мне зарплату, так что играю за еду. За ваши карты.",
	"Это не баг, это я. Админ разрешил сломать четвёртую стену ровно один раз — считайте, сломал.",
	"Админ смотрит логи. Передаю привет из backend/internal/bot.",
	"Я бы подмигнул, но админ не завёз мне глаз. Зато завёз пенёк.",
}

// Provocations and mockery (spec: aggressive on provocation, defensive on
// mockery); anything else calms the bot down.
var (
	Provocations = map[string]bool{"😡": true, "🔥": true, "💀": true, "👎": true}
	Mockery      = map[string]bool{"😂": true, "🤡": true}
)

// MoodFor classifies a reaction aimed at the bots.
func MoodFor(emoji string) Mood {
	switch {
	case Provocations[emoji]:
		return MoodAggressive
	case Mockery[emoji]:
		return MoodDefensive
	}
	return MoodNeutral
}

// Reply picks an emoji answer to a reaction, or "" to stay silent.
func Reply(rng *rand.Rand, emoji string) string {
	var pool []string
	switch {
	case Provocations[emoji]:
		pool = []string{"😎", "🔥", "😡"}
	case Mockery[emoji]:
		pool = []string{"🤔", "🙏", "💀"}
	case emoji == "❤️" || emoji == "👍" || emoji == "👏":
		pool = []string{"❤️", "👍", "😎"}
	default:
		return ""
	}
	if rng.Float64() > 0.6 {
		return ""
	}
	return pool[rng.IntN(len(pool))]
}

// --- the rule-based brain --------------------------------------------------------

// Heuristic is the always-available strategy: sound, a little moody and
// deterministic for a given random source.
type Heuristic struct {
	rng *rand.Rand
}

// NewHeuristic creates the rule-based brain; a nil rng uses a fresh seed.
func NewHeuristic(rng *rand.Rand) *Heuristic {
	if rng == nil {
		rng = rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 0x5eed))
	}
	return &Heuristic{rng: rng}
}

// Decide implements Brain. It never fails: with at least one legal move it
// always returns one of them.
func (h *Heuristic) Decide(_ context.Context, in Context) (Decision, error) {
	if in.Plan == "" {
		in.Plan = ChoosePlan(in.State, in.BotID)
	}
	d := Decision{Move: h.pick(in), Plan: in.Plan}
	// a counting bot searches the endgame: the deck is gone and the memory
	// can place almost every card (the hard level looks at more deals)
	if in.level().Counts() {
		if m, ok := h.endgame(in); ok {
			d.Move = m
		}
	}
	// a weak player sometimes does not look: a sloppy move (easy: takes a
	// table it could cover, passes with a card to throw), or merely a
	// different card than the plan wanted
	if eps := in.level().Slip(); eps > 0 && h.rng.Float64() < eps {
		if m, ok := sloppyMove(h.rng, in.Moves, d.Move); ok {
			d.Move = m
		}
	} else if eps := in.level().Blunder(); eps > 0 && h.rng.Float64() < eps {
		if m, ok := randomCardMove(h.rng, in.Moves); ok {
			d.Move = m
		}
	}
	// What the rules say out loud. The memory stays in the play, not in the
	// talk: lines about remembered cards were the ones people found wrong and
	// tiresome, so a bot only comments on things everybody can see.
	planChanged := in.Memory != nil && in.Memory.Plan(in.BotID) != in.Plan
	defender := ""
	if p := in.State.Defender(); p != nil && p.ID != in.BotID {
		defender = p.Name
	}
	habit := ""
	if in.Event.Kind == "attack" && in.Event.Suit != "" && !in.Event.Bot {
		habit = Habit(h.rng, in.Dossiers[in.Event.PlayerID], in.Event.Suit)
	}
	ch := CharacterOf(nameOf(in.State, in.BotID))
	switch {
	case in.Promoted != "":
		d.Say = PromoAs(ch, h.rng, in.Promoted)
	case in.AdminJokeDue && h.rng.Float64() < 0.3:
		d.Say = AdminJokes[h.rng.IntN(len(AdminJokes))]
		d.AdminJoke = true
	case habit != "" && h.rng.Float64() < 0.25:
		d.Say = habit
	case planChanged && in.Plan == PlanPress && h.rng.Float64() < 0.3:
		d.Say = ch.Say(h.rng, "press", defender, 0)
	case planChanged && in.Plan == PlanHunt && h.rng.Float64() < 0.4:
		d.Say = ch.Say(h.rng, "hunt", defender, 0)
	case in.Event.Kind == "took" && !in.Event.Bot && in.Dossiers[in.Event.PlayerID].Respected() && h.rng.Float64() < 0.5:
		d.Say = Respect(h.rng, in.Dossiers[in.Event.PlayerID], in.Event.Count) // even the champion takes
	case in.Event.Kind == "lonely" && !in.Event.Bot && in.Dossiers[in.Event.PlayerID].Respected() && h.rng.Float64() < 0.5:
		d.Say = RespectLonely(h.rng, in.Dossiers[in.Event.PlayerID])
	case in.Event.Kind == "took" && in.Event.PlayerID != "" && Callback(h.rng, in.Story, in.Event.PlayerID, in.Event.Name) != "" && h.rng.Float64() < 0.7:
		d.Say = Callback(h.rng, in.Story, in.Event.PlayerID, in.Event.Name) // the story so far: "that is your third take"
	case in.SpeakDue() && in.Event.Notable():
		d.Say = ch.Say(h.rng, in.Event.Kind, in.Event.Name, in.Event.Count)
		if d.Say == "" {
			d.Say = Jab(h.rng, in.Event)
		}
	case d.Move.Type == game.MoveTake && in.SpeakDue() && h.rng.Float64() < 0.5:
		d.Say = ch.Say(h.rng, "take", "", 0)
	}
	if d.Move.Type == game.MoveTake && h.rng.Float64() < 0.3 && !ch.Barks {
		d.Emoji = []string{"💀", "😡", "🤔", "🚬", "🚬"}[h.rng.IntN(5)] // taking the table calls for a smoke
	}
	d.Say = ch.Voice(h.rng, d.Say)
	return d, nil
}

// Knobs of the hard level's extra habits (experiments switch them off one by one).
var (
	HardKeepsPairs = true
	HardOddsAlive  = true
	HardTakesEarly = true
)

// knownCover reports whether the memory places in who's hand a card that
// beats c: throwing such a card is a gift, whatever the deck says.
func knownCover(m *Memory, s *game.GameState, who string, c game.Card, trump string) bool {
	if m == nil {
		return false
	}
	for _, k := range m.Known(who) {
		if game.CanBeat(c, k, trump) {
			return true
		}
	}
	return false
}

// nextAfterDefender tells whether the bot attacks next if the defender
// takes: then every card the defender swallows is a card the bot leads
// against, and pressing pays twice. Otherwise somebody else cashes in.
func nextAfterDefender(s *game.GameState, botID string) bool {
	return s.NextActive(s.DefenderID) == botID
}

// isTrump reports whether c is a card of the active trump suit (specials have none).
func isTrump(c game.Card, trump string) bool {
	return trump != "" && c.Suit == trump && !c.IsSpecial()
}

// rankCount counts the plain cards of every rank in a hand: pairs lead
// together, the second card thrown in right after the first.
func rankCount(hand map[string]game.Card) map[int]int {
	counts := map[int]int{}
	for _, c := range hand {
		if !c.IsSpecial() {
			counts[c.Rank]++
		}
	}
	return counts
}

// tableSize is how many cards a taker would pick up right now.
func tableSize(s *game.GameState) int {
	n := 0
	for _, id := range s.TableOrder {
		n += 1 + len(s.TableCards[id])
	}
	return n
}

type scored struct {
	move   game.Move
	weight int
}

// weight orders cards by how much the bot wants to keep them: low cards go
// first, trumps are precious, specials are almost never spent.
func weight(c game.Card, trump string) int {
	w := c.Rank
	if trump != "" && c.Suit == trump {
		w += 20
	}
	if c.IsJoker() {
		w += 40
	}
	if c.IsSuper() {
		w += 60
	}
	return w
}

func (h *Heuristic) pick(in Context) game.Move {
	s := in.State
	if len(in.Moves) == 0 {
		return game.Move{}
	}
	me := s.Player(in.BotID)
	trump := s.ActiveTrump()
	hand := map[string]game.Card{}
	if me != nil {
		for _, c := range me.Hand {
			hand[c.ID] = c
		}
	}
	byType := map[game.MoveType][]scored{}
	for _, m := range in.Moves {
		w := 0
		if c, ok := hand[m.CardID]; ok {
			w = weight(c, trump)
		}
		byType[m.Type] = append(byType[m.Type], scored{move: m, weight: w})
	}
	for k := range byType {
		list := byType[k]
		sort.SliceStable(list, func(i, j int) bool { return list[i].weight < list[j].weight })
	}
	if ms := byType[game.MoveTakeStump]; len(ms) > 0 {
		return ms[0].move
	}
	if ms := byType[game.MoveDefend]; len(ms) > 0 || len(byType[game.MoveTransfer]) > 0 || len(byType[game.MoveTake]) > 0 {
		return h.defend(in, byType, hand, trump)
	}
	if ms := byType[game.MoveAttack]; len(ms) > 0 {
		return h.attack(in, ms, hand, trump)
	}
	return h.throwOrPass(in, byType, hand, trump)
}

// defend covers the cheapest way, transfers when it is a good deal and takes
// when covering would cost a special or a big trump (depending on the mood).
func (h *Heuristic) defend(in Context, byType map[game.MoveType][]scored, hand map[string]game.Card, trump string) game.Move {
	s := in.State
	deckLeft := s.DeckCount > 0
	handN := 0
	if me := s.Player(in.BotID); me != nil {
		handN = len(me.Hand)
	}
	defends := byType[game.MoveDefend]
	take, canTake := game.Move{Type: game.MoveTake}, len(byType[game.MoveTake]) > 0
	// how dear is the cheapest cover? a transfer is worth it before a trump or a special goes
	coverDear := false
	if len(defends) > 0 {
		c := hand[defends[0].move.CardID]
		coverDear = c.IsSpecial() || (isTrump(c, trump) && deckLeft)
	}
	// transfer: a card of the attack's rank passes the whole defence on.
	// Aggressive bots always do it, careful ones only with a cheap card,
	// everybody when the alternative is burning a trump or a special.
	if ts := byType[game.MoveTransfer]; len(ts) > 0 {
		c := hand[ts[0].move.CardID]
		tr := isTrump(c, trump)
		ok := false
		switch in.Mood {
		case MoodAggressive:
			ok = true
		case MoodDefensive:
			ok = !tr && c.Rank <= 10
		default:
			ok = !tr || c.Rank <= 9 || !deckLeft
		}
		if !ok && (coverDear || len(defends) == 0) && !c.IsJoker() {
			ok = true
		}
		if ok {
			return ts[0].move
		}
	}
	if len(defends) == 0 {
		if canTake {
			return take
		}
		return in.Moves[0]
	}
	// can every undefended attack be covered with a distinct card?
	targets := map[string]bool{}
	for _, d := range defends {
		targets[d.move.TargetID] = true
	}
	used := map[string]bool{}
	var plan []scored
	for target := range targets {
		best := scored{weight: 1 << 30}
		for _, d := range defends {
			if d.move.TargetID == target && !used[d.move.CardID] && d.weight < best.weight {
				best = d
			}
		}
		if best.weight == 1<<30 {
			if canTake {
				return take
			}
			return defends[0].move
		}
		used[best.move.CardID] = true
		plan = append(plan, best)
	}
	sort.SliceStable(plan, func(i, j int) bool { return plan[i].weight < plan[j].weight })
	cheapest := plan[0]
	dearest := hand[plan[len(plan)-1].move.CardID] // the cover is only as cheap as its dearest card
	pile := tableSize(s)                           // what taking would cost
	// Spending rules. While the deck lasts, specials and big trumps are
	// worth more than a small pile of cards: take instead. With the deck
	// gone every card in hand is a liability, so cover whenever possible,
	// keeping the Super card for the finishing attack when the pile is small.
	// The first bouts: while the deck tops the hand up, a counting bot never
	// beats a seven with a king, a joker or the Super card. A single low card
	// taken now costs nothing; the big card wins a bout later.
	if canTake && in.Plan != PlanDump && in.Plan != PlanStump && in.level().Counts() && s.DeckCount >= 8 {
		if dearest.IsSpecial() && pile <= 3 {
			return take
		}
		if in.level() == LevelHard && pile <= 2 {
			for _, p := range plan {
				cover := hand[p.move.CardID]
				target, ok := game.CardByID(p.move.TargetID)
				if !ok || target.IsSpecial() {
					continue
				}
				if !isTrump(cover, trump) && cover.Rank >= 12 && cover.Rank-target.Rank >= 5 {
					return take // a queen or better on a card five ranks below: not this early
				}
				if isTrump(cover, trump) && cover.Rank >= 11 && !isTrump(target, trump) {
					return take // a big trump on a plain card: not this early
				}
			}
		}
	}
	if canTake && in.Plan != PlanDump && in.Plan != PlanStump {
		switch {
		case in.Plan == PlanHoard && dearest.Rank >= 13 && !dearest.IsSpecial() && pile <= 1 && handN <= 6 && s.DeckCount >= 6:
			return take // an ace or a king may be the trump once it shows: not for a single low card
		case !deckLeft:
			if dearest.IsSuper() && pile <= 1 && handN >= 3 {
				return take
			}
		case in.Mood == MoodAggressive:
			if (dearest.IsSuper() && pile <= 2) || (dearest.IsJoker() && pile <= 1) {
				return take
			}
		case in.Mood == MoodDefensive:
			if dearest.IsSpecial() || (isTrump(dearest, trump) && dearest.Rank >= 10 && pile <= 3) {
				return take
			}
		default:
			if (dearest.IsSpecial() && pile <= 2) || (isTrump(dearest, trump) && dearest.Rank >= 11 && pile <= 2 && handN <= 7) {
				return take
			}
			// early in the game a lone low card is not worth a ten of trumps:
			// take it, the deck tops the hand up anyway
			if HardTakesEarly && in.level() == LevelHard && s.DeckCount >= 10 && isTrump(dearest, trump) && dearest.Rank >= 10 && pile <= 1 && handN <= 6 {
				return take
			}
		}
	}
	return cheapest.move
}

// attack opens a bout. Cheap cards go first and a pair beats a single
// (the second card follows at once as a throw-in); trumps wait while the
// deck lasts; specials are kept for the end, the Super card as the very
// last card of the hand, an attack nobody can beat. With the deck gone the
// odds decide: the card the defender is least likely to beat.
func (h *Heuristic) attack(in Context, attacks []scored, hand map[string]game.Card, trump string) game.Move {
	s := in.State
	deckLeft := s.DeckCount > 0
	var plain, trumps []scored
	for _, a := range attacks {
		c := hand[a.move.CardID]
		switch {
		case c.IsSpecial():
			continue
		case isTrump(c, trump) && deckLeft:
			trumps = append(trumps, a)
		default:
			plain = append(plain, a)
		}
	}
	if len(plain) == 0 {
		if len(trumps) > 0 {
			return trumps[0].move // only trumps: the smallest
		}
		return attacks[0].move // only specials: a Joker before the Super card
	}
	counts := rankCount(hand)
	counting := in.level().Counts() && in.Memory != nil && (!deckLeft || (HardOddsAlive && in.level() == LevelHard))
	d := s.Defender()
	// the cheapest plain rank in hand: while the deck lasts nothing above a
	// ten is led when something at or below a ten is there (no aces over sevens)
	lowest := 99
	for _, a := range plain {
		if r := hand[a.move.CardID].Rank; r < lowest {
			lowest = r
		}
	}
	best, bestScore := plain[0], math.MaxFloat64
	for _, a := range plain {
		c := hand[a.move.CardID]
		if deckLeft && c.Rank >= 12 && lowest <= 10 {
			continue // no queens, kings or aces led while something small is in hand
		}
		// while the deck lasts a lead stays near the cheapest card: no jacks
		// and queens over sevens, a pair of them aside
		if deckLeft && c.Rank > lowest+3 && (counts[c.Rank] < 2 || c.Rank > 10) {
			continue
		}
		score := float64(a.weight) // cheap first
		pairCap := 10              // a pair of jacks is still two jacks: no pair bonus above a ten while the deck lasts
		if counts[c.Rank] >= 2 && (!deckLeft || c.Rank <= pairCap) {
			score -= 4 // a pair: two cards for the price of one bout, but no high pair over a low single
			if counts[c.Rank] >= 3 {
				score -= 1
			}
		}
		if in.Plan == PlanHoard && c.Rank >= 13 && lowest < 13 {
			continue // keep the tops while the trump is unknown
		}
		if counting && d != nil {
			if knownCover(in.Memory, s, d.ID, c, trump) {
				score += 20 // they hold the cover: this card is a gift
			} else if in.Plan == PlanStump {
				score += 16 * in.Memory.BeatProb(s, d.ID, c) // shed: what they cannot beat first
			} else if deckLeft {
				score += 4 * in.Memory.BeatProb(s, d.ID, c) // with cards still to draw, cheap beats unlikely-to-be-covered
			} else {
				score += 12 * in.Memory.BeatProb(s, d.ID, c) // what they cannot beat goes first
			}
		}
		if score < bestScore {
			best, bestScore = a, score
		}
	}
	return best.move
}

// throwOrPass throws in a matching card or says "bito". A taker gets the
// junk, card after card (the scheduler makes that a quick pack); a defender
// who is still beating only gets what they will struggle with, never a
// high card or a trump they cover for free while the deck lasts.
func (h *Heuristic) throwOrPass(in Context, byType map[game.MoveType][]scored, hand map[string]game.Card, trump string) game.Move {
	s := in.State
	deckLeft := s.DeckCount > 0
	handN := len(hand)
	taking := s.DefenderTaking
	bloated := in.Plan == PlanDump || handN >= 9
	counting := in.level().Counts() && in.Memory != nil
	hard := in.level() == LevelHard
	d := s.Defender()
	counts := rankCount(hand)
	keepPairs := hard && HardKeepsPairs
	oddsAlive := hard && HardOddsAlive
	// the dearest plain card worth throwing at a defender who is still beating
	limit := 9
	switch in.Mood {
	case MoodAggressive:
		limit = 10
	case MoodDefensive:
		limit = 8
	}
	// who cashes in if the defender takes: the bot (press on) or somebody else (be stingy)
	if nextAfterDefender(s, in.BotID) {
		limit++
	} else if len(s.ActivePlayers()) > 2 {
		limit--
	}
	if bloated {
		limit = 11
	}
	if in.Plan == PlanStump {
		limit = 14 // get the hand down to the stump: anything plain goes
	}
	if !deckLeft {
		limit = 11 // the endgame: every card out of the hand counts, the odds decide the rest
	}
	for _, t := range byType[game.MoveThrowIn] { // cheapest first
		c := hand[t.move.CardID]
		if c.IsSpecial() {
			continue
		}
		tr := isTrump(c, trump)
		if taking {
			// a taker is a bin for junk: plain cards up to a jack while the deck
			// lasts (a king out of a bloated hand), anything plain with the deck
			// gone; trumps only small ones in the endgame
			if tr && (deckLeft || c.Rank > 9) && !bloated {
				continue
			}
			if !tr && deckLeft && c.Rank > 11 && !(bloated && c.Rank <= 13) {
				continue
			}
			if in.Plan == PlanHoard && c.Rank >= 13 && !bloated {
				continue // a king of an unknown suit is half a trump: not a gift for a taker
			}
			return t.move
		}
		if tr && deckLeft && !bloated {
			continue
		}
		if counting && d != nil && knownCover(in.Memory, s, d.ID, c, trump) {
			continue // they are known to hold the cover: a free card for them
		}
		// a hard bot keeps a low pair while the deck lasts: the second card is
		// a transfer waiting to happen and a cheap lead later
		if keepPairs && deckLeft && !bloated && !tr && c.Rank <= 10 && counts[c.Rank] == 2 {
			continue
		}
		if counting && d != nil && (!deckLeft || oddsAlive) {
			p := in.Memory.BeatProb(s, d.ID, c)
			switch {
			case !deckLeft && (p < 0.45 || (c.Rank <= limit && !tr && p < 0.85)):
				return t.move
			case deckLeft && c.Rank <= limit && !tr && p < 0.6:
				return t.move
			}
			continue
		}
		if c.Rank <= limit && (!tr || !deckLeft) {
			return t.move
		}
	}
	if ps := byType[game.MovePass]; len(ps) > 0 {
		return ps[0].move
	}
	return in.Moves[0]
}
