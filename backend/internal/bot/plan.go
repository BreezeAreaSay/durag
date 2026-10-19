package bot

import (
	"math/rand/v2"
	"strings"

	"github.com/breezeareasay/durag/backend/internal/game"
)

// Level is how well a bot plays. One knob does it: the chance that a move
// is picked at random among the card moves instead of by the plan. Cheap,
// and it reads right at the table: a weak player is not a player with a
// bad plan, it is one who sometimes does not look.
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
		return 0.35
	case LevelHard:
		return 0
	}
	return 0.1
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

// MemoryLines are the lines about memory: a card the human threw that the
// bot now returns, and a card the bot knew the human was holding.
var (
	ReturnLines = []string{"Помнишь, {n}? Ты мне это подкидывал", "{n}, твоя же карта. Возвращаю", "Узнаёшь, {n}? Твоё", "Держи обратно, {n}, не благодари"}
	RecallLines = []string{"А, та самая {c}. Помню, {n}", "{n}, я знал, что {c} у тебя", "{c}? Я её у тебя с прошлого кона считаю", "Ждал эту {c}, {n}"}
)

func fill(line, name, card string) string {
	line = strings.ReplaceAll(line, "{n}", name)
	return strings.ReplaceAll(line, "{c}", card)
}
