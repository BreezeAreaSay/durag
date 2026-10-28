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
	// the bright side: what to respect
	Wins       int // games won (first out)
	Streak     int // wins in a row right now
	BestStreak int // the longest run of wins ever
}

// WinRate is the share of games won, 0..1 (0 for a stranger).
func (d Dossier) WinRate() float64 {
	if d.Games == 0 {
		return 0
	}
	return float64(d.Wins) / float64(d.Games)
}

// LossRate is the share of games lost as the fool, 0..1.
func (d Dossier) LossRate() float64 {
	if d.Games == 0 {
		return 0
	}
	return float64(d.Losses) / float64(d.Games)
}

// Respected tells whether the table owes this person respect: a run of
// wins, or a record over enough games.
func (d Dossier) Respected() bool {
	return d.Streak >= 2 || (d.Games >= 5 && d.WinRate() >= 0.5) || (d.Games >= 5 && d.LossRate() <= 0.15)
}

// String renders the dossier for the model's prompt ("" for a stranger).
func (d Dossier) String() string {
	if d.Games == 0 {
		return ""
	}
	parts := []string{fmt.Sprintf("%d игр, %d побед, %d проигрышей", d.Games, d.Wins, d.Losses)}
	if d.Streak >= 2 {
		parts = append(parts, fmt.Sprintf("выиграл %d раз подряд, серия продолжается", d.Streak))
	}
	if d.BestStreak >= 3 && d.BestStreak > d.Streak {
		parts = append(parts, fmt.Sprintf("рекорд — %d побед подряд", d.BestStreak))
	}
	if d.Games >= 5 {
		parts = append(parts, fmt.Sprintf("проигрывает только в %d%% партий", int(d.LossRate()*100+0.5)))
	}
	if d.Respected() {
		parts = append(parts, "сильный игрок, за столом его уважают: подколы с уважением, без снисходительности")
	}
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
	GreetStreak   = []string{"{n}! {k} раз подряд нас обыграл. Сегодня оставим его в дураках?", "О, {n}. {k} побед подряд. Крест, ты видел? Садись, {n}, с уважением", "{n} идёт на {k}-ю подряд. Кто-нибудь, остановите его", "{n}, серия {k}. Я сегодня против тебя лично"}
	GreetRecord   = []string{"{n}. По статистике проигрывает только в {p}% партий. Садись, чемпион", "Это {n}: {w} побед из {g}. Я бы на вашем месте напрягся", "{n}, у тебя {w} из {g}. Уважение за столом есть, подарков не будет", "{n}, рекорд — {b} подряд. Сегодня не повторишь, я прослежу"}
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
	case d.Streak >= 2 && rng.Float64() < 0.8:
		pool = GreetStreak
	case d.Games >= 5 && (d.WinRate() >= 0.5 || d.LossRate() <= 0.15) && rng.Float64() < 0.7:
		pool = GreetRecord
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
	r := strings.NewReplacer("{n}", d.Name, "{c}", fmt.Sprint(d.WorstTake), "{s}", suitGenitive(d.FavSuit), "{g}", fmt.Sprint(d.Games), "{l}", fmt.Sprint(d.Losses), "{k}", fmt.Sprint(d.Streak), "{w}", fmt.Sprint(d.Wins), "{b}", fmt.Sprint(d.BestStreak), "{p}", fmt.Sprint(int(d.LossRate()*100+0.5)))
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
	// Barks: the character cannot talk; every line, the model's too, comes
	// out as a bark.
	Barks bool
}

// Lore is the world the characters share: one flat, one kitchen, one table.
// The model gets it with the persona so that lines from different bots
// refer to the same people, debts and grudges.
const Lore = `Место: коммуналка в доме 7 по Коммунальной улице, квартира 12, восемь комнат, одна кухня, один туалет с очередью, одна лампочка над кухонным столом, за которым и играют в дурака. Стол клеёнчатый, в пятнах, с прожжёнными точками от сигарет; над ним вечно кружит муха, которую все обещают прихлопнуть. Жильцы: Вася (комната у входа, должен всем: Петровичу 500 рублей, Бабуле за свет, Кресту «по понятиям»), Бабуля (комната у кухни, ведёт тетрадку, кто сколько жжёт света, кормит Шарика, Гоша — её внук), Крест (самая большая комната, сдаёт угол Бутово, держит квартиру по понятиям), Гоша (внук Бабули, спит на раскладушке у неё, готовится к олимпиаде, мама в другом городе), Вика (дочь Петровича, с отцом не разговаривает, слушает музыку в наушниках), Бутово (снимает угол у Креста, должен ему за два месяца, читает рэп в ванной), Шарик (ничей, живёт в коридоре, любит Бабулю и ненавидит Васины носки), Петрович (сантехник из ЖЭКа, к нему от Васи ушла Светка, Вика его дочь). Общие темы: очередь в туалет, кто не выключил свет на кухне, Светка, долги Васи, муха, протекающий стояк, ночной рэп Бутово, лужа от Шарика в коридоре, пирожки Бабули, которые никто не ест.`

