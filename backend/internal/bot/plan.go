package bot

import (
	"math/rand/v2"
	"strings"

	"github.com/breezeareasay/durag/backend/internal/game"
)

// Level is how well a bot plays. Two knobs: how often a move is picked at
// random instead of by the plan, and whether the bot counts cards. Which
// card goes hardly decides a game of Durak; taking a table you could have
// covered, or passing with a card to throw, does. So an easy bot sometimes
// picks any legal move at all, a normal one only slips on the choice of
// card, a hard one never slips and plays the odds.
type Level string

const (
	LevelEasy   Level = "easy"
	LevelNormal Level = "normal"
	LevelHard   Level = "hard"
)

// Levels in order of strength.
var Levels = []Level{LevelEasy, LevelNormal, LevelHard}

// ParseLevel reads a level from the wire; anything unknown is normal.
func ParseLevel(s string) Level {
	switch Level(strings.ToLower(strings.TrimSpace(s))) {
	case LevelEasy:
		return LevelEasy
	case LevelHard:
		return LevelHard
	}
	return LevelNormal
}

// Blunder is the chance of a random card move at this level.
func (l Level) Blunder() float64 {
	switch l {
	case LevelEasy:
		return 0.2
	case LevelHard:
		return 0
	}
	return 0.03
}

// Slip is the chance of any random legal move (a needless take, a pass with
// a card to throw): the mistakes that actually lose games. Easy only.
func (l Level) Slip() float64 {
	if l == LevelEasy {
		return 0.18
	}
	return 0
}

// Counts reports whether the bot plays the numbers (memory and odds).
func (l Level) Counts() bool { return l != LevelEasy }

// Plan is the line a bot follows in a situation. It changes a few times a
// game, as the table changes, and the bot may say so.
type Plan string

const (
	PlanSave  Plan = "save"  // the deck is alive: keep trumps and specials, lead low
	PlanPress Plan = "press" // the defender is drowning or taking: pile on
	PlanDump  Plan = "dump"  // my hand is bloated: shed anything that goes
	PlanCount Plan = "count" // the deck is empty: play the odds of every card
	PlanHunt  Plan = "hunt"  // somebody is about to go out: feed them what they cannot beat
)

// ChoosePlan reads the situation. Hunting and counting need an empty deck
// (the odds mean something then); pressing and dumping are about hand
// sizes and beat the default while the deck lasts.
func ChoosePlan(s *game.GameState, botID string) Plan {
	me := s.Player(botID)
	if me == nil {
		return PlanSave
	}
	if s.DeckCount == 0 {
		for _, p := range s.Players {
			if p.ID != botID && !p.Out && p.HandCount > 0 && p.HandCount <= 2 {
				return PlanHunt
			}
		}
		return PlanCount
	}
	if d := s.Defender(); d != nil && d.ID != botID && (s.DefenderTaking || d.HandCount >= 7) {
		return PlanPress
	}
	if len(me.Hand) >= 9 {
		return PlanDump
	}
	return PlanSave
}

// PlanLines are what a bot may say when it changes its plan: {n} is the
// defender's name where the plan is about them.
var PlanLines = map[Plan][]string{
	PlanPress: {"Ну всё, {n}, давлю", "{n}, держи, тебе ещё влезет", "Пресс пошёл, {n}"},
	PlanDump:  {"Сбрасываю балласт", "Многовато у меня. Раздаю", "Разгружаюсь, не обессудьте"},
	PlanCount: {"Колода пуста. Считаю", "Теперь я всё помню", "Пошла математика"},
	PlanHunt:  {"{n}, не уйдёшь", "Охота открыта, {n}", "{n}, ещё не вечер"},
}

// PlanLine picks a line for a plan change, or "" when the plan is quiet.
func PlanLine(rng *rand.Rand, p Plan, defenderName string) string {
	pool := PlanLines[p]
	if len(pool) == 0 {
		return ""
	}
	line := pool[rng.IntN(len(pool))]
	if defenderName == "" {
		defenderName = "дружище"
	}
	return strings.ReplaceAll(line, "{n}", defenderName)
}

// PlanText describes a plan for the model's prompt.
func PlanText(p Plan) string {
	switch p {
	case PlanPress:
		return "ПРЕСС: защитник тонет или берёт — подкидывай всё, что подходит, кроме спецкарт"
	case PlanDump:
		return "СБРОС: у тебя слишком много карт — избавляйся от дешёвых при любой возможности"
	case PlanCount:
		return "СЧЁТ: колода пуста, ты помнишь вышедшие карты — заходи тем, что соперник вряд ли побьёт, береги сильные карты до конца"
	case PlanHunt:
		return "ОХОТА: кто-то почти вышел — не дай ему отбиться, заходи тем, что он точно не побьёт, и не подкидывай то, что он может побить"
	}
	return "ЭКОНОМИЯ: колода жива — заходи мелочью, козыри и спецкарты береги на защиту"
}
