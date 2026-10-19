// Package bot drives server-side opponents (spec v10 §2): a rule-based brain
// that always works, an optional LLM brain (DeepSeek/Qwen through the
// OpenAI-compatible chat API) that picks a move and may say a short line,
// moods driven by the emoji people send, and the once-per-game fourth-wall
// "joke from the admin".
package bot

import (
	"context"
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

// Names bots are called, in order of preference.
var Names = []string{"Нейронка", "Васёк", "Бабуля", "Дядя Толя", "Кефир", "Шуруп", "Люся", "Гоша"}

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
	// a weak player sometimes does not look
	if eps := in.level().Blunder(); eps > 0 && h.rng.Float64() < eps {
		if m, ok := randomCardMove(h.rng, in.Moves); ok {
			d.Move = m
		}
	}
	planChanged := in.Memory != nil && in.Memory.Plan(in.BotID) != in.Plan
	defender := ""
	if p := in.State.Defender(); p != nil && p.ID != in.BotID {
		defender = p.Name
	}
	switch {
	case in.AdminJokeDue && h.rng.Float64() < 0.3:
		d.Say = AdminJokes[h.rng.IntN(len(AdminJokes))]
		d.AdminJoke = true
	case in.Memory != nil && in.Memory.TookFrom(d.Move.CardID) != "" && h.rng.Float64() < 0.8:
		// returning a card the human threw at it: the memory speaks
		d.Say = fill(ReturnLines[h.rng.IntN(len(ReturnLines))], nameOf(in.State, in.Memory.TookFrom(d.Move.CardID)), "")
	case in.Memory != nil && in.Memory.Recall.Version == in.State.Version && in.Memory.Recall.PlayerID != "" && h.rng.Float64() < 0.6:
		r := in.Memory.Recall
		d.Say = fill(RecallLines[h.rng.IntN(len(RecallLines))], r.Name, cardText(r.Card))
	case planChanged && h.rng.Float64() < 0.5:
		d.Say = PlanLine(h.rng, in.Plan, defender)
	case in.SpeakDue():
		d.Say = Jab(h.rng, in.Event)
	}
	if d.Move.Type == game.MoveTake && h.rng.Float64() < 0.4 {
		d.Emoji = []string{"💀", "😡", "🤔", "🚬", "🚬"}[h.rng.IntN(5)] // taking the table calls for a smoke
	}
	return d, nil
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
	// transfer: aggressive bots always pass the buck, careful ones only with
	// a card they do not mind losing
	if ts := byType[game.MoveTransfer]; len(ts) > 0 {
		c := hand[ts[0].move.CardID]
		isTrump := trump != "" && c.Suit == trump
		switch in.Mood {
		case MoodAggressive:
			return ts[0].move
		case MoodDefensive:
			if !isTrump && c.Rank <= 10 {
				return ts[0].move
			}
		default:
			if !isTrump {
				return ts[0].move
			}
		}
	}
	defends := byType[game.MoveDefend]
	take, canTake := game.Move{Type: game.MoveTake}, len(byType[game.MoveTake]) > 0
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
	card := hand[cheapest.move.CardID]
	// spending rules: specials and big trumps are kept while the deck lasts;
	// a bloated hand covers with anything rather than grow
	if canTake && deckLeft && in.Plan != PlanDump {
		switch in.Mood {
		case MoodDefensive:
			if card.IsSpecial() || (trump != "" && card.Suit == trump && card.Rank >= 11) {
				return take
			}
		case MoodAggressive:
			if card.IsSuper() && len(s.TableOrder) <= 1 {
				return take
			}
		default:
			if card.IsSuper() || (card.IsJoker() && len(s.TableOrder) <= 1) {
				return take
			}
		}
	}
	return cheapest.move
}

// attack opens with a low card; aggressive bots lead the rank they hold most
// of (to pile on later), careful ones keep trumps for defence.
func (h *Heuristic) attack(in Context, attacks []scored, hand map[string]game.Card, trump string) game.Move {
	s := in.State
	deckLeft := s.DeckCount > 0
	var plain []scored
	for _, a := range attacks {
		c := hand[a.move.CardID]
		if c.IsSpecial() {
			continue
		}
		if trump != "" && c.Suit == trump && deckLeft && in.Mood != MoodAggressive {
			continue
		}
		plain = append(plain, a)
	}
	if len(plain) == 0 {
		return attacks[0].move // only specials or trumps left: lead the cheapest
	}
	// by the numbers: the card the defender is least likely to beat, the
	// cheapest among equals (hunting accepts a dearer card for a surer kill)
	if (in.Plan == PlanCount || in.Plan == PlanHunt) && in.level().Counts() {
		if d := s.Defender(); d != nil {
			best, bestP := plain[0], 2.0
			for _, a := range plain {
				p := in.Memory.BeatProb(s, d.ID, hand[a.move.CardID])
				if in.Plan == PlanCount {
					p += float64(a.weight) / 100 // a tie-break, not a reason
				}
				if p < bestP {
					best, bestP = a, p
				}
			}
			return best.move
		}
	}
	if in.Mood == MoodAggressive || in.Plan == PlanPress {
		count := map[int]int{}
		for _, c := range hand {
			if !c.IsSpecial() {
				count[c.Rank]++
			}
		}
		best := plain[0]
		for _, a := range plain {
			c := hand[a.move.CardID]
			if count[c.Rank] > count[hand[best.move.CardID].Rank] {
				best = a
			}
		}
		return best.move
	}
	return plain[0].move
}

// throwOrPass throws in cheap matching cards and otherwise says "bito".
func (h *Heuristic) throwOrPass(in Context, byType map[game.MoveType][]scored, hand map[string]game.Card, trump string) game.Move {
	s := in.State
	deckLeft := s.DeckCount > 0
	limit := 10
	switch in.Mood {
	case MoodAggressive:
		limit = 14
	case MoodDefensive:
		limit = 8
	}
	if s.DefenderTaking || in.Plan == PlanPress || in.Plan == PlanDump {
		limit = 14 // dump everything that matches on a taker, a drowning defender or out of a bloated hand
		if in.Mood == MoodDefensive && !s.DefenderTaking {
			limit = 10
		}
	}
	counting := (in.Plan == PlanCount || in.Plan == PlanHunt) && in.level().Counts() && !s.DefenderTaking
	for _, t := range byType[game.MoveThrowIn] {
		c := hand[t.move.CardID]
		if c.IsSpecial() {
			continue
		}
		if trump != "" && c.Suit == trump && deckLeft && !(in.Mood == MoodAggressive && s.DefenderTaking) && in.Plan != PlanDump {
			continue
		}
		if counting {
			// with the deck gone every card is a bet: throw what they likely
			// cannot beat, or what is cheap anyway; keep a trump that would
			// only hand them a card they can cover
			p := in.Memory.BeatProb(s, s.DefenderID, c)
			isTrump := trump != "" && c.Suit == trump
			if p < 0.5 || (c.Rank <= limit && !isTrump) {
				return t.move
			}
			continue
		}
		if c.Rank <= limit {
			return t.move
		}
	}
	if ps := byType[game.MovePass]; len(ps) > 0 {
		return ps[0].move
	}
	return in.Moves[0]
}
