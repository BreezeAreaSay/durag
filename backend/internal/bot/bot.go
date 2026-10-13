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
}

// Decision is a legal move plus an optional short line and an optional
// emoji reaction shown next to the bot's avatar.
type Decision struct {
	Move      game.Move
	Say       string
	Emoji     string
	AdminJoke bool // Say is the fourth-wall line: do not repeat it this game
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
	d := Decision{Move: h.pick(in)}
	if in.AdminJokeDue && h.rng.Float64() < 0.3 {
		d.Say = AdminJokes[h.rng.IntN(len(AdminJokes))]
		d.AdminJoke = true
	}
	if d.Move.Type == game.MoveTake && h.rng.Float64() < 0.3 {
		d.Emoji = []string{"💀", "😡", "🤔"}[h.rng.IntN(3)]
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
	// spending rules: specials and big trumps are kept while the deck lasts
	if canTake && deckLeft {
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
	if in.Mood == MoodAggressive {
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
	if s.DefenderTaking {
		limit = 14 // dump everything that matches on a taker
		if in.Mood == MoodDefensive {
			limit = 10
		}
	}
	for _, t := range byType[game.MoveThrowIn] {
		c := hand[t.move.CardID]
		if c.IsSpecial() {
			continue
		}
		if trump != "" && c.Suit == trump && deckLeft && !(in.Mood == MoodAggressive && s.DefenderTaking) {
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
