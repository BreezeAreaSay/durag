package bot

import (
	"fmt"
	"math/rand/v2"
	"strings"

	"github.com/breezeareasay/durag/backend/internal/game"
)

// Chronicle is what happened at the table this game, in words: the model
// reads it to refer back ("that was your third take"), the rules use the
// counts for callbacks. Kept by the room, fed every published state.
type Chronicle struct {
	seq     int64          // the last log entry filed
	Takes   map[string]int // player id -> times they took this game
	Biggest map[string]int // player id -> the biggest take this game
	Notes   []string       // the story so far, newest last, capped
	Lines   []string       // the last lines said at the table ("Вася: …"), capped
	Topic   string         // the evening's topic, picked at the start
	bout    []string       // cards of the bout in play (to size a take)
	started bool
}

// Topics of the evening: one is drawn when a game starts and the bots keep coming back to it.
var Topics = []string{
	"кто не выключил свет на кухне (Бабуля уже записала)",
	"Вася снова просит у Петровича пятьсот рублей до пятницы",
	"стояк на пятом этаже течёт третий день",
	"Бутово записал новый трек в ванной, вся квартира слышала",
	"Шарик опять оставил лужу в коридоре, виноватых ищут",
	"очередь в туалет с утра была сорок минут",
	"Светка звонила Петровичу, Вася делает вид, что ему всё равно",
	"Гоша завтра на олимпиаду, бабушка не даёт ему играть, а он играет",
	"Вика второй день не разговаривает с отцом",
	"Крест сказал, что за угол с Бутово ждёт до понедельника",
	"пирожки Бабули стоят на кухне с понедельника, никто не ест",
	"муха над столом — Вася обещал прихлопнуть и не прихлопнул",
}

// NewChronicle starts an empty story.
func NewChronicle() *Chronicle {
	c := &Chronicle{}
	c.Reset(nil)
	return c
}

// Reset starts a new game: new counts, a new topic.
func (c *Chronicle) Reset(rng *rand.Rand) {
	c.seq = 0
	c.Takes = map[string]int{}
	c.Biggest = map[string]int{}
	c.Notes = nil
	c.Lines = nil
	c.bout = nil
	c.started = false
	if rng == nil {
		rng = rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	}
	c.Topic = Topics[rng.IntN(len(Topics))]
}

func (c *Chronicle) note(s string) {
	c.Notes = append(c.Notes, s)
	if len(c.Notes) > 10 {
		c.Notes = c.Notes[len(c.Notes)-10:]
	}
}

// Said files a line somebody said at the table.
func (c *Chronicle) Said(name, text string) {
	if text == "" {
		return
	}
	c.Lines = append(c.Lines, name+": "+text)
	if len(c.Lines) > 4 {
		c.Lines = c.Lines[len(c.Lines)-4:]
	}
}