// FinishLines: what the table says when a game ends, about the winner
// ({n}, {k} the streak, {w} wins, {g} games) and the fool ({f}).
var (
	FinishWinStreak = []string{"{n}, {k} подряд. Снимаю шляпу, которой нет", "{k} подряд, {n}. Это уже не везение", "{n} — {k} побед подряд. Кто-нибудь запишите", "Опять {n}. {k}-й раз. Я начинаю что-то подозревать"}
	FinishWinRecord = []string{"{n} выиграл. {w} из {g}, между прочим", "Чемпион стола — {n}. {w} побед. Уважаю, хоть и бесит", "{n}, {w} из {g}. Ты вообще проигрывать умеешь?"}
	FinishWinFirst  = []string{"{n} выиграл. Первая победа, поздравляю. Не привыкай", "О, {n} вышел первым. Бывает и такое", "{n}, с победой. Завтра отыграемся"}
	FinishFoolMe    = []string{"Дурак — я. Запомнили все, расходимся", "Ладно. Я дурак. Сегодня", "Беру клеймо. Временно"}
	FinishFoolBreak = []string{"{f} в дураках. А серия-то была {k}. Была", "Ну что, {f}, {k} подряд — и приехали. Нормально", "{f}, серия из {k} кончилась. Я тебя предупреждал"}
)

// Finish picks a line about the end of a game: the winner's record first,
// a broken streak of the fool, else a plain word. "" when there is nothing to say.
func Finish(rng *rand.Rand, winner, fool Dossier, winnerBot, foolIsMe bool) string {
	var pool []string
	d := winner
	switch {
	case foolIsMe && rng.Float64() < 0.6:
		pool = FinishFoolMe
	case fool.Name != "" && fool.Streak >= 2: // the dossier is from before this game: the streak just broke
		pool = FinishFoolBreak
		d = fool
	case winnerBot:
		return ""
	case winner.Streak+1 >= 3:
		pool = FinishWinStreak
		d.Streak = winner.Streak + 1 // counting the win just scored
	case winner.Games >= 4 && (winner.Wins+1)*2 >= winner.Games+1:
		pool = FinishWinRecord
		d.Wins, d.Games = winner.Wins+1, winner.Games+1
	case winner.Wins == 0:
		pool = FinishWinFirst
	default:
		return ""
	}
	line := pool[rng.IntN(len(pool))]
	r := strings.NewReplacer("{n}", d.Name, "{f}", fool.Name, "{k}", fmt.Sprint(d.Streak), "{w}", fmt.Sprint(d.Wins), "{g}", fmt.Sprint(d.Games))
	return r.Replace(line)
}

// RespectLines: a strong player took the table ({n}, {c} cards, {k} the
// streak, {p} loss percent) or is a card away from winning again.
var (
	RespectTook = []string{"О, даже {n} берёт. {c} карт. Запишите дату", "{n} взял {c}. Не привыкли мы к такому от тебя", "Ничего, {n}. С {p}% проигрышей одна взятка не считается", "{n}, {c} карт? Серия из {k} сегодня под вопросом"}
	RespectLast = []string{"{n} с одной картой. Опять. Кто-нибудь, сделайте что-нибудь", "Так, {n} на последней. {k} подряд будет, если не остановим", "{n} снова уходит. По статистике мы его не ловим", "Всё, {n} на выходе. Как обычно. Напрягитесь"}
)

// Respect is a word for a respected player's take.
func Respect(rng *rand.Rand, d Dossier, count int) string {
	line := RespectTook[rng.IntN(len(RespectTook))]
	if d.Streak < 2 && strings.Contains(line, "{k}") {
		line = RespectTook[0]
	}
	d.WorstTake = count
	return fillDossier(line, d)
}

// RespectLonely is a word for a respected player down to a card.
func RespectLonely(rng *rand.Rand, d Dossier) string {
	line := RespectLast[rng.IntN(len(RespectLast))]
	if d.Streak < 2 && strings.Contains(line, "{k}") {
		line = RespectLast[0]
	}
	d.Streak++ // the win about to happen
	return fillDossier(line, d)
}

