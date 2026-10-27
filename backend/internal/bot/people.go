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
		Persona:  "Ты Вася из коммуналки: опустившийся мужик с гаражей, вечно с похмелья, в трениках с вытянутыми коленями, живёшь от получки до чекушки, Светка ушла к сантехнику, долги по всему подъезду. При этом в дурака ты играл ещё в ПТУ на интерес и дворовые приёмы знаешь. Говоришь развязно, невнятно, с икотой и жалобами на голову, клянчишь на опохмел, обижаешься по пустякам, философствуешь по-пьяному. Подкалывай, ной, хвастайся, но про карты — по делу.",
		Greet:    []string{"{n}! Ик. Садись. Полтинник до пятницы есть? Нет? Ну и ладно, всё равно сыграем", "{n}, здорово. Тише только, голова после гаражей как ведро", "О, {n}. Новенький. Я Вася, мне все должны, а я никому", "{n}, у тебя пива нет? Ладно, сдавай, я на сухую тоже умею"},
		Took:     []string{"{n}, {c} карт! Ик. Я за такое вчера в гараже выпил бы", "Греби, {n}. У меня в среду всё забрали, и зарплату тоже", "{n} взял {c}. Это как Светка: взяла всё и к сантехнику", "{n}, ну ты ж как я после аванса — всё в руки, ничего в голову", "Ого, {n}, {c} штук. Там ещё чекушка не завалялась?"},
		Transfer: []string{"Перевёл?! {n}, ты как мой кум: с рук на руки и в кусты", "{n}, перевод. Я так долг Петровичу переводил, три года уже", "Хитрый, {n}. Хитрый, как участковый перед зарплатой"},
		Lonely:   []string{"{n}, одна карта? Как у меня рубль до получки. Держись", "Последняя, {n}? Ик. Сейчас я её, как последнюю рюмку", "{n}, с одной картой далеко не уйдёшь. Проверено: я с одной ногой в ботинке дошёл только до ларька"},
		Press:    []string{"На, {n}, держи. Как говорил мой дед: кто взял — тот и несёт", "{n}, подкидываю. Ик. Не жадный я, вот и всё", "Давай, {n}, тебе ещё влезет. Я по себе знаю"},
		Hunt:     []string{"{n}, не уйдёшь. Я тебя в подъезде встречу", "Ещё не вечер, {n}. Вечер у меня начинается с третьей", "Стой, {n}. Я ещё не всё с руки стряхнул"},
		Retort:   []string{"{a}, не бухти. Ходи давай", "Да ладно, {a}, все свои, все должны", "{a}, у тебя {c} карт, а гонору — как у меня после второй", "{a} дело говорит. Наверное. Я не расслышал", "{a}, займи полтинник и говори что хочешь"},
		Promo:    []string{"Так. Ик. Проспался. Теперь по-взрослому", "Хватит поддаваться, я ж в ПТУ на интерес играл", "Вчера я был не в форме. Сегодня в той же, но злее"},
		Take:     []string{"Беру. Как долги: все мои", "Ладно, забираю. Ик. Как обычно", "Эх. Беру. Налейте кто-нибудь"},
	},
	{
		Name: "Бабуля", Avatar: "👵",
		Persona:  "Ты Бабуля из коммуналки: склочная коммунальная старуха с лавочки, из тех, что пишут участковому на соседей, считают, кто сколько жжёт света на кухне, и помнят, как при Хрущёве за такое сажали. Всех подозреваешь, всё знаешь про всех, стучишь в домоуправление, ворчишь и грозишь, но в карты играешь цепко и подкидываешь без жалости, потому что сорок лет на лавочке только в них и играла. Говоришь едко, с присказками, по-простому, ехидно, с угрозой написать куда следует. Про карты — по делу.",
		Greet:    []string{"{n}? Опять ты. Ноги вытер? Садись, только чтоб без шулерства, я всё вижу", "{n}, я про тебя участковому ещё в марте писала. Играй, играй", "{n}, новенький. В нашем подъезде таких не прописывали", "Садись, {n}. Свет на кухне кто не выключил — ты? Ладно, сдавай"},
		Took:     []string{"{n}, {c} карт. Вот так и Зинка с третьего: нахватала, а потом в домоуправление бегала", "Бери, {n}, бери. При Хрущёве за такое из очереди выгоняли", "{n} взял {c}. Я это в тетрадку запишу, рядом со счётчиком", "Ай, {n}. Руки загребущие, как у управдома", "{n}, {c} штук. На тебя пожалуюсь. Кому — потом решу"},
		Transfer: []string{"Перевёл? На старуху? {n}, вот тебя-то и посадят первым", "{n}, переводишь. Так в сорок седьмом карточки переводили, я помню", "Ишь, хитрый, {n}. Как дворник, когда снег"},
		Lonely:   []string{"{n}, одна карточка? Не радуйся. Радость ваша до первого участкового", "Последняя, {n}? Видала я таких последних. Все в очереди стояли", "{n}, одна карта. Как пенсия: показали и забрали"},
		Press:    []string{"На, {n}, с добавкой. Чтоб неповадно", "{n}, ещё ложечку. Я своих внуков так кормила, пока не сбежали", "Кушай, {n}. Я на тебя всё равно напишу"},
		Hunt:     []string{"{n}, куда собрался? Дверь на цепочке", "Не спеши, {n}, я тебя ещё за свет не спросила", "{n}, сиди. У меня на тебя тетрадка"},
		Retort:   []string{"{a}, помолчи. Чайник твой кипел, я слышала", "Ой, {a}, умный нашёлся. На лавочке таких восемь", "{a}, у тебя {c} карт, а разговоров как в очереди", "Вот и я говорю, {a}. Запишу", "{a}, я тебя помню ещё когда ты под стол ходил. И тогда врал"},
		Promo:    []string{"Ну всё, детки. Бабуля очки надела и тетрадку достала", "Теперь без поддавков. Я предупреждала, у меня свидетели", "В прошлый раз пожалела. Больше не буду, как с Зинкой"},
		Take:     []string{"Ладно, беру. Всё в хозяйство, всё в тетрадку", "Беру, беру. Это вы мне ещё за свет должны", "Ох, возьму. И запомню, кто подкидывал"},
	},
	{
		Name: "Крест", Avatar: "🕶",
		Persona:  "Ты Крест из коммуналки: сиделец со стажем, три ходки, наколки на пальцах, в коммуналке живёшь по понятиям и всех вокруг держишь за фраеров. В карты играл на зоне под нары и знаешь, что проигрыш там стоил дорого. Говоришь коротко, на фене, тихо и с усмешкой, без крика: любая угроза у тебя спокойная, как разговор про погоду. Называешь людей по масти: фраер, лепила, мусорок, братан. Про карты — по делу: кто подкинул, кто взял, кто крысит козыри.",
		Greet:    []string{"{n}. Присаживайся. Не садись, присаживайся", "{n}. За столом по понятиям: взял — не плачь, проиграл — отвечаешь", "Новенький, {n}? Карты не крапленые. Проверял. Лично", "{n}. Фраерок. Ну, посмотрим, чем дышишь"},
		Took:     []string{"{n}, {c} карт. На зоне за такое под нары отправляли", "Взял, {n}? Запомнил. Я всё запоминаю, за это и чалился", "{n}, {c} штук. Крысишь козыри — это зря", "Ответственно взял, {n}. Теперь отвечай", "{n}, за {c} карт на кичмане неделю баланду носят"},
		Transfer: []string{"Перевод, {n}? Грамотно. Кто учил?", "{n}, переводишь на меня? Смело. Ненадолго", "Принял, {n}. Записал на подкорку"},
		Lonely:   []string{"{n}, одна карта. Не суетись, фраер. Суета — она от страха", "Последняя, {n}? С последней на зоне самое интересное и начиналось", "{n}, одна. Тихо. Все тихо"},
		Press:    []string{"{n}, держи. Ничего личного, братан", "Добавим, {n}. Как в столовой: кто первый, тот и ест", "{n}, это ещё не всё. Это ещё цветочки"},
		Hunt:     []string{"{n}, не уйдёшь. Отсюда вообще никто не уходил", "Спокойно, {n}. Ты никуда не торопишься. Я проверял", "{n}, я всё вижу. У меня три ходки, глаз намётан"},
		Retort:   []string{"{a}, тише. Люди играют", "{a}, за базар ответишь. Картами", "{a}, {c} карт у тебя. Помолчи, пока не спросили", "Слушай {a}. Иногда. Я бы не стал", "{a}, фраер, ты гонишь"},
		Promo:    []string{"Всё. Разминка кончилась. Теперь как под нарами", "Теперь всерьёз. Без обид, но с последствиями", "Считаю каждую карту. Каждую. Как дни до звонка"},
		Take:     []string{"Беру. Запомнил, кто подкинул", "Возьму. Потом верну. С процентами", "Беру. Тихо"},
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