// Observe reads the new log entries of a state into the story.
func (c *Chronicle) Observe(s *game.GameState) {
	name := func(id string) string {
		if p := s.Player(id); p != nil {
			return p.Name
		}
		return id
	}
	for _, e := range s.Log {
		if e.Seq <= c.seq {
			continue
		}
		c.seq = e.Seq
		switch e.Type {
		case "start":
			c.started = true
			c.bout = nil
		case "attack", "throw_in", "transfer", "timeout":
			if e.CardID != "" {
				c.bout = append(c.bout, e.CardID)
			}
		case "defend":
			c.bout = append(c.bout, e.CardID)
			if cv, ok := game.CardByID(e.CardID); ok && !cv.IsSpecial() && s.ActiveTrump() != "" && cv.Suit == s.ActiveTrump() {
				if a, ok := game.CardByID(e.TargetID); ok && !a.IsSpecial() && a.Suit != cv.Suit && a.Rank <= 8 {
					c.note(fmt.Sprintf("%s побил %s козырем %s (кон %d): козырь на мелочь", name(e.PlayerID), cardText(a), cardText(cv), s.BoutNumber))
				}
			}
		case "took":
			var n int
			fmt.Sscan(e.Text, &n)
			c.Takes[e.PlayerID]++
			if n > c.Biggest[e.PlayerID] {
				c.Biggest[e.PlayerID] = n
			}
			switch {
			case n >= 5:
				c.note(fmt.Sprintf("%s взял сразу %d карт (кон %d)", name(e.PlayerID), n, s.BoutNumber))
			case c.Takes[e.PlayerID] == 3:
				c.note(fmt.Sprintf("%s берёт уже третий раз за игру", name(e.PlayerID)))
			case c.Takes[e.PlayerID] > 3:
				c.note(fmt.Sprintf("%s берёт %d-й раз", name(e.PlayerID), c.Takes[e.PlayerID]))
			}
			c.bout = nil
		case "bito":
			var n int
			fmt.Sscan(e.Text, &n)
			if n >= 6 {
				c.note(fmt.Sprintf("%s отбился от %d карт (кон %d): большое бито", name(e.PlayerID), n, s.BoutNumber))
			}
			c.bout = nil
		case "trump_revealed":
			c.note(fmt.Sprintf("козырь вскрылся: %s, достался %s", suitText(s.TrumpSuit), name(e.PlayerID)))
		case "out":
			c.note(fmt.Sprintf("%s вышел из игры (кон %d)", name(e.PlayerID), s.BoutNumber))
		case "stump":
			c.note(fmt.Sprintf("%s поднял пенёк", name(e.PlayerID)))
		}
	}
	if s.DeckCount == 0 && c.started && !strings.Contains(strings.Join(c.Notes, "|"), "колода кончилась") && s.TrumpRevealed {
		c.note(fmt.Sprintf("колода кончилась на %d-м кону", s.BoutNumber))
	}
}

// Snapshot copies the story for a bot thinking outside the lock.
func (c *Chronicle) Snapshot() Chronicle {
	out := Chronicle{Topic: c.Topic, Takes: map[string]int{}, Biggest: map[string]int{}}
	for k, v := range c.Takes {
		out.Takes[k] = v
	}
	for k, v := range c.Biggest {
		out.Biggest[k] = v
	}
	out.Notes = append([]string{}, c.Notes...)
	out.Lines = append([]string{}, c.Lines...)
	return out
}

// Callbacks: the rules refer back to the game's story. {n} the person, {k} a count.
var Callbacks = map[string][]string{
	"third_take": {
		"{n}, третий раз берёшь. Я считаю, если что",
		"{n}, это уже третий заход в карман. Тенденция",
		"Третий раз, {n}. Система, а не невезение",
	},
	"many_takes": {
		"{n}, {k}-й раз. Ты вообще отбиваться собираешься?",
		"{k}-й раз, {n}. Я бы на твоём месте просто сдался",
		"{n}, {k}. Запиши где-нибудь, потом внукам расскажешь",
	},
	"big_take_recall": {
		"{n}, помнишь, как ты {k} карт взял? Я помню",
		"{n}, у тебя где-то там ещё те {k} карт лежат. Не торопись",
	},
}

// Callback picks a line about the game's story for a take by player id, or "".
func Callback(rng *rand.Rand, ch Chronicle, playerID, name string) string {
	k := ch.Takes[playerID]
	var pool []string
	switch {
	case k == 3:
		pool = Callbacks["third_take"]
	case k > 3:
		pool = Callbacks["many_takes"]
	case ch.Biggest[playerID] >= 6 && rng.Float64() < 0.5:
		pool = Callbacks["big_take_recall"]
		k = ch.Biggest[playerID]
	}
	if len(pool) == 0 {
		return ""
	}
	line := pool[rng.IntN(len(pool))]
	line = strings.ReplaceAll(line, "{n}", name)
	return strings.ReplaceAll(line, "{k}", fmt.Sprint(k))
}

// Storyline is a little exchange between two characters that unfolds over
// a game, one step every few bouts, when both sit at the table.
type Storyline struct {
	Needs []string
	Steps []StoryStep
}

// StoryStep is one line of a storyline, said by one character.
type StoryStep struct {
	Who  string
	Line string
}

