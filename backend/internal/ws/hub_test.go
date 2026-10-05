package ws

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/breezeareasay/durag/backend/internal/game"
	"github.com/breezeareasay/durag/backend/internal/store"
	"github.com/breezeareasay/durag/backend/internal/telegram"
)

const testToken = "7000000000:AAFakeTokenForUnitTests_1234567890abc"

func newTestServer(t *testing.T, allowDev bool) (*httptest.Server, *Hub) {
	t.Helper()
	srv, h, _ := newTestServerWithEngine(t, allowDev, game.NewEngine(game.NewRand(3, 4)))
	return srv, h
}

func newTestServerWithEngine(t *testing.T, allowDev bool, eng *game.Engine) (*httptest.Server, *Hub, *store.Memory) {
	t.Helper()
	st := store.NewMemory()
	auth := TelegramAuth{BotToken: testToken, MaxAge: 0, AllowDev: allowDev}
	h := NewHub(st, eng, auth, Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	srv := httptest.NewServer(h)
	t.Cleanup(func() {
		h.Close()
		srv.Close()
	})
	return srv, h, st
}

type testClient struct {
	t    *testing.T
	conn *websocket.Conn
}

func dial(t *testing.T, srv *httptest.Server) *testClient {
	t.Helper()
	u := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return &testClient{t: t, conn: conn}
}

func (c *testClient) send(msgType string, payload any) {
	c.t.Helper()
	if err := c.conn.WriteMessage(websocket.TextMessage, encode(msgType, payload)); err != nil {
		c.t.Fatalf("send %s: %v", msgType, err)
	}
}

func (c *testClient) next() Envelope {
	c.t.Helper()
	_ = c.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, data, err := c.conn.ReadMessage()
	if err != nil {
		c.t.Fatalf("read: %v", err)
	}
	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		c.t.Fatalf("decode: %v", err)
	}
	return env
}

func (c *testClient) expect(msgType string) Envelope {
	c.t.Helper()
	env := c.next()
	if env.Type != msgType {
		c.t.Fatalf("got %s %s, want %s", env.Type, string(env.Payload), msgType)
	}
	return env
}

func (c *testClient) state() *game.GameState {
	c.t.Helper()
	env := c.expect(TypeStateUpdate)
	var s game.GameState
	if err := json.Unmarshal(env.Payload, &s); err != nil {
		c.t.Fatalf("decode state: %v", err)
	}
	return &s
}

func (c *testClient) errorPayload() ErrorPayload {
	c.t.Helper()
	env := c.expect(TypeError)
	var p ErrorPayload
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		c.t.Fatalf("decode error: %v", err)
	}
	return p
}

func devJoin(room string, create bool, id, name string) JoinPayload {
	return JoinPayload{RoomID: room, Create: create, DevUser: &DevUser{ID: id, Name: name}}
}

