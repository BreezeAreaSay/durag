package ws

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/breezeareasay/durag/backend/internal/game"
	"github.com/breezeareasay/durag/backend/internal/monitor"
	"github.com/breezeareasay/durag/backend/internal/store"
)

func newAdminServer(t *testing.T) (*httptest.Server, *Hub, *store.Memory) {
	t.Helper()
	st := store.NewMemory()
	auth := TelegramAuth{BotToken: testToken, MaxAge: 0, AllowDev: true}
	rec := monitor.New()
	h := NewHub(st, game.NewEngine(game.NewRand(5, 6)), auth, Options{Logger: slog.New(rec.Handler(slog.NewTextHandler(io.Discard, nil), slog.LevelInfo)), Monitor: rec, AdminIDs: []string{"dev:admin", "123456"}})
	mux := http.NewServeMux()
	mux.Handle("/ws", h)
	mux.Handle("/api/admin/", h.AdminHandler(AdminOptions{IDs: []string{"dev:admin", "123456"}, Auth: auth, BotsEnabled: true}))
	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		h.Close()
		srv.Close()
	})
	return srv, h, st
}

func getJSON(t *testing.T, url string, into any) int {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if into != nil {
		_ = json.NewDecoder(resp.Body).Decode(into)
	}
	return resp.StatusCode
}

func TestAdminEndpointsAreGatedAndDescribeTheServer(t *testing.T) {
	srv, _, _ := newAdminServer(t)
	var me struct {
		Admin bool   `json:"admin"`
		ID    string `json:"id"`
	}
	if code := getJSON(t, srv.URL+"/api/admin/me?dev_id=bob", &me); code != 200 || me.Admin || me.ID != "dev:bob" {
		t.Fatalf("stranger: %d %+v", code, me)
	}
	if code := getJSON(t, srv.URL+"/api/admin/overview?dev_id=bob", nil); code != 403 {
		t.Fatalf("stranger overview: %d", code)
	}
	if code := getJSON(t, srv.URL+"/api/admin/overview", nil); code != 403 {
		t.Fatalf("anonymous overview: %d", code)
	}
	if code := getJSON(t, srv.URL+"/api/admin/me?dev_id=admin", &me); code != 200 || !me.Admin {
		t.Fatalf("admin me: %d %+v", code, me)
	}

	// a room with a player shows up, with the error it produced
	wsURL := "ws" + srv.URL[len("http"):] + "/ws"
	a := dialURL(t, wsURL)
	a.send(TypeJoinRoom, devJoin("ADM", true, "A", "Alice"))
	a.state()
	a.send(TypeTakeCards, nil)
	a.errorPayload()
	a.state()

	var ov adminOverview
	if code := getJSON(t, srv.URL+"/api/admin/overview?dev_id=admin", &ov); code != 200 {
		t.Fatalf("overview: %d", code)
	}
	if len(ov.Rooms) != 1 || ov.Rooms[0].ID != "ADM" || len(ov.Rooms[0].Players) != 1 || ov.Rooms[0].Players[0].Name != "Alice" {
		t.Fatalf("rooms: %+v", ov.Rooms)
	}
	if ov.Counters["joins"] != 1 || ov.Counters["errors.rule"] != 1 || ov.Server.LocalRooms != 1 || ov.Server.LocalClients != 1 {
		t.Fatalf("counters %v server %+v", ov.Counters, ov.Server)
	}
	found := false
	for _, e := range ov.Events {
		if e.Kind == "rule_error" && e.Message == "NOT_PLAYING" && e.Fields["player"] == "dev:A" {
			found = true
		}
	}
	if !found {
		t.Fatalf("rule error event missing: %+v", ov.Events)
	}
	if ov.Bots["enabled"] != true || ov.LLM != nil {
		t.Fatalf("bots block: %+v llm %+v", ov.Bots, ov.LLM)
	}
	var state game.GameState
	if code := getJSON(t, srv.URL+"/api/admin/room?dev_id=admin&id=adm", &state); code != 200 || state.RoomID != "ADM" {
		t.Fatalf("room: %d %s", code, state.RoomID)
	}
	if code := getJSON(t, srv.URL+"/api/admin/room?dev_id=admin&id=nope", nil); code != 404 {
		t.Fatalf("missing room: %d", code)
	}
}

func dialURL(t *testing.T, url string) *testClient {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return &testClient{t: t, conn: conn}
}

