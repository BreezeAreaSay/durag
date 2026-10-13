package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/breezeareasay/durag/backend/internal/game"
)

// LLM asks a chat model (DeepSeek, Qwen or anything speaking the
// OpenAI-compatible /chat/completions API) for a move, in JSON. Every answer
// is validated against the legal moves; the caller falls back to the
// Heuristic on any error.
type LLM struct {
	BaseURL string // e.g. https://api.deepseek.com/v1 or https://dashscope-intl.aliyuncs.com/compatible-mode/v1
	APIKey  string
	Model   string // e.g. deepseek-chat, qwen-plus
	Timeout time.Duration
	Client  *http.Client
}

// ErrBadAnswer is returned when the model's JSON does not name a legal move.
var ErrBadAnswer = errors.New("llm: answer is not a legal move")

type chatRequest struct {
	Model          string        `json:"model"`
	Messages       []chatMessage `json:"messages"`
	Temperature    float64       `json:"temperature"`
	MaxTokens      int           `json:"max_tokens"`
	ResponseFormat *respFormat   `json:"response_format,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type respFormat struct {
	Type string `json:"type"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// answer is what the model is asked to produce.
type answer struct {
	Move  int    `json:"move"`
	Say   string `json:"say"`
	Emoji string `json:"emoji"`
}

const systemPrompt = `Ты играешь в «Дурака» (вариант DURAG) за одного из игроков. Правила кратко: атакующий кладёт карту, защищающийся бьёт её старшей картой той же масти или козырем (козырь известен только после вскрытия), может перевести картой того же достоинства или взять весь стол. Подкидывать можно карты тех достоинств, что уже на столе, но не больше, чем карт в руке у защищающегося. Джокер бьёт любую карту своего цвета, Супер-карта бьёт всё. Кто остался с картами последним — проиграл.
Тебе дают твою руку, стол и ПРОНУМЕРОВАННЫЙ список легальных ходов. Выбери лучший ход и ответь СТРОГО одним JSON-объектом вида {"move": <номер хода>, "say": "<короткая реплика до 60 символов или пустая строка>", "emoji": "<один эмодзи из списка 👍 👎 😂 😡 😎 🤔 🔥 💀 🤡 ❤️ 👏 🙏 или пустая строка>"}. Реплика — по-русски, в характере дворового игрока, без оскорблений, не чаще чем в каждом третьем ходе. Никакого текста вне JSON.`

// Decide implements Brain.
func (l *LLM) Decide(ctx context.Context, in Context) (Decision, error) {
	if len(in.Moves) == 0 {
		return Decision{}, ErrBadAnswer
	}
	timeout := l.Timeout
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	body, err := json.Marshal(chatRequest{
		Model:          l.Model,
		Messages:       []chatMessage{{Role: "system", Content: systemPrompt}, {Role: "user", Content: Prompt(in)}},
		Temperature:    0.7,
		MaxTokens:      160,
		ResponseFormat: &respFormat{Type: "json_object"},
	})
	if err != nil {
		return Decision{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(l.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Decision{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+l.APIKey)
	client := l.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return Decision{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Decision{}, err
	}
	if resp.StatusCode/100 != 2 {
		return Decision{}, fmt.Errorf("llm: http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var cr chatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return Decision{}, fmt.Errorf("llm: decode response: %w", err)
	}
	if cr.Error != nil {
		return Decision{}, errors.New("llm: " + cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return Decision{}, errors.New("llm: no choices")
	}
	return Parse(cr.Choices[0].Message.Content, in)
}

// Prompt renders the bot's view of the table for the model.
func Prompt(in Context) string {
	s := in.State
	var b strings.Builder
	me := s.Player(in.BotID)
	fmt.Fprintf(&b, "Ты — %s. ", nameOf(s, in.BotID))
	switch in.Mood {
	case MoodAggressive:
		b.WriteString("Тебя провоцируют эмодзи: играй агрессивно, дави, переводи, подкидывай больше.\n")
	case MoodDefensive:
		b.WriteString("Над тобой насмехаются: играй осторожно, береги козыри, не рискуй.\n")
	default:
		b.WriteString("Настроение ровное: играй разумно.\n")
	}
	if me != nil {
		b.WriteString("Твоя рука: ")
		for i, c := range me.Hand {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(cardText(c))
		}
		b.WriteString("\n")
	}
	if s.TrumpRevealed && s.TrumpSuit != "" {
		fmt.Fprintf(&b, "Козырь: %s. ", suitText(s.TrumpSuit))
	} else {
		b.WriteString("Козырь ещё не вскрыт. ")
	}
	fmt.Fprintf(&b, "В колоде %d карт, в отбое %d, кон №%d.\n", s.DeckCount, s.DiscardCount, s.BoutNumber)
	b.WriteString("Игроки: ")
	for i, p := range s.Players {
		if i > 0 {
			b.WriteString("; ")
		}
		role := ""
		switch {
		case p.Out:
			role = ", вышел"
		case p.ID == s.DefenderID:
			role = ", защищается"
			if s.DefenderTaking {
				role = ", берёт"
			}
		case p.ID == s.CurrentTurn:
			role = ", ходит"
		}
		fmt.Fprintf(&b, "%s (%d карт%s)", p.Name, p.HandCount, role)
	}
	b.WriteString("\nСтол: ")
	if len(s.TableOrder) == 0 {
		b.WriteString("пусто")
	}
	for i, id := range s.TableOrder {
		if i > 0 {
			b.WriteString("; ")
		}
		if c, ok := game.CardByID(id); ok {
			b.WriteString(cardText(c))
		}
		if d := s.TableCards[id]; len(d) > 0 {
			b.WriteString(" побита " + cardText(d[0]))
		} else {
			b.WriteString(" (не побита)")
		}
	}
	if len(in.Recent) > 0 {
		b.WriteString("\nНедавние эмодзи: ")
		for i, e := range in.Recent {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "%s %s", e.FromName, e.Emoji)
		}
	}
	if in.AdminJokeDue {
		b.WriteString("\nОдин раз за игру ты можешь сломать четвёртую стену — пошутить, что ты бот и привет админу. Можешь сделать это сейчас в поле say.")
	}
	b.WriteString("\nЛегальные ходы:\n")
	for i, m := range in.Moves {
		fmt.Fprintf(&b, "%d. %s\n", i+1, moveText(m))
	}
	b.WriteString("Ответь JSON.")
	return b.String()
}

// Parse validates the model's JSON against the legal moves.
func Parse(content string, in Context) (Decision, error) {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	if i := strings.Index(content, "{"); i > 0 {
		content = content[i:]
	}
	if i := strings.LastIndex(content, "}"); i >= 0 && i < len(content)-1 {
		content = content[:i+1]
	}
	var a answer
	if err := json.Unmarshal([]byte(content), &a); err != nil {
		return Decision{}, fmt.Errorf("llm: bad json: %w", err)
	}
	if a.Move < 1 || a.Move > len(in.Moves) {
		return Decision{}, ErrBadAnswer
	}
	d := Decision{Move: in.Moves[a.Move-1], Say: trimSay(a.Say), Emoji: a.Emoji}
	if d.Say != "" && in.AdminJokeDue && mentionsAdmin(d.Say) {
		d.AdminJoke = true
	}
	return d, nil
}

func trimSay(s string) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > 80 {
		return string(r[:80]) + "…"
	}
	return s
}

func mentionsAdmin(s string) bool {
	l := strings.ToLower(s)
	return strings.Contains(l, "админ") || strings.Contains(l, "admin") || strings.Contains(l, "бот") || strings.Contains(l, "скрипт")
}

func nameOf(s *game.GameState, id string) string {
	if p := s.Player(id); p != nil {
		return p.Name
	}
	return id
}

var suitNames = map[string]string{game.SuitHearts: "червы", game.SuitDiamonds: "бубны", game.SuitClubs: "трефы", game.SuitSpades: "пики"}

func suitText(suit string) string {
	if n, ok := suitNames[suit]; ok {
		return n
	}
	return suit
}

func cardText(c game.Card) string {
	switch {
	case c.IsSuper():
		return "СУПЕР-КАРТА"
	case c.ID == "RJ":
		return "КРАСНЫЙ ДЖОКЕР"
	case c.ID == "BJ":
		return "ЧЁРНЫЙ ДЖОКЕР"
	}
	rank := map[int]string{11: "В", 12: "Д", 13: "К", 14: "Т"}[c.Rank]
	if rank == "" {
		rank = fmt.Sprint(c.Rank)
	}
	return rank + " " + suitText(c.Suit)
}

func moveText(m game.Move) string {
	card := func(id string) string {
		if c, ok := game.CardByID(id); ok {
			return cardText(c)
		}
		return id
	}
	switch m.Type {
	case game.MoveAttack:
		return "зайти " + card(m.CardID)
	case game.MoveThrowIn:
		return "подкинуть " + card(m.CardID)
	case game.MoveDefend:
		return "побить " + card(m.TargetID) + " картой " + card(m.CardID)
	case game.MoveTransfer:
		return "перевести картой " + card(m.CardID)
	case game.MoveTake:
		return "взять весь стол"
	case game.MovePass:
		return "бито (больше не подкидывать)"
	case game.MoveTakeStump:
		return "взять пенёк"
	}
	return string(m.Type)
}
