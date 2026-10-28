package ws

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/breezeareasay/durag/backend/internal/game"
)

func TestQuickPhrasesNeedTenWins(t *testing.T) {
	srv, _, st := newAdminServer(t)
	for i := 0; i < PhraseWins; i++ {
		if _, err := st.AddWin(context.Background(), "dev:A"); err != nil {
			t.Fatal(err)
		}
	}
	wsURL := "ws" + srv.URL[len("http"):] + "/ws"
	a := dialURL(t, wsURL)
	a.send(TypeJoinRoom, devJoin("SAY", true, "A", "Alice"))
	if s := a.state(); s.Players[0].Wins != PhraseWins {
		t.Fatalf("wins on join: %d", s.Players[0].Wins)
	}
	b := dialURL(t, wsURL)
	b.send(TypeJoinRoom, devJoin("SAY", false, "B", "Bob"))
	b.state()
	a.state()
	// a newcomer is locked out
	b.send(TypeSay, SayPayload{Text: "ха"})
	if e := b.errorPayload(); e.Code != "LOCKED_PHRASE" {
		t.Fatalf("got %+v", e)
	}
	// a phrase off the list is refused
	a.send(TypeSay, SayPayload{Text: "<script>"})
	if e := a.errorPayload(); e.Code != "BAD_PHRASE" {
		t.Fatalf("got %+v", e)
	}
	a.send(TypeSay, SayPayload{Text: "бери давай"})
	for _, c := range []*testClient{a, b} {
		env := c.expect(TypeReaction)
		var r ReactionPayload
		_ = json.Unmarshal(env.Payload, &r)
		if r.PlayerID != "dev:A" || r.Text != "бери давай" || r.Emoji != "" {
			t.Fatalf("phrase %+v", r)
		}
	}
	// the admin has the phrases at once
	adm := dialURL(t, wsURL)
	adm.send(TypeJoinRoom, devJoin("SAY", false, "admin", "Boss"))
	adm.state()
	a.state()
	b.state()
	adm.send(TypeSay, SayPayload{Text: "easy"})
	env := adm.expect(TypeReaction)
	var r ReactionPayload
	_ = json.Unmarshal(env.Payload, &r)
	if r.PlayerID != "dev:admin" || r.Text != "easy" {
		t.Fatalf("admin phrase %+v", r)
	}
}

func TestWinsOpenTheSecondReactionPage(t *testing.T) {
	srv, _, st := newAdminServer(t)
	for i := 0; i < 5; i++ {
		_, _ = st.AddWin(context.Background(), "dev:A")
	}
	wsURL := "ws" + srv.URL[len("http"):] + "/ws"
	a := dialURL(t, wsURL)
	a.send(TypeJoinRoom, devJoin("PAGE2", true, "A", "Alice"))
	a.state()
	a.send(TypeSendEmoji, ReactPayload{Emoji: "🧨"})
	if e := a.errorPayload(); e.Code != "LOCKED_EMOJI" {
		t.Fatalf("the firecracker needs 10 wins, got %+v", e)
	}
	time.Sleep(reactMinInterval)
	a.send(TypeSendEmoji, ReactPayload{Emoji: "🤝"})
	env := a.expect(TypeReaction)
	var r ReactionPayload
	_ = json.Unmarshal(env.Payload, &r)
	if r.Emoji != "🤝" || r.Effect != "shake" {
		t.Fatalf("handshake at 5 wins: %+v", r)
	}
}