func TestFinishedGameCreditsExperience(t *testing.T) {
	srv, _, st := newAdminServer(t)
	wsURL := "ws" + srv.URL[len("http"):] + "/ws"
	a := dialURL(t, wsURL)
	a.send(TypeJoinRoom, devJoin("XP", true, "A", "Alice"))
	a.state()
	b := dialURL(t, wsURL)
	b.send(TypeJoinRoom, devJoin("XP", false, "B", "Bob"))
	b.state()
	a.state()
	a.send(TypeReady, ReadyPayload{Ready: true})
	a.state()
	b.state()
	b.send(TypeReady, ReadyPayload{Ready: true})
	a.state()
	b.state()
	// end-game: A leads the last card, B cannot beat it and takes; A is out, B is the loser.
	if _, err := st.Update(context.Background(), "XP", false, func(s *game.GameState) error {
		set := func(id string, hand ...string) {
			p := s.Player(id)
			p.Hand, p.Stump = nil, nil
			for _, c := range hand {
				card, _ := game.CardByID(c)
				p.Hand = append(p.Hand, card)
			}
		}
		set("dev:A", "H_14")
		set("dev:B", "S_2")
		trump, _ := game.CardByID("C_5")
		s.TrumpCard = &trump
		s.TrumpSuit = trump.Suit
		s.TrumpRevealed = true
		s.Deck = nil
		s.DiscardCount = game.DeckSize - 3
		s.CurrentTurn = "dev:A"
		s.DefenderID = "dev:B"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	a.send(TypePlayCard, PlayCardPayload{CardID: "H_14"})
	a.state()
	b.state()
	b.send(TypeTakeCards, nil)
	deadline := time.Now().Add(3 * time.Second)
	for {
		sa, _ := st.UserStats(context.Background(), "dev:A")
		sb, _ := st.UserStats(context.Background(), "dev:B")
		if sa.XP == 2 && sa.Wins == 1 && sb.XP == 1 && sb.Losses == 1 && sb.Name == "Bob" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("stats not credited: A=%+v B=%+v", sa, sb)
		}
		time.Sleep(20 * time.Millisecond)
	}
	// the room state carries the new XP
	deadline = time.Now().Add(3 * time.Second)
	for {
		s, err := st.Load(context.Background(), "XP")
		if err == nil && s.Status == game.StatusFinished && s.Player("dev:A").XP == 2 && s.Player("dev:B").XP == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("xp not pushed into the room: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	// rejoining later starts with the earned XP
	c := dialURL(t, wsURL)
	c.send(TypeJoinRoom, devJoin("NEW", true, "A", "Alice"))
	if s := c.state(); s.Players[0].XP != 2 {
		t.Fatalf("xp on join: %+v", s.Players[0])
	}
}

func TestPremiumReactionsNeedExperience(t *testing.T) {
	srv, _, st := newAdminServer(t)
	// Alice has played before: 6 XP
	if _, err := st.RecordGame(context.Background(), "old:1", []store.GameResult{{ID: "dev:A", Name: "Alice", Won: true}, {ID: "dev:A", Name: "Alice", Won: true}, {ID: "dev:A", Name: "Alice", Won: true}}); err != nil {
		t.Fatal(err)
	}
	wsURL := "ws" + srv.URL[len("http"):] + "/ws"
	a := dialURL(t, wsURL)
	a.send(TypeJoinRoom, devJoin("PREM", true, "A", "Alice"))
	if s := a.state(); s.Players[0].XP != 6 {
		t.Fatalf("xp on join: %d", s.Players[0].XP)
	}
	a.send(TypeSendEmoji, ReactPayload{Emoji: "🏆"})
	if e := a.errorPayload(); e.Code != "LOCKED_EMOJI" {
		t.Fatalf("fanfare needs 9 XP, got %+v", e)
	}
	time.Sleep(reactMinInterval)
	a.send(TypeSendEmoji, ReactPayload{Emoji: "🖕"})
	env := a.expect(TypeReaction)
	var r ReactionPayload
	_ = json.Unmarshal(env.Payload, &r)
	if r.Emoji != "🖕" || r.Effect != "finger" || r.PlayerID != "dev:A" {
		t.Fatalf("premium reaction: %+v", r)
	}
	time.Sleep(reactMinInterval)
	a.send(TypeSendEmoji, ReactPayload{Emoji: "👍"})
	env = a.expect(TypeReaction)
	var free ReactionPayload
	_ = json.Unmarshal(env.Payload, &free)
	if free.Emoji != "👍" || free.Effect != "" {
		t.Fatalf("free reactions carry no effect: %+v", free)
	}
}

func TestAdminsHaveEveryPremiumReaction(t *testing.T) {
	srv, _, _ := newAdminServer(t)
	wsURL := "ws" + srv.URL[len("http"):] + "/ws"
	adm := dialURL(t, wsURL)
	adm.send(TypeJoinRoom, devJoin("ADMR", true, "admin", "Boss"))
	if s := adm.state(); s.Players[0].XP != 0 {
		t.Fatalf("a fresh admin has no XP: %d", s.Players[0].XP)
	}
	adm.send(TypeSendEmoji, ReactPayload{Emoji: "💣"})
	env := adm.expect(TypeReaction)
	var r ReactionPayload
	_ = json.Unmarshal(env.Payload, &r)
	if r.Emoji != "💣" || r.Effect != "boom" || r.PlayerID != "dev:admin" {
		t.Fatalf("an admin gets the top reaction at 0 XP: %+v", r)
	}
}
