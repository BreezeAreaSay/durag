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
const SpeakEvery = 6

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
	Kind     string // took, bito, transfer, lonely, attack, defend, pass, ""
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
		return e.Count >= 3
	case "transfer", "lonely":
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
			case "transfer", "pass", "defend":
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
	return strings.ReplaceAll(line, "{c}", fmt.Sprint(ev.Count))
}