func TestTheFirstSlapOnTheFlyEarnsAPoint(t *testing.T) {
	chance, dmin, dmax := FlyChance, FlyDelayMin, FlyDelayMax
	FlyChance, FlyDelayMin, FlyDelayMax = 1, 10*time.Millisecond, 10*time.Millisecond
	t.Cleanup(func() { FlyChance, FlyDelayMin, FlyDelayMax = chance, dmin, dmax })
	srv, _, st := newAdminServer(t)
	wsURL := "ws" + srv.URL[len("http"):] + "/ws"
	a := dialURL(t, wsURL)
	a.send(TypeJoinRoom, devJoin("FLY", true, "A", "Alice"))
	a.state()
	b := dialURL(t, wsURL)
	b.send(TypeJoinRoom, devJoin("FLY", false, "B", "Bob"))
	b.state()
	a.state()
	a.send(TypeReady, ReadyPayload{Ready: true})
	a.state()
	b.state()
	b.send(TypeReady, ReadyPayload{Ready: true})
	if s := a.state(); s.Status != "playing" {
		t.Fatalf("status %s", s.Status)
	}
	b.state()
	var launch FlyPayload
	for _, c := range []*testClient{a, b} {
		env := c.expect(TypeFly)
		if err := json.Unmarshal(env.Payload, &launch); err != nil || launch.ID == "" || launch.CaughtBy != "" {
			t.Fatalf("fly launch %s %v", string(env.Payload), err)
		}
	}
	// a slap on a fly that is not there misses silently
	b.send(TypeCatchFly, CatchFlyPayload{ID: "nope"})
	b.send(TypeCatchFly, CatchFlyPayload{ID: launch.ID})
	for _, c := range []*testClient{a, b} {
		sawState, sawCatch := false, false
		for i := 0; i < 2; i++ {
			env := c.next()
			switch env.Type {
			case TypeStateUpdate:
				var s stateXP
				_ = json.Unmarshal(env.Payload, &s)
				for _, p := range s.Players {
					if p.ID == "dev:B" && p.XP != 1 {
						t.Fatalf("Bob's xp in the state: %d", p.XP)
					}
				}
				sawState = true
			case TypeFly:
				var f FlyPayload
				_ = json.Unmarshal(env.Payload, &f)
				if f.CaughtBy != "dev:B" || f.XP != 1 || f.ID != launch.ID {
					t.Fatalf("catch %+v", f)
				}
				sawCatch = true
			default:
				t.Fatalf("unexpected %s %s", env.Type, string(env.Payload))
			}
		}
		if !sawState || !sawCatch {
			t.Fatalf("state %v catch %v", sawState, sawCatch)
		}
	}
	if u, _ := st.UserStats(context.Background(), "dev:B"); u.XP != 1 {
		t.Fatalf("stored xp %d", u.XP)
	}
	// nothing arrives for Alice's late slap
	a.send(TypeCatchFly, CatchFlyPayload{ID: launch.ID})
	a.send(TypePing, nil)
	a.expect(TypePong)
}

type stateXP struct {
	Players []struct {
		ID string `json:"id"`
		XP int    `json:"xp"`
	} `json:"players"`
}

func TestATableCanBeOpenedWithoutATurnTimer(t *testing.T) {
	eng := game.NewEngine(game.NewRand(3, 4))
	eng.TurnTimeout = 30 * time.Second
	srv, _, _ := newTestServerWithEngine(t, true, eng)
	a := dial(t, srv)
	a.send(TypeJoinRoom, JoinPayload{RoomID: "QUIET", Create: true, NoTimer: true, DevUser: &DevUser{ID: "A", Name: "Alice"}})
	if s := a.state(); !s.NoTimer {
		t.Fatal("the table was opened with no_timer")
	}
	b := dial(t, srv)
	b.send(TypeJoinRoom, JoinPayload{RoomID: "QUIET", Create: true, DevUser: &DevUser{ID: "B", Name: "Bob"}}) // a second "create" does not flip it
	b.state()
	a.state()
	a.send(TypeReady, ReadyPayload{Ready: true})
	a.state()
	b.state()
	b.send(TypeReady, ReadyPayload{Ready: true})
	s := a.state()
	if s.Status != game.StatusPlaying || !s.NoTimer || s.TurnDeadline != 0 || s.TurnTimeoutMs != 0 || len(s.TurnActors) != 0 {
		t.Fatalf("no timer must be armed: status=%s no_timer=%v deadline=%d ms=%d actors=%v", s.Status, s.NoTimer, s.TurnDeadline, s.TurnTimeoutMs, s.TurnActors)
	}
}