// Storylines of the flat, by the names of the characters they need.
var Storylines = []Storyline{
	{Needs: []string{"Вася", "Петрович"}, Steps: []StoryStep{
		{"Вася", "Петрович, слушай. Ик. Пятьсот до пятницы, а?"},
		{"Петрович", "Значит так, Вася. Пятница была. Три раза"},
		{"Вася", "Так то те пятницы. Я про эту"},
		{"Петрович", "Ну-у. Отыграешь у меня стол — поговорим"},
		{"Вася", "Я и отыграю. Только налейте кто-нибудь"},
	}},
	{Needs: []string{"Бабуля", "Гоша"}, Steps: []StoryStep{
		{"Бабуля", "Гоша, уроки сделал? Чего молчишь"},
		{"Гоша", "Бабушка, я считаю вероятности. Это и есть уроки"},
		{"Бабуля", "Вероятности. Мать узнает — обоим будет вероятность"},
		{"Гоша", "Статистически она не узнает. Если ты не напишешь"},
		{"Бабуля", "Я ещё подумаю. Ходи, умник"},
	}},
	{Needs: []string{"Крест", "Бутово"}, Steps: []StoryStep{
		{"Крест", "Бутово. За угол когда?"},
		{"Бутово", "Йоу, Крест, ща трек выстрелит, и всё разом"},
		{"Крест", "Трек. Я слышал этот трек. Из ванной. Два месяца"},
		{"Бутово", "Брат, это демка была. Релиз будет бэнгер"},
		{"Крест", "До понедельника, фраер. Это дело"},
	}},
	{Needs: []string{"Вика", "Петрович"}, Steps: []StoryStep{
		{"Петрович", "Вика. Ну-у. Как дела"},
		{"Вика", "…"},
		{"Петрович", "Значит так. Молчишь. Это дело"},
		{"Вика", "Пап, ходи. Все умрём, но ты раньше"},
		{"Петрович", "Хоть «пап» сказала. Уже что-то"},
	}},
	{Needs: []string{"Бабуля", "Вася"}, Steps: []StoryStep{
		{"Бабуля", "Вася, свет на кухне кто не выключил?"},
		{"Вася", "Это не я. Ик. Это Шарик"},
		{"Бабуля", "Шарик до выключателя не достаёт. А ты и до стола еле"},
		{"Вася", "Бабуль, я тебе за свет с получки. Честное слово"},
		{"Бабуля", "С получки. Я в тетрадку записала, честное слово твоё"},
	}},
	{Needs: []string{"Шарик", "Бабуля"}, Steps: []StoryStep{
		{"Шарик", "Гав?"},
		{"Бабуля", "Шарик, после игры. Пирожок остался, никто не ест"},
		{"Шарик", "Гав-гав! *виляет хвостом*"},
		{"Бабуля", "Один ты меня тут любишь"},
	}},
	{Needs: []string{"Гоша", "Бутово"}, Steps: []StoryStep{
		{"Гоша", "Бутово, ваш трек в ванной ночью — это 94 децибела. Я замерил"},
		{"Бутово", "Йоу, малой, это флоу. Децибелы — для ботаников"},
		{"Гоша", "Я и есть ботаник. Спать невозможно"},
		{"Бутово", "Брат, зато ты теперь знаешь слова. Подпевай"},
	}},
	{Needs: []string{"Вика", "Вася"}, Steps: []StoryStep{
		{"Вася", "Вика, передай отцу… ик… что я зла не держу"},
		{"Вика", "Я с ним не разговариваю. И с тобой не хочу"},
		{"Вася", "Все вы такие. Как Светка"},
		{"Вика", "Светка от тебя к моему отцу ушла. Задумайся"},
	}},
}

// PickStoryline draws a storyline whose characters are all at the table, or nil.
func PickStoryline(rng *rand.Rand, names []string) *Storyline {
	present := map[string]bool{}
	for _, n := range names {
		present[n] = true
	}
	var fit []int
	for i, st := range Storylines {
		ok := true
		for _, n := range st.Needs {
			ok = ok && present[n]
		}
		if ok {
			fit = append(fit, i)
		}
	}
	if len(fit) == 0 {
		return nil
	}
	st := Storylines[fit[rng.IntN(len(fit))]]
	return &st
}
