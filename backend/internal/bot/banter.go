package bot

import (
	"fmt"
	"math"
	"math/rand/v2"
	"strings"

	"github.com/breezeareasay/durag/backend/internal/game"
)

// Banter: when a bot talks and what it says without a model.
//
// Cadence. Instead of "every N-th move" (predictable) or a coin flip (long
// silences, bursts), a bot speaks on its i-th move when frac(i·φ) < 1/N,
// φ being the golden ratio. The sequence {i·φ} is the most evenly spread
// irrational rotation there is (Weyl, three-distance theorem): over any
// window of N moves the bot speaks about once, never twice in a row for
// N ≥ 2, yet the pattern does not repeat.

// SpeakEvery is the mean number of a bot's own moves between two lines.
// Six: a line now and then, not a commentary track; the room cooldown in
// the hub keeps two bots from talking over each other.
const SpeakEvery = 3

const phi = 1.618033988749895

// Due reports whether a bot that made `turn` moves this game talks now.
func Due(turn, every int) bool {
	if every <= 1 {
		return true
	}
	f := float64(turn) * phi
	return f-math.Floor(f) < 1/float64(every)
}

// Event is the last thing that happened on the table, seen from the bot:
// what to tease somebody about.
type Event struct {
	Kind     string // took, bito, transfer, lonely, attack, defend, overkill, trump, deck_out, pass, ""
	Name     string // the player it concerns (never the bot itself)
	PlayerID string
	Count    int    // cards: taken, left in hand...
	Suit     string // the suit of an opening attack
	Bot      bool   // the player is another bot
}

// Notable reports whether the event deserves a line of its own: a big take,
// a transfer, somebody down to the last card. Ordinary attacks, defences
// and passes happen every move and are left alone.
func (e Event) Notable() bool {
	switch e.Kind {
	case "took":
		return e.Count >= 2
	case "transfer", "lonely", "overkill", "trump", "deck_out":
		return true
	}
	return false
}

// String renders the event for the model's prompt ("" for nothing notable).
func (e Event) String() string {
	switch e.Kind {
	case "took":
		return fmt.Sprintf("%s только что взял %d карт", e.Name, e.Count)
	case "bito":
		return fmt.Sprintf("%s отбился, бито", e.Name)
	case "transfer":
		return fmt.Sprintf("%s перевёл на тебя", e.Name)
	case "lonely":
		return fmt.Sprintf("у %s осталась одна карта", e.Name)
	case "pass":
		return fmt.Sprintf("%s больше не подкидывает", e.Name)
	case "attack":
		return fmt.Sprintf("%s зашёл", e.Name)
	case "defend":
		return fmt.Sprintf("%s отбил карту", e.Name)
	}
	return ""
}

// LastEvent finds the most recent notable thing another player did. A
// human down to one card beats everything: that is the moment to needle.
func LastEvent(s *game.GameState, botID string) Event {
	for _, p := range s.Players {
		if p.ID != botID && !p.IsBot && !p.Out && p.HandCount == 1 && s.DeckCount == 0 {
			return Event{Kind: "lonely", Name: p.Name}
		}
	}
	// the deck ran dry just now: everybody's event, the table changes gear
	if s.DeckCount == 0 && len(s.Log) > 0 {
		for i := len(s.Log) - 1; i >= 0 && i >= len(s.Log)-3; i-- {
			if s.Log[i].Type == "took" || s.Log[i].Type == "bito" {
				if s.DiscardCount > 0 && deckJustEmptied(s) {
					return Event{Kind: "deck_out"}
				}
				break
			}
		}
	}
	// people first; another bot's doing only when no person did anything notable lately
	for _, botsToo := range []bool{false, true} {
		for i := len(s.Log) - 1; i >= 0 && i >= len(s.Log)-6; i-- {
			e := s.Log[i]
			if e.PlayerID == "" || e.PlayerID == botID {
				continue
			}
			p := s.Player(e.PlayerID)
			if p == nil || (p.IsBot && !botsToo) {
				continue
			}
			ev := Event{Name: p.Name, PlayerID: p.ID, Bot: p.IsBot}
			switch e.Type {
			case "took":
				fmt.Sscan(e.Text, &ev.Count)
				ev.Kind = "took"
			case "bito":
				ev.Kind = "bito"
			case "attack":
				ev.Kind = "attack"
				if c, ok := game.CardByID(e.CardID); ok && !c.IsSpecial() {
					ev.Suit = c.Suit
				}
			case "defend":
				ev.Kind = "defend"
				// a trump on a six: a cover that cost far more than the card it answered
				if c, ok := game.CardByID(e.CardID); ok && !c.IsSpecial() && c.Suit == s.ActiveTrump() && s.ActiveTrump() != "" {
					if a, ok := game.CardByID(e.TargetID); ok && !a.IsSpecial() && a.Suit != c.Suit && a.Rank <= 8 {
						ev.Kind = "overkill"
						ev.Suit = c.Suit
					}
				}
			case "trump_revealed":
				ev.Kind = "trump"
				ev.Suit = s.TrumpSuit
			case "transfer", "pass":
				ev.Kind = e.Type
			default:
				continue
			}
			return ev
		}
	}
	return Event{}
}