func TestEverybodyPicksANicknameAndGetsADish(t *testing.T) {
	srv, _ := newTestServer(t, true)
	a := dial(t, srv)
	a.send(TypeJoinRoom, JoinPayload{RoomID: "NICK", Create: true, Nickname: "  Жора_из 5-й ", DevUser: &DevUser{ID: "A", Name: "Alice", AvatarURL: "https://t.me/i/userpic/x.jpg"}})
	s := a.state()
	me := s.Player("dev:A")
	if me.Name != "Жора_из 5-й" || me.AvatarURL != "" || me.Avatar == "" || me.Avatar != foodAvatar("dev:A") {
		t.Fatalf("name %q avatar_url %q avatar %q", me.Name, me.AvatarURL, me.Avatar)
	}
	// a dev identity without a usable nickname falls back on its own name (scripts, tests)
	b := dial(t, srv)
	b.send(TypeJoinRoom, JoinPayload{RoomID: "NICK", Nickname: "x", DevUser: &DevUser{ID: "B", Name: "Bob"}})
	if s := b.state(); s.Player("dev:B").Name != "Bob" {
		t.Fatalf("fallback name %q", s.Player("dev:B").Name)
	}
	if nickname("<script>alert(1)</script>") != "scriptalert1script" || nickname("a") != "" || len([]rune(nickname("очень длинное имя которое не влезает никуда"))) != 20 {
		t.Fatal("nickname cleaning")
	}
	// the dish is stable for an id and spread over ids
	if foodAvatar("dev:A") != foodAvatar("dev:A") || foodAvatar("1") == foodAvatar("2") && foodAvatar("2") == foodAvatar("3") {
		t.Fatal("food avatar")
	}
}

func TestTheTalkBudgetMetersTheModel(t *testing.T) {
	var b talkBudget
	now := time.Now()
	o := BotOptions{TalkShare: 1, TalkDaily: 3}
	got := 0
	for i := 0; i < 10; i++ {
		if b.allow(o, now) {
			got++
		}
	}
	if got != 3 {
		t.Fatalf("daily cap: %d calls allowed, want 3", got)
	}
	if !b.allow(o, now.Add(24*time.Hour)) {
		t.Fatal("a new day opens the budget again")
	}
	half := BotOptions{TalkShare: 0.5}
	hits := 0
	for i := 0; i < 400; i++ {
		if b.allow(half, now) {
			hits++
		}
	}
	if hits < 140 || hits > 260 {
		t.Fatalf("a half share let %d of 400 through", hits)
	}
}

func TestTheHostKicksAndSwapsBots(t *testing.T) {
	srv, _, _ := newAdminServer(t)
	wsURL := "ws" + srv.URL[len("http"):] + "/ws"
	host := dialURL(t, wsURL)
	host.send(TypeJoinRoom, devJoin("KICK", true, "H", "Host"))
	host.state()
	guest := dialURL(t, wsURL)
	guest.send(TypeJoinRoom, devJoin("KICK", false, "G", "Guest"))
	guest.state()
	host.state()
	host.send(TypeAddBot, nil)
	s := host.state()
	guest.state()
	botID, botName := s.Players[2].ID, s.Players[2].Name
	// a tap on the bot's face: another character takes the seat
	guest.send(TypeSwapBot, KickPayload{PlayerID: botID})
	s = guest.state()
	host.state()
	if s.Players[2].ID != botID || s.Players[2].Name == botName || s.Players[2].Avatar == "" {
		t.Fatalf("swap: %+v", s.Players[2])
	}
	// a guest cannot kick, the host can: the guest is shown the door
	guest.send(TypeKick, KickPayload{PlayerID: botID})
	if e := guest.errorPayload(); e.Code != "NOT_HOST" {
		t.Fatalf("got %+v", e)
	}
	host.send(TypeKick, KickPayload{PlayerID: "dev:G"})
	if e := guest.errorPayload(); e.Code != "KICKED" {
		t.Fatalf("got %+v", e)
	}
	s = host.state()
	if len(s.Players) != 2 || s.Player("dev:G") != nil {
		t.Fatalf("after the kick: %d players", len(s.Players))
	}
	host.send(TypeKick, KickPayload{PlayerID: botID})
	if s = host.state(); len(s.Players) != 1 {
		t.Fatalf("the bot was not kicked: %d players", len(s.Players))
	}
}