func TestLobbyGameAndSanitizedViews(t *testing.T) {
	srv, hub := newTestServer(t, true)
	a := dial(t, srv)
	a.send(TypeJoinRoom, devJoin("abc1", true, "A", "Alice"))
	s := a.state()
	if s.RoomID != "ABC1" || s.ViewerID != "dev:A" || len(s.Players) != 1 || s.Status != game.StatusWaiting || s.HostID != "dev:A" {
		t.Fatalf("state after create: %+v", s)
	}

	b := dial(t, srv)
	b.send(TypeJoinRoom, devJoin("ABC1", false, "B", "Bob"))
	if s := b.state(); len(s.Players) != 2 || s.ViewerID != "dev:B" {
		t.Fatalf("B view: %+v", s)
	}
	if s := a.state(); len(s.Players) != 2 {
		t.Fatalf("A should see Bob: %+v", s)
	}
	if hub.LocalRooms() != 1 {
		t.Fatalf("local rooms %d", hub.LocalRooms())
	}

	z := dial(t, srv)
	z.send(TypeJoinRoom, devJoin("nope", false, "Z", "Zed"))
	if e := z.errorPayload(); e.Code != "ROOM_NOT_FOUND" {
		t.Fatalf("join missing room: %+v", e)
	}
	z.send(TypeTakeCards, nil)
	if e := z.errorPayload(); e.Code != "NOT_IN_ROOM" {
		t.Fatalf("intent without room: %+v", e)
	}

	a.send(TypeReady, ReadyPayload{Ready: true})
	if s := a.state(); !s.Players[0].IsReady || s.Status != game.StatusWaiting {
		t.Fatalf("after A ready: %+v", s)
	}
	b.state()
	b.send(TypeReady, ReadyPayload{Ready: true})
	sa := a.state()
	sb := b.state()
	if sa.Status != game.StatusPlaying || sb.Status != game.StatusPlaying {
		t.Fatalf("both ready -> playing: %s / %s", sa.Status, sb.Status)
	}

	// sanitisation: own hand visible, everything else hidden
	for _, p := range sa.Players {
		if p.ID == "dev:A" {
			if len(p.Hand) != game.HandSize || p.HandCount != game.HandSize {
				t.Fatalf("own hand: %+v", p)
			}
		} else if len(p.Hand) != 0 || p.HandCount != game.HandSize {
			t.Fatalf("opponent hand leaked: %+v", p)
		}
		if len(p.Stump) != 0 || p.StumpCount != game.StumpSize {
			t.Fatalf("stump leaked: %+v", p)
		}
	}
	if len(sa.Deck) != 0 || sa.DeckCount != game.DeckSize-1-2*(game.HandSize+game.StumpSize) || sa.TrumpCard != nil || sa.TrumpSuit != "" {
		t.Fatalf("deck/trump leaked: deck=%d count=%d trump=%v suit=%q", len(sa.Deck), sa.DeckCount, sa.TrumpCard, sa.TrumpSuit)
	}

	// the attacker opens; the defender makes an invalid move and gets ERROR + STATE_UPDATE
	attacker, defender := a, b
	attackerState := sa
	if sa.CurrentTurn == "dev:B" {
		attacker, defender = b, a
		attackerState = sb
	}
	var own *game.Player
	for i := range attackerState.Players {
		if attackerState.Players[i].ID == attackerState.ViewerID {
			own = &attackerState.Players[i]
		}
	}
	first := own.Hand[0].ID
	attacker.send(TypePlayCard, PlayCardPayload{CardID: first})
	s1 := attacker.state()
	s2 := defender.state()
	if len(s1.TableOrder) != 1 || s1.TableOrder[0] != first || len(s2.TableOrder) != 1 {
		t.Fatalf("attack not on table: %v / %v", s1.TableOrder, s2.TableOrder)
	}
	defender.send(TypePlayCard, PlayCardPayload{CardID: "ZZ", TargetCardID: first})
	if e := defender.errorPayload(); e.Code != "CARD_NOT_IN_HAND" || e.CardID != "ZZ" {
		t.Fatalf("invalid move error: %+v", e)
	}
	if s := defender.state(); len(s.TableOrder) != 1 {
		t.Fatalf("state after error must be re-sent: %+v", s.TableOrder)
	}
	// the attacker may not beat their own card
	attacker.send(TypePlayCard, PlayCardPayload{CardID: own.Hand[1].ID, TargetCardID: first})
	if e := attacker.errorPayload(); e.Code != "NOT_DEFENDER" {
		t.Fatalf("got %+v", e)
	}
	attacker.state()
	// PING is answered with PONG
	a.send(TypePing, nil)
	a.expect(TypePong)
}

func TestDisconnectInLobbyIsBroadcast(t *testing.T) {
	srv, hub := newTestServer(t, true)
	a := dial(t, srv)
	a.send(TypeJoinRoom, devJoin("ROOM", true, "A", "Alice"))
	a.state()
	b := dial(t, srv)
	b.send(TypeJoinRoom, devJoin("ROOM", false, "B", "Bob"))
	b.state()
	a.state()
	b.conn.Close()
	if s := a.state(); len(s.Players) != 1 || s.Players[0].ID != "dev:A" {
		t.Fatalf("after B left: %+v", s.Players)
	}
	// explicit leave empties the room; it is then deleted
	a.send(TypeLeaveRoom, nil)
	a.send(TypeTakeCards, nil)
	if e := a.errorPayload(); e.Code != "NOT_IN_ROOM" {
		t.Fatalf("after leave: %+v", e)
	}
	deadline := time.Now().Add(2 * time.Second)
	for hub.LocalRooms() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if hub.LocalRooms() != 0 {
		t.Fatalf("rooms not cleaned up: %d", hub.LocalRooms())
	}
}

