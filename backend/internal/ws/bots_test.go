package ws

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/breezeareasay/durag/backend/internal/bot"
	"github.com/breezeareasay/durag/backend/internal/game"
	"github.com/breezeareasay/durag/backend/internal/store"
)

func newBotServer(t *testing.T, opts BotOptions) (*httptest.Server, *Hub, *store.Memory) {
	t.Helper()
	st := store.NewMemory()
	auth := TelegramAuth{BotToken: testToken, MaxAge: 0, AllowDev: true}
	h := NewHub(st, game.NewEngine(game.NewRand(21, 22)), auth, Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Bots: opts})
	srv := httptest.NewServer(h)
	t.Cleanup(func() {
		h.Close()
		srv.Close()
	})
	return srv, h, st
}

// intentFor turns a legal move of the human into the wire message a client would send.
func intentFor(m game.Move) (string, any) {
	switch m.Type {
	case game.MoveAttack, game.MoveThrowIn:
		return TypePlayCard, PlayCardPayload{CardID: m.CardID}
	case game.MoveDefend:
		return TypePlayCard, PlayCardPayload{CardID: m.CardID, TargetCardID: m.TargetID}
	case game.MoveTransfer:
		return TypeTransferTurn, TransferPayload{CardID: m.CardID}
	case game.MoveTake:
		return TypeTakeCards, nil
	case game.MoveTakeStump:
		return TypeTakeStump, nil
	}
	return TypeResolveBout, nil
}

func TestBotPlaysAgainstAHuman(t *testing.T) {
	srv, _, _ := newBotServer(t, BotOptions{}) // zero delays: bots act at once
	a := dial(t, srv)
	a.send(TypeJoinRoom, devJoin("BOT", true, "A", "Alice"))
	a.state()
	a.send(TypeAddBot, nil)
	s := a.state()
	if len(s.Players) != 2 || !s.Players[1].IsBot || !s.Players[1].IsReady || !s.Players[1].Connected || s.Players[1].Name != bot.Names[0] {
		t.Fatalf("bot seat: %+v", s.Players)
	}
	botID := s.Players[1].ID
	a.send(TypeReady, ReadyPayload{Ready: true})

	// From here the bot moves on its own; the human replies with its first
	// sensible legal move until a few bouts went by.
	botMoved := false
	deadline := time.Now().Add(10 * time.Second)
	var last *game.GameState
	for time.Now().Before(deadline) {
		env := a.next()
		if env.Type == TypeReaction {
			continue // the bot said something
		}
		if env.Type == TypeError {
			continue // the bot moved first and our move went stale; the fresh state follows
		}
		if env.Type != TypeStateUpdate {
			t.Fatalf("unexpected %s %s", env.Type, env.Payload)
		}
		var st game.GameState
		if err := json.Unmarshal(env.Payload, &st); err != nil {
			t.Fatal(err)
		}
		last = &st
		for _, e := range st.Log {
			if e.PlayerID == botID {
				switch e.Type {
				case "attack", "throw_in", "defend", "transfer", "take", "pass", "stump":
					botMoved = true
				}
			}
		}
		if st.Status == game.StatusFinished || (st.BoutNumber >= 4 && botMoved) {
			break
		}
		moves := st.LegalMoves("dev:A")
		if len(moves) == 0 {
			continue
		}
		pick := moves[0]
		for _, m := range moves {
			if m.Type != game.MovePass && m.Type != game.MoveTake {
				pick = m
				break
			}
		}
		msgType, payload := intentFor(pick)
		a.send(msgType, payload)
	}
	if last == nil || !botMoved {
		t.Fatalf("the bot never moved; last state: %+v", last)
	}
	if last.Status == game.StatusPlaying && last.BoutNumber < 4 {
		t.Fatalf("game stalled at bout %d", last.BoutNumber)
	}
}

