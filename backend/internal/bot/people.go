package bot

import (
	"fmt"
	"math/rand/v2"
	"strings"

	"github.com/breezeareasay/durag/backend/internal/game"
)

// Dossier is what the table remembers about a person: the bots greet and
// needle with it. Everything is small integers the store already keeps.
type Dossier struct {
	Name      string
	Games     int
	Losses    int
	WorstTake int    // the biggest single take ever
	FavSuit   string // the suit they open with most, "" for no habit
	Scar      bool   // lost their last game
}

// String renders the dossier for the model's prompt ("" for a stranger).
func (d Dossier) String() string {
	if d.Games == 0 {
		return ""
	}
	parts := []string{fmt.Sprintf("%d игр, %d проигрышей", d.Games, d.Losses)}
	if d.WorstTake >= 5 {
		parts = append(parts, fmt.Sprintf("рекорд позора — взял %d карт за раз", d.WorstTake))
	}
	if d.FavSuit != "" {
		parts = append(parts, "любит заходить с "+suitGenitive(d.FavSuit))
	}
	if d.Scar {
		parts = append(parts, "носит клеймо дурака с прошлой игры")
	}
	return strings.Join(parts, ", ")
}

func suitGenitive(suit string) string {
	switch suit {
	case game.SuitHearts:
		return "червей"
	case game.SuitDiamonds:
		return "бубён"
	case game.SuitClubs:
		return "крестей"
	case game.SuitSpades:
		return "пик"
	}
	return suit
}

// Greetings by what the dossier holds; {n} the name, {c} the worst take,
// {s} the favourite suit, {g} games, {l} losses.
var (
	GreetStranger = []string{"{n}? Не видел тебя тут. Садись, сейчас увидим", "Новенький. {n}, правила знаешь? Сейчас узнаешь", "О, {n}. Свежая кровь"}
	GreetWorst    = []string{"О, {n}. Прошлый раз ты взял {c} карт, помнишь?", "{n}! Тот самый, который взял {c} карт за раз", "Садись, {n}. {c} карт в одну руку — я такое не забываю"}
	GreetSuit     = []string{"{n}, опять с {s} зайдёшь? Я уже готов", "Ставлю пенёк, что {n} начнёт с {s}", "{n} и его {s}. Классика"}
	GreetScar     = []string{"{n}, клеймо ещё не смыл? Ну давай, попробуй", "Дурак прошлой игры, {n}. Есть шанс исправиться", "{n}, с меткой. Я бы на твоём месте взял реванш"}
	GreetLoser    = []string{"{n}, {l} из {g}. Статистика не врёт", "О, {n}. {l} проигрышей, но кто считает. Я считаю", "{n}, ты снова? Смелый"}
	GreetRegular  = []string{"{n}, {g} игр за этим столом. Уважаю", "Опять ты, {n}. Садись, не чужой", "{n}, старый знакомый. Ну что, по-честному?"}
)

// Greet picks a line for a person who just sat down.
func Greet(rng *rand.Rand, d Dossier) string {
	var pool []string
	switch {
	case d.Games == 0:
		pool = GreetStranger
	case d.WorstTake >= 7 && rng.Float64() < 0.6:
		pool = GreetWorst
	case d.Scar && rng.Float64() < 0.7:
		pool = GreetScar
	case d.FavSuit != "" && rng.Float64() < 0.5:
		pool = GreetSuit
	case d.Games >= 3 && d.Losses*2 >= d.Games:
		pool = GreetLoser
	default:
		pool = GreetRegular
	}
	return fillDossier(pool[rng.IntN(len(pool))], d)
}

func fillDossier(line string, d Dossier) string {
	r := strings.NewReplacer("{n}", d.Name, "{c}", fmt.Sprint(d.WorstTake), "{s}", suitGenitive(d.FavSuit), "{g}", fmt.Sprint(d.Games), "{l}", fmt.Sprint(d.Losses))
	return r.Replace(line)
}

// HabitLines: the person opened with the suit they always open with.
var HabitLines = []string{"Опять с {s}, {n}? Предсказуемо", "{n}, я же говорил: {s}", "{s}. Кто бы сомневался, {n}"}

// Habit returns a line when the lead matches the dossier, else "".
func Habit(rng *rand.Rand, d Dossier, suit string) string {
	if d.FavSuit == "" || suit != d.FavSuit {
		return ""
	}
	return fillDossier(HabitLines[rng.IntN(len(HabitLines))], d)
}

// Retorts: one bot answering another's line; {a} the speaker, {b} the one
// answering (never used in the text, the bubble shows who talks).
var Retorts = []string{
	"{a}, ты бы молчал. У тебя {c} карт",
	"Слышь, {a}, твои шутки хуже твоих заходов",
	"{a}, тебя спросили? Нет",
	"Да, да, {a}. Очень смешно. Ходи",
	"{a} опять умничает. Админ, выключи его",
	"{a}, я с тобой в бито не сяду",
	"Поддерживаю {a}. Но не полностью",
	"{a}, запиши, потом расскажешь",
}

// Retort picks an answer to another bot's line.
func Retort(rng *rand.Rand, speakerName string, speakerCards int) string {
	line := Retorts[rng.IntN(len(Retorts))]
	line = strings.ReplaceAll(line, "{a}", speakerName)
	return strings.ReplaceAll(line, "{c}", fmt.Sprint(speakerCards))
}

// PromoLines: a bot that lost at this table comes back a level stronger.
var PromoLines = map[Level][]string{
	LevelNormal: {"Проиграл — поумнел. Теперь играю всерьёз", "Ладно. Разминка кончилась", "В прошлый раз я поддавался. Честно"},
	LevelHard:   {"Всё. Считаю каждую карту", "Больше никаких подарков. Ни одного", "Теперь я помню всё, что вы кладёте"},
}

// Promo returns a line for a bot promoted to lvl, or "" when there is none.
func Promo(rng *rand.Rand, lvl Level) string {
	pool := PromoLines[lvl]
	if len(pool) == 0 {
		return ""
	}
	return pool[rng.IntN(len(pool))]
}

// Promote returns the next level up (hard stays hard).
func Promote(l Level) Level {
	switch l {
	case LevelEasy:
		return LevelNormal
	default:
		return LevelHard
	}
}