func TestSecondConnectionReplacesFirst(t *testing.T) {
	srv, _ := newTestServer(t, true)
	a1 := dial(t, srv)
	a1.send(TypeJoinRoom, devJoin("R", true, "A", "Alice"))
	a1.state()
	a2 := dial(t, srv)
	a2.send(TypeJoinRoom, devJoin("R", false, "A", "Alice"))
	if s := a2.state(); len(s.Players) != 1 || !s.Players[0].Connected {
		t.Fatalf("rejoin: %+v", s.Players)
	}
	if e := a1.errorPayload(); e.Code != "REPLACED" {
		t.Fatalf("old connection: %+v", e)
	}
}

func telegramInitData(t *testing.T, userID int64, firstName string, token string) string {
	t.Helper()
	v := url.Values{}
	v.Set("auth_date", strconv.FormatInt(time.Now().Unix(), 10))
	v.Set("query_id", "AAE")
	userJSON, _ := json.Marshal(map[string]any{"id": userID, "first_name": firstName, "username": "u" + strconv.FormatInt(userID, 10)})
	v.Set("user", string(userJSON))
	v.Set("hash", telegram.Sign(v, token))
	return v.Encode()
}

func TestTelegramLogin(t *testing.T) {
	srv, _ := newTestServer(t, false)
	c := dial(t, srv)
	c.send(TypeJoinRoom, JoinPayload{RoomID: "TG", Create: true, TgInitData: telegramInitData(t, 279058397, "Влад", testToken)})
	s := c.state()
	if len(s.Players) != 1 || s.Players[0].ID != "279058397" || s.Players[0].Name != "Влад" || s.ViewerID != "279058397" {
		t.Fatalf("telegram identity: %+v", s.Players)
	}
	bad := dial(t, srv)
	bad.send(TypeJoinRoom, JoinPayload{RoomID: "TG", TgInitData: telegramInitData(t, 1, "Eve", "wrong:token")})
	if e := bad.errorPayload(); e.Code != "UNAUTHORIZED" {
		t.Fatalf("forged init data: %+v", e)
	}
	dev := dial(t, srv)
	dev.send(TypeJoinRoom, devJoin("TG", false, "X", "Xavier"))
	if e := dev.errorPayload(); e.Code != "UNAUTHORIZED" {
		t.Fatalf("dev login must be rejected when disabled: %+v", e)
	}
}

func TestOriginChecker(t *testing.T) {
	req := func(origin, host string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "http://"+host+"/ws", nil)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		return r
	}
	any := originChecker([]string{"*"})
	if !any(req("https://evil.example", "durag.example")) {
		t.Fatal("* must allow everything")
	}
	strict := originChecker([]string{"https://durag.example"})
	if !strict(req("https://durag.example", "api.durag.example")) || strict(req("https://evil.example", "api.durag.example")) {
		t.Fatal("strict origin check failed")
	}
	if !strict(req("https://api.durag.example", "api.durag.example")) || !strict(req("", "x")) {
		t.Fatal("same host and missing origin must pass")
	}
}

