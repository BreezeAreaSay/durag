package ws

import (
	"context"
	"encoding/json"
	"testing"
	"time"
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