func TestBotsLobbyAndRoomCleanup(t *testing.T) {
	srv, h, st := newBotServer(t, BotOptions{})
	a := dial(t, srv)
	a.send(TypeJoinRoom, devJoin("CLEAN", true, "A", "Alice"))
	a.state()
	a.send(TypeAddBot, nil)
	a.state()
	a.send(TypeAddBot, nil)
	s := a.state()
	if len(s.Players) != 3 || s.Players[1].Name == s.Players[2].Name {
		t.Fatalf("two bots with distinct names expected: %+v", s.Players)
	}
	a.send(TypeRemoveBot, RemoveBotPayload{PlayerID: s.Players[2].ID})
	if s := a.state(); len(s.Players) != 2 {
		t.Fatalf("bot not removed: %+v", s.Players)
	}
	a.send(TypeRemoveBot, RemoveBotPayload{PlayerID: "dev:A"})
	if e := a.errorPayload(); e.Code != "NOT_A_BOT" {
		t.Fatalf("removing a human: %+v", e)
	}
	a.state()
	// the bots' moods follow the room's emoji
	a.send(TypeSendEmoji, ReactPayload{Emoji: "😡"})
	a.expect(TypeReaction)
	botID := s.Players[1].ID
	if mood, _, _ := h.botRoomFor("CLEAN").view(botID, time.Now()); mood != bot.MoodAggressive {
		t.Fatalf("mood after a provocation: %s", mood)
	}
	// a bot's line travels as a reaction with text
	h.botSay("CLEAN", botID, "Админ, привет", "")
	env := a.next()
	for env.Type == TypeReaction {
		var r ReactionPayload
		_ = json.Unmarshal(env.Payload, &r)
		if r.PlayerID == botID && r.Text == "Админ, привет" {
			break
		}
		env = a.next()
	}
	if env.Type != TypeReaction {
		t.Fatalf("bot line not delivered: %s %s", env.Type, env.Payload)
	}
	// the last human leaves: the table (bots included) is gone
	a.send(TypeLeaveRoom, nil)
	time.Sleep(50 * time.Millisecond)
	if _, err := st.Load(t.Context(), "CLEAN"); err == nil {
		t.Fatal("a room with only bots must be deleted")
	}
	b := dial(t, srv)
	b.send(TypeJoinRoom, devJoin("CLEAN", false, "B", "Bob"))
	if e := b.errorPayload(); e.Code != "ROOM_NOT_FOUND" {
		t.Fatalf("got %+v", e)
	}
}

func TestBotsCanBeDisabled(t *testing.T) {
	srv, _, _ := newBotServer(t, BotOptions{Disabled: true})
	a := dial(t, srv)
	a.send(TypeJoinRoom, devJoin("OFF", true, "A", "Alice"))
	a.state()
	a.send(TypeAddBot, nil)
	if e := a.errorPayload(); e.Code != "BOTS_DISABLED" {
		t.Fatalf("got %+v", e)
	}
}

// chattyBrain is a model stand-in: it must never be asked for a move when
// LLMMoves is off, and it answers every request for a line.
type chattyBrain struct {
	mu      sync.Mutex
	decides int
	talks   int
}

func (b *chattyBrain) Decide(context.Context, bot.Context) (bot.Decision, error) {
	b.mu.Lock()
	b.decides++
	b.mu.Unlock()
	return bot.Decision{}, errors.New("not asked for moves")
}

func (b *chattyBrain) Talk(_ context.Context, in bot.Context, move game.Move) (bot.Line, error) {
	b.mu.Lock()
	b.talks++
	b.mu.Unlock()
	return bot.Line{Say: "Сказал модельный бот: " + move.String(), Model: "fake/model"}, nil
}

func TestModelTalksAfterTheRulesMove(t *testing.T) {
	brain := &chattyBrain{}
	srv, _, _ := newBotServer(t, BotOptions{Brain: brain})
	a := dial(t, srv)
	a.send(TypeJoinRoom, devJoin("TALK", true, "A", "Alice"))
	a.state()
	a.send(TypeAddBot, nil)
	s := a.state()
	botID := s.Players[1].ID
	a.send(TypeReady, ReadyPayload{Ready: true})
	deadline := time.Now().Add(10 * time.Second)
	heard := ""
	botMoves := 0
	for time.Now().Before(deadline) && heard == "" {
		env := a.next()
		switch env.Type {
		case TypeReaction:
			var r ReactionPayload
			_ = json.Unmarshal(env.Payload, &r)
			if r.PlayerID == botID && strings.HasPrefix(r.Text, "Сказал модельный бот") {
				heard = r.Text
			}
		case TypeStateUpdate:
			var st game.GameState
			_ = json.Unmarshal(env.Payload, &st)
			for _, e := range st.Log {
				if e.PlayerID == botID {
					botMoves++
				}
			}
			if st.Status != game.StatusPlaying {
				break
			}
			moves := st.LegalMoves("dev:A")
			if len(moves) == 0 {
				continue
			}
			pick := moves[0]
			for _, m := range moves {
				if m.Type != game.MovePass && m.Type != game.MoveTake {
					pick = m
					break
				}
			}
			msgType, payload := intentFor(pick)
			a.send(msgType, payload)
		}
	}
	if heard == "" {
		t.Fatalf("no model line arrived after %d bot log entries", botMoves)
	}
	brain.mu.Lock()
	defer brain.mu.Unlock()
	if brain.decides != 0 || brain.talks == 0 {
		t.Fatalf("with LLM_MOVES off the model must only talk: decides=%d talks=%d", brain.decides, brain.talks)
	}
}