// Jabs are the lines the rule-based brain uses: short, cheeky, never rude.
// {n} is the player's name, {c} a count.
var Jabs = map[string][]string{
	"took": {
		"{n}, {c} карт? Коллекционируешь?",
		"Бери-бери, {n}, у меня ещё есть",
		"{n} взял. Классика жанра",
		"Это была ловушка, {n}",
	},
	"bito": {
		"Повезло, {n}. Пока",
		"Отбился? Ну-ну, {n}",
		"Разминка, {n}. Сейчас начнётся",
	},
	"transfer": {
		"Перевод? Серьёзно, {n}?",
		"{n}, это ты зря",
		"Ладно, {n}, принято. Запомнил",
	},
	"lonely": {
		"{n}, одна карта? Не спеши",
		"Последняя, {n}? Я тоже так думал",
		"Держись, {n}, финал близко",
	},
	"pass": {
		"Сдулся, {n}?",
		"Больше нечем, {n}? Жаль",
	},
	"attack": {
		"И это заход, {n}?",
		"Смело, {n}. Глупо, но смело",
		"{n}, ты уверен?",
	},
	"defend": {
		"Неплохо, {n}. Для новичка",
		"Отбил. Случайно, {n}?",
	},
	"overkill": {
		"Козырем по шестёрке, {n}? Щедро",
		"{n}, ты козырь на мелочь потратил. Я запомнил",
		"Богато живёшь, {n}. Козырями отбиваться от мусора",
		"{n}, козырь ушёл. На что ты его потратил, напомни?",
	},
	"trump": {
		"Козырь {s}. Теперь понятно, кто тут при деньгах",
		"{s} козырь. У кого на руках, тот и молчит",
		"Открыли {s}. Я свои теперь берегу, а вы как хотите",
	},
	"deck_out": {
		"Колода кончилась. Теперь считаем, кто что держит",
		"Всё, колода пустая. Дальше по-честному: кто что набрал",
		"Колоды нет. Самое интересное начинается",
	},
	"": {
		"Козырь у меня. Наверное",
		"Кто-то тут нервничает",
		"Пенёк мой, не трогать",
		"Скучно. Давайте быстрее",
	},
}

// Jab picks a line for an event.
func Jab(rng *rand.Rand, ev Event) string {
	pool := Jabs[ev.Kind]
	if len(pool) == 0 {
		pool = Jabs[""]
	}
	line := pool[rng.IntN(len(pool))]
	line = strings.ReplaceAll(line, "{n}", ev.Name)
	line = strings.ReplaceAll(line, "{s}", suitGenitive(ev.Suit))
	return strings.ReplaceAll(line, "{c}", fmt.Sprint(ev.Count))
}

// deckJustEmptied: the last refill drained the deck (no "took"/"bito" entry
// before this one happened with the deck already empty).
func deckJustEmptied(s *game.GameState) bool {
	seen := 0
	for i := len(s.Log) - 1; i >= 0; i-- {
		switch s.Log[i].Type {
		case "took", "bito":
			seen++
			if seen == 2 {
				return false
			}
		case "trump_revealed":
			return true // the last card out of the deck is the trump: that was this refill
		}
	}
	return false
}