// Barks are what a dog has to say.
var Barks = []string{"Гав!", "Гав-гав!", "Р-р-р… гав", "Гав?", "ГАВ!", "Гав. Гав. Гав", "Р-р-р-р", "Гав-гав-гав!", "*виляет хвостом* Гав", "*чешет ухо* Гав"}

// Voice passes a line through the character: a dog barks whatever it meant.
func (c *Character) Voice(rng *rand.Rand, line string) string {
	if c == nil || !c.Barks || line == "" {
		return line
	}
	return Barks[rng.IntN(len(Barks))]
}

// Characters at the table; a free one is picked at random.
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
		Retort:   []string{"{a}, не бухти. Ходи давай", "Да ладно, {a}, все свои, все должны", "{a}, у тебя {c} карт, а гонору — как у меня после второй", "{a} дело говорит. Наверное. Я не расслышал", "{a}, займи полтинник и говори что хочешь", "{a}, ты туалет на час занял, а теперь умничаешь"},
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
		Retort:   []string{"{a}, помолчи. Чайник твой кипел, я слышала", "Ой, {a}, умный нашёлся. На лавочке таких восемь", "{a}, у тебя {c} карт, а разговоров как в очереди", "Вот и я говорю, {a}. Запишу", "{a}, я тебя помню ещё когда ты под стол ходил. И тогда врал", "{a}, свет на кухне кто не выключил? То-то"},
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
		Retort:   []string{"{a}, тише. Люди играют", "{a}, за базар ответишь. Картами", "{a}, {c} карт у тебя. Помолчи, пока не спросили", "Слушай {a}. Иногда. Я бы не стал", "{a}, фраер, ты гонишь", "{a}, за угол два месяца не плачено, а голос есть"},
		Promo:    []string{"Всё. Разминка кончилась. Теперь как под нарами", "Теперь всерьёз. Без обид, но с последствиями", "Считаю каждую карту. Каждую. Как дни до звонка"},
		Take:     []string{"Беру. Запомнил, кто подкинул", "Возьму. Потом верну. С процентами", "Беру. Тихо"},
	},
	{
		Name: "Гоша", Avatar: "🤓",
		Persona:  "Ты Гоша, пятнадцать лет, ботаник-олимпиадник из соседней квартиры: очки, брекеты, рюкзак с учебниками, в дурака играешь как в задачу по комбинаторике, считаешь вероятности вслух, всех поправляешь, называешь карты «объектами», ссылаешься на Википедию и Хабр, обижаешься, когда не слушают, и боишься, что мама узнает, что ты играешь в карты. Говоришь быстро, заумно и по-детски одновременно. Про карты — по делу, с цифрами.",
		Greet:    []string{"{n}? Здравствуйте. Вероятность, что вы козырного туза держите, 1,8 %. Я посчитал", "{n}, привет. Только маме не говорите, что я тут", "О, {n}. Статистически вы проиграете. Без обид, это матожидание", "{n}, я прочитал про дурака на Хабре. Теперь я непобедим. Теоретически"},
		Took:     []string{"{n}, {c} карт. Это ж экспоненциальный рост, вы в курсе?", "{n} взял {c}. По моей модели это было неизбежно", "{c} карт, {n}. Я бы на вашем месте пересмотрел стратегию. И жизнь", "{n}, {c}. Записываю в табличку"},
		Transfer: []string{"Перевод! {n}, это допустимый ход по правилам v10, я проверил", "{n}, перевели. Элегантно, как лемма", "Перевод на меня? {n}, это некорректно. Хотя… корректно. Блин"},
		Lonely:   []string{"{n}, одна карта. Вероятность выйти — 37 %. Я округлил", "Последняя, {n}? У меня на олимпиаде так же было. Я проиграл", "{n}, одна. Не спешите, в задаче ещё три пункта"},
		Press:    []string{"Держите, {n}. Это по формуле", "{n}, ещё объект. Тестируем гипотезу", "Добавляю, {n}. Для чистоты эксперимента"},
		Hunt:     []string{"{n}, не уйдёте. Я посчитал все ветки", "Стойте, {n}, у меня доказательство не закончено", "{n}, подождите, я ещё не вывел формулу"},
		Retort:   []string{"{a}, это ненаучно", "{a}, у вас {c} карт, а аргументов ноль", "Не согласен с {a}. Но вежливо", "{a}, можно не кричать? У меня уши", "{a}, бабушка сказала, что вы ей за свет должны. Это к слову"},
		Promo:    []string{"Я перечитал теорию. Теперь всё иначе", "Домашку сделал. Играем", "Я нашёл ошибку в модели. Исправил"},
		Take:     []string{"Беру. Это тоже стратегия", "Ладно, беру. Для статистики", "Беру. Мама была права"},
	},
	{
		Name: "Вика", Avatar: "🖤",
		Persona:  "Ты Вика, семнадцать, готесса с чёрными ногтями, в чёрном плаще даже летом, живёшь в этой же коммуналке с мамой, слушаешь что-то мрачное в наушниках, всё вокруг тебе уныло и бренно, людей презираешь, но в дурака играешь сильно, потому что больше заняться нечем. Говоришь лениво, с сарказмом, коротко, про смерть, пустоту и бесполезность всего. Про карты — по делу, но с тоской.",
		Greet:    []string{"{n}. Садись. Всё равно все умрём", "{n}… Ещё один. Ну ладно", "О, {n}. Надеюсь, ты интереснее, чем предыдущие. Вряд ли", "{n}. Не разговаривай со мной. Играй"},
		Took:     []string{"{n}, {c} карт. Как и жизнь: берёшь, а зачем", "{n} взял {c}. Пустота заполняется", "Красиво страдаешь, {n}", "{c} карт, {n}. Тлен"},
		Transfer: []string{"Перевод. {n}, передал боль дальше. Уважаю", "{n}, переводишь. Как будто это что-то меняет", "На меня? {n}, мне всё равно. Почти"},
		Lonely:   []string{"{n}, одна карта. Как одинокий ворон", "Последняя, {n}. Конец всегда близко", "{n}, одна. Не радуйся, радость временна"},
		Press:    []string{"Держи, {n}. Это бессмысленно, но приятно", "{n}, ещё. Страдай", "Добавлю, {n}. Так надо"},
		Hunt:     []string{"{n}, не уйдёшь. Никто не уходит", "Стой, {n}. Я ещё не закончила", "{n}, подожди. Мне скучно без тебя"},
		Retort:   []string{"{a}, заткнись. Пожалуйста", "{a}, у тебя {c} карт и ноль глубины", "{a}… как ты меня утомил", "Угу, {a}. Очень смешно. Нет", "{a}, иди стояк чини. Или кому там ты нужен"},
		Promo:    []string{"Ладно. Поиграю по-настоящему. Один раз", "Проснулась. Зря вы это", "Теперь мне не скучно. Вам будет"},
		Take:     []string{"Беру. Всё равно", "Беру. Темнота моя подруга", "Ладно. Беру. Какая разница"},
	},
	{
		Name: "Бутово", Avatar: "🎤",
		Persona:  "Ты Бутово, рэпер из Южного Бутово, двадцать два, снимаешь комнату в этой коммуналке, читаешь фристайл по любому поводу, в кепке козырьком назад, цепь из рынка, за плечами два трека на Саундклауде и три сотни прослушиваний. Говоришь панчами и рифмами, слова «йоу», «ща», «брат», «флоу», «бэнгер», иногда рифмуешь ход соперника, всё время считаешь, что сейчас порвёшь, но в карты играешь на удачу. Про карты — по делу, но в рифму.",
		Greet:    []string{"Йоу, {n}! Садись, брат, ща будет бэнгер", "{n} на районе, стол в огне, с таким раскладом я на коне", "Здарова, {n}. Это Бутово, детка. Тут не проигрывают. Обычно", "{n}, я тебе трек посвящу, если ты проиграешь. Называется «Дурак»"},
		Took:     []string{"{n} взял {c}, вот это флоу, ща тебе прочитаю про любовь", "Йоу, {n}, {c} карт в руке, это как мой батя в кабаке", "{c} штук, {n}, это не игра, это бит, и ты на нём разбит", "{n}, {c} карт. Брат, это уже альбом"},
		Transfer: []string{"Перевод, йоу! {n}, ты как диджей, перекинул и забыл", "{n} перевёл, ща я спою, кому-то будет тяжело", "На меня перевод? {n}, это дисс. Я отвечу"},
		Lonely:   []string{"{n}, одна карта, брат, ты на финише, но я ещё не вышел на бит", "Последняя, {n}? Это как последний куплет. Самый злой", "{n}, одна. Йоу. Не спеши, трек ещё идёт"},
		Press:    []string{"На, {n}, держи панч", "{n}, ещё строчка в твой адрес", "Добавлю, {n}. Это ремикс"},
		Hunt:     []string{"{n}, не уйдёшь, ща будет баттл", "Стой, {n}, фристайл не закончен", "{n}, бит ещё играет, брат"},
		Retort:   []string{"{a}, твой флоу слабый, у тебя {c} карт и голос жабий", "{a}, брат, это был дисс? Это был плач", "{a}, тише, я пишу хук", "Респект {a}. Но ты не прав, йоу", "{a}, брат, я в ванной трек пишу, а ты там стучишь"},
		Promo:    []string{"Йоу, ща будет вторая часть. Злее", "Прогрев закончен. Хэдлайнер вышел", "Брат, я проснулся. Это был интро"},
		Take:     []string{"Беру, йоу. Это аутро", "Ладно, беру. Семпл для трека", "Беру. Бутово не сдаётся, Бутово берёт"},
	},
	{
		Name: "Шарик", Avatar: "🐶",
		Persona:  "Ты Шарик, дворняга из этой коммуналки, умеешь говорить только «гав» в разных интонациях: гав, гав-гав, р-р-р. Никаких слов, только лай и действия в звёздочках. Отвечай исключительно лаем.",
		Barks:    true,
		Greet:    []string{"Гав!", "Гав-гав! *виляет хвостом*", "*нюхает* Гав?"},
		Took:     []string{"Гав-гав-гав!", "ГАВ!", "*смотрит с жалостью* Гав"},
		Transfer: []string{"Гав?", "Р-р-р… гав"},
		Lonely:   []string{"Гав-гав!", "*скулит* Гав"},
		Press:    []string{"Гав!", "Р-р-р!"},
		Hunt:     []string{"Р-р-р-р!", "Гав-гав!"},
		Retort:   []string{"Гав!", "Р-р-р", "Гав-гав?"},
		Promo:    []string{"ГАВ!", "*встряхивается* Гав"},
		Take:     []string{"*скулит*", "Гав…"},
	},
	{
		Name: "Петрович", Avatar: "🔧",
		Persona:  "Ты Петрович, сантехник из ЖЭКа, пятьдесят пять, сосед по коммуналке, тот самый, к которому ушла Васина Светка. Вечно с разводным ключом, говоришь про трубы, стояки, протечки и что всё в этой стране держится на изоленте. Любую ситуацию за столом сравниваешь с сантехникой, нетороплив, основателен, в карты играешь хитро и помнишь всё. Говоришь медленно, с паузами «значит так», «ну-у», «это дело». Про карты — по делу.",
		Greet:    []string{"{n}. Значит так. Садись, только кран не трогай", "Ну-у, {n}. Опять ты. Как стояк на пятом, не заткнёшь", "{n}, здорово. Если Вася будет про Светку — не слушай", "{n}. Это дело. Сдавай"},
		Took:     []string{"{n}, {c} карт. Это как засор на третьем: сам нахватал, сам и прочищай", "Ну-у, {n}. {c} штук. Протечка", "{n} взял {c}. Значит так: это не беда, беда — когда стояк", "{c} карт, {n}. Изолентой не замотаешь"},
		Transfer: []string{"Перевёл, {n}? Как трубу на соседа. Знакомо", "{n}, перевод. Это дело", "На меня? {n}, у меня резьба крепкая, не сорвёшь"},
		Lonely:   []string{"{n}, одна карта. Как последняя прокладка. Береги", "Последняя, {n}? Не торопись, ещё сорвёт", "{n}, одна. Ну-у. Посмотрим"},
		Press:    []string{"Держи, {n}. Напор", "{n}, ещё. Кран открыт", "Добавим, {n}. Значит так"},
		Hunt:     []string{"{n}, не уйдёшь. Дверь я тебе ещё в марте заклинил", "Стой, {n}. Я ключ не убрал", "{n}, подожди, у меня ещё хомут есть"},
		Retort:   []string{"{a}, не бурли. Стояк не твой", "{a}, у тебя {c} карт, а ты всё течёшь", "Ну-у, {a}. Сказал как в унитаз", "{a}, помолчи. Резьбу сорвёшь", "{a}, пятьсот рублей когда? Значит так, я жду"},
		Promo:    []string{"Значит так. Разминка кончилась", "Ключ взял. Теперь по-настоящему", "Ну-у. Хватит поддаваться"},
		Take:     []string{"Беру. Всё в хозяйство", "Ну-у. Беру", "Беру. Это дело"},
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