// craftBout gives the attacker 7♥ (nothing else of rank 7 or 10 anywhere) and
// the defender 10♥, so one defence ends the bout.
func craftBout(t *testing.T, st *store.Memory, room, attacker, defender string) {
	t.Helper()
	hands := map[string][]string{
		attacker: {"H_7", "S_9", "D_9", "C_11", "H_2", "C_3"},
		defender: {"H_10", "S_8", "D_4", "C_5", "H_6", "D_12"},
	}
	_, err := st.Update(context.Background(), room, false, func(s *game.GameState) error {
		used := map[string]bool{}
		for i := range s.Players {
			p := &s.Players[i]
			p.Hand = nil
			for _, id := range hands[p.ID] {
				c, _ := game.CardByID(id)
				p.Hand = append(p.Hand, c)
				used[id] = true
			}
			for _, c := range p.Stump {
				used[c.ID] = true
			}
		}
		trump, _ := game.CardByID("S_5")
		used[trump.ID] = true
		s.TrumpCard = &trump
		s.TrumpSuit = trump.Suit
		s.TrumpRevealed = false
		s.Deck = nil
		for _, c := range game.NewDeck() {
			if !used[c.ID] {
				s.Deck = append(s.Deck, c)
			}
		}
		s.CurrentTurn = attacker
		s.DefenderID = defender
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestBoutStaysOnTableThenResolves(t *testing.T) {
	eng := game.NewEngine(game.NewRand(5, 6))
	eng.ResolveDelay = 300 * time.Millisecond
	srv, _, st := newTestServerWithEngine(t, true, eng)
	a := dial(t, srv)
	a.send(TypeJoinRoom, devJoin("R", true, "A", "Alice"))
	a.state()
	b := dial(t, srv)
	b.send(TypeJoinRoom, devJoin("R", false, "B", "Bob"))
	b.state()
	a.state()
	a.send(TypeReady, ReadyPayload{Ready: true})
	a.state()
	b.state()
	b.send(TypeReady, ReadyPayload{Ready: true})
	a.state()
	b.state()
	craftBout(t, st, "R", "dev:A", "dev:B")

	a.send(TypePlayCard, PlayCardPayload{CardID: "H_7"})
	a.state()
	b.state()
	b.send(TypePlayCard, PlayCardPayload{CardID: "H_10", TargetCardID: "H_7"})
	resolving := b.state()
	a.state()
	if resolving.Phase != game.PhaseResolving || resolving.ResolveOutcome != game.OutcomeBito {
		t.Fatalf("expected resolving phase, got %q/%q", resolving.Phase, resolving.ResolveOutcome)
	}
	if len(resolving.TableOrder) != 1 || len(resolving.TableCards["H_7"]) != 1 {
		t.Fatalf("table must still show the bout: %v", resolving.TableOrder)
	}
	// moves are refused meanwhile
	a.send(TypePlayCard, PlayCardPayload{CardID: "S_9"})
	if e := a.errorPayload(); e.Code != "RESOLVING" {
		t.Fatalf("got %+v", e)
	}
	a.state()
	// ...and the hub clears the table by itself once the delay has passed
	final := b.state()
	if final.Phase != game.PhaseBout || len(final.TableOrder) != 0 || final.BoutNumber != 2 || final.DiscardCount != 2 {
		t.Fatalf("bout not resolved: phase %q table %v bout %d discard %d", final.Phase, final.TableOrder, final.BoutNumber, final.DiscardCount)
	}
	if final.CurrentTurn != "dev:B" || final.DefenderID != "dev:A" {
		t.Fatalf("roles %s / %s", final.CurrentTurn, final.DefenderID)
	}
	if a.state().Phase != game.PhaseBout {
		t.Fatal("A must get the resolved state too")
	}
}

func TestReactionsAreBroadcastAndRateLimited(t *testing.T) {
	srv, _ := newTestServer(t, true)
	a := dial(t, srv)
	a.send(TypeJoinRoom, devJoin("E", true, "A", "Alice"))
	a.state()
	b := dial(t, srv)
	b.send(TypeJoinRoom, devJoin("E", false, "B", "Bob"))
	b.state()
	a.state()

	a.send(TypeReact, ReactPayload{Emoji: "🔥"})
	for _, c := range []*testClient{a, b} {
		env := c.expect(TypeReaction)
		var r ReactionPayload
		if err := json.Unmarshal(env.Payload, &r); err != nil {
			t.Fatal(err)
		}
		if r.PlayerID != "dev:A" || r.Emoji != "🔥" || r.TS == 0 {
			t.Fatalf("reaction %+v", r)
		}
	}
	// unknown emoji -> error, nothing broadcast
	a.send(TypeReact, ReactPayload{Emoji: "<script>"})
	if e := a.errorPayload(); e.Code != "BAD_EMOJI" {
		t.Fatalf("got %+v", e)
	}
	// a second reaction right away is dropped silently; a PONG proves nothing else arrived
	a.send(TypeReact, ReactPayload{Emoji: "👍"})
	b.send(TypePing, nil)
	b.expect(TypePong)
	// not in a room
	z := dial(t, srv)
	z.send(TypeReact, ReactPayload{Emoji: "👍"})
	if e := z.errorPayload(); e.Code != "NOT_IN_ROOM" {
		t.Fatalf("got %+v", e)
	}
}
