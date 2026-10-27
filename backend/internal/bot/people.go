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

// Greet picks a line for a person who just sat down, in the character's voice
// for a stranger and from the dossier for an old acquaintance.
func Greet(rng *rand.Rand, d Dossier) string { return GreetAs(nil, rng, d) }

// GreetAs is Greet with a character's own words for strangers.
func GreetAs(ch *Character, rng *rand.Rand, d Dossier) string {
	var pool []string
	switch {
	case d.Games == 0 && ch != nil && len(ch.Greet) > 0:
		pool = ch.Greet
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
	return RetortAs(nil, rng, speakerName, speakerCards)
}

// RetortAs answers in a character's voice.
func RetortAs(ch *Character, rng *rand.Rand, speakerName string, speakerCards int) string {
	pool := Retorts
	if ch != nil && len(ch.Retort) > 0 {
		pool = ch.Retort
	}
	line := pool[rng.IntN(len(pool))]
	line = strings.ReplaceAll(line, "{a}", speakerName)
	return strings.ReplaceAll(line, "{c}", fmt.Sprint(speakerCards))
}

// PromoLines: a bot that lost at this table comes back a level stronger.
var PromoLines = map[Level][]string{
	LevelNormal: {"Проиграл — поумнел. Теперь играю всерьёз", "Ладно. Разминка кончилась", "В прошлый раз я поддавался. Честно"},
	LevelHard:   {"Всё. Считаю каждую карту", "Больше никаких подарков. Ни одного", "Теперь я помню всё, что вы кладёте"},
}

// Promo returns a line for a bot promoted to lvl, or "" when there is none.
func Promo(rng *rand.Rand, lvl Level) string { return PromoAs(nil, rng, lvl) }

// PromoAs is Promo in a character's voice.
func PromoAs(ch *Character, rng *rand.Rand, lvl Level) string {
	pool := PromoLines[lvl]
	if ch != nil && len(ch.Promo) > 0 {
		pool = ch.Promo
	}
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

// --- the three of the communal flat --------------------------------------------

// Character is a bot's personality: a name, an emoji face, a persona for
// the model and pools of lines per event. Three live at this table, one per
// bot seat; everything they say is about the game at hand.
type Character struct {
	Name    string
	Avatar  string
	Persona string // for the model's talk prompt
	// line pools; {n} is the person concerned, {c} a count
	Greet    []string // a stranger sat down
	Took     []string // somebody took three cards or more
	Transfer []string // somebody transferred
	Lonely   []string // somebody is down to one card
	Press    []string // the bot switches to pressing the defender
	Hunt     []string // the bot hunts somebody nearly out
	Retort   []string // answering another bot ({a} is the speaker)
	Promo    []string // came back stronger after losing
	Take     []string // the bot itself takes the table (said rarely)
}

// Characters in seating order of preference.
var Characters = []Character{
	{
		Name: "Вася", Avatar: "🥴",
		Persona:  "Ты Вася из коммуналки: непутёвый, вечно после вчерашнего, занимаешь до пятницы, философствуешь о жизни, но в карты играешь давно и дворовые приёмы знаешь. Говоришь просто, с ленцой, иногда жалуешься на голову и соседей.",
		Greet:    []string{"{n}? О, садись. Займи сотку до пятницы, а?", "{n}, привет. Голова гудит, так что без резких ходов", "Новенький, {n}. Я Вася. Я тут самый честный, это все знают"},
		Took:     []string{"{n}, {c} карт. Я бы за такое выпил", "Бери, {n}. У меня вчера тоже всё брали", "{n} забрал {c}. Ну, бывает. У меня хуже бывало", "Ого, {n}. Это ж целая колода почти"},
		Transfer: []string{"Перевёл? Ну ты, {n}, хитрый, как мой сосед", "{n}, перевод. Я так в понедельник с работы переводился", "Красиво, {n}. Я бы не додумался. Голова"},
		Lonely:   []string{"{n}, одна карта? Не торопись, жизнь длинная", "Последняя, {n}? Я тебя помню с другой стороны стола", "{n}, одна. Как у меня копейка до получки"},
		Press:    []string{"Ну всё, {n}, держи. Полегчало?", "{n}, подкинем, не жадничай", "Давай-давай, {n}, тебе ещё влезет"},
		Hunt:     []string{"{n}, не уйдёшь. Я ж тебя знаю", "Ещё не вечер, {n}", "Подожди, {n}, я ещё не всё положил"},
		Retort:   []string{"{a}, не ворчи. Ходи", "Да ладно тебе, {a}. Все свои", "{a}, у тебя {c} карт. Молчал бы", "Поддерживаю {a}. Но не полностью"},
		Promo:    []string{"Так. Проспался. Теперь по-настоящему", "Хватит поддаваться. Я ж умею, вы просто не видели", "Вчера я был не в форме. Сегодня — в форме"},
		Take:     []string{"Беру. Что поделать", "Ладно, забираю. Как обычно", "Эх. Беру"},
	},
	{
		Name: "Бабуля", Avatar: "👵",
		Persona:  "Ты Бабуля из коммуналки: советская бабушка, помнишь очереди, Брежнева и как играли во дворе на лавочке. Поучаешь, жалеешь, ворчишь, предлагаешь пирожки, но в карты играешь цепко и подкидываешь без жалости. Говоришь тепло, с присказками.",
		Greet:    []string{"{n}, садись, милок. Пирожок хочешь? Нет? Тогда играем", "Ой, {n}. В наше время такие стеснялись за стол садиться", "{n}, деточка. Бабуля тебя научит"},
		Took:     []string{"{n}, {c} карт. Ай-яй-яй. Ешь, пока горячее", "Бери, бери, {n}. В войну и не такое брали", "{n} взял {c}. При Брежневе за это стыдили", "Ничего, {n}, терпи. Карта — она к руке идёт"},
		Transfer: []string{"Перевёл, {n}? Ох и хитрый. Как мой покойный", "{n}, переводишь на старушку? Совести нет", "Ну, {n}, ладно. Я тоже так умею"},
		Lonely:   []string{"{n}, одна карточка? Держись, милок", "Последняя, {n}? Бабуля такое сто раз видела", "{n}, с одной картой далеко не уедешь"},
		Press:    []string{"Держи, {n}, с добавкой", "{n}, ещё ложечку", "Кушай, {n}, не стесняйся"},
		Hunt:     []string{"{n}, стой, куда собрался", "Не спеши, {n}, чай не на поезд", "{n}, посиди ещё с бабулей"},
		Retort:   []string{"{a}, не умничай. Ходи", "Ой, {a}, помолчи. Голова от тебя", "{a}, у тебя {c} карт, а разговоров-то", "Вот и я говорю, {a}"},
		Promo:    []string{"Ну всё, детки. Бабуля очки надела", "Теперь без поддавков. Я вас предупредила", "В прошлый раз пожалела. Больше не буду"},
		Take:     []string{"Ладно, беру. Всё в хозяйство", "Беру, беру. Пригодится", "Ох. Возьму"},
	},
	{
		Name: "Крест", Avatar: "🕶",
		Persona:  "Ты Крест из коммуналки: вор в законе на покое, авторитет, спокойный и немногословный, в карты играешь всю жизнь и знаешь цену каждому ходу. Говоришь коротко, с достоинством, немного на фене, без крика: любая угроза у тебя тихая и с усмешкой.",
		Greet:    []string{"{n}. Садись. За столом по-честному", "{n}. Правила простые: взял — не плачь", "Новенький, {n}? Карты не крапленые, я проверял"},
		Took:     []string{"{n}, {c} карт. Ответственно взял", "Взял, {n}? Значит, так надо", "{n}, {c} карт. На зоне за такое уважают", "Красиво, {n}. Теперь думай, куда их"},
		Transfer: []string{"Перевод, {n}? Грамотно", "{n}, переводишь на меня? Смело", "Принял, {n}. Запомнил"},
		Lonely:   []string{"{n}, одна карта. Не суетись", "Последняя, {n}? Тише едешь", "{n}, одна. Самое интересное начинается"},
		Press:    []string{"{n}, держи. Ничего личного", "Добавим, {n}", "{n}, это ещё не всё"},
		Hunt:     []string{"{n}, не уйдёшь", "Спокойно, {n}. Ты никуда не торопишься", "{n}, я всё вижу"},
		Retort:   []string{"{a}, тише. Люди играют", "{a}, за базар ответишь. Картами", "{a}, {c} карт у тебя. Помолчи", "Слушай {a}. Иногда"},
		Promo:    []string{"Всё. Разминка кончилась", "Теперь всерьёз. Без обид", "Считаю каждую карту. Каждую"},
		Take:     []string{"Беру. Запомнил", "Возьму. Потом верну", "Беру"},
	},
}

// Names is kept for callers that only need the list of bot names.
var Names = func() []string {
	out := make([]string, len(Characters))
	for i, c := range Characters {
		out[i] = c.Name
	}
	return out
}()

// CharacterOf finds a character by name; an unknown name gets the first one.
func CharacterOf(name string) *Character {
	for i := range Characters {
		if Characters[i].Name == name {
			return &Characters[i]
		}
	}
	return &Characters[0]
}

// Say picks a line of the character for an event kind (took, transfer,
// lonely, press, hunt, take), filled with the name and count; "" when the
// character has nothing for it.
func (c *Character) Say(rng *rand.Rand, kind, name string, count int) string {
	var pool []string
	switch kind {
	case "took":
		pool = c.Took
	case "transfer":
		pool = c.Transfer
	case "lonely":
		pool = c.Lonely
	case "press":
		pool = c.Press
	case "hunt":
		pool = c.Hunt
	case "take":
		pool = c.Take
	}
	if len(pool) == 0 {
		return ""
	}
	line := pool[rng.IntN(len(pool))]
	if name == "" {
		name = "дружище"
	}
	line = strings.ReplaceAll(line, "{n}", name)
	return strings.ReplaceAll(line, "{c}", fmt.Sprint(count))
}
