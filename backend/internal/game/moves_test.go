package game

import "testing"

// Every move LegalMoves lists must be accepted by the engine, on any state a
// random game passes through.
func TestLegalMovesAreAcceptedByTheEngine(t *testing.T) {
	for seed := uint64(1); seed <= 12; seed++ {
		e := NewEngine(NewRand(seed, 99))
		s := NewGameState("LM")
		names := []string{"A", "B", "C", "D"}
		for _, n := range names[:2+int(seed%3)] {
			if err := e.Join(s, n, n, ""); err != nil {
				t.Fatal(err)
			}
		}
		for _, n := range names[:2+int(seed%3)] {
			if err := e.SetReady(s, n, true); err != nil {
				t.Fatal(err)
			}
		}
		rng := NewRand(seed, 7)
		for step := 0; step < 2000 && s.Status == StatusPlaying; step++ {
			e.ResolveIfDue(s, e.now())
			e.StumpsIfDue(s, e.now())
			var all []struct {
				id string
				m  Move
			}
			for _, p := range s.Players {
				for _, m := range s.LegalMoves(p.ID) {
					clone := s.Clone()
					if err := e.ApplyMove(clone, p.ID, m); err != nil {
						t.Fatalf("seed %d step %d: legal move %s for %s rejected: %v", seed, step, m, p.ID, err)
					}
					all = append(all, struct {
						id string
						m  Move
					}{p.ID, m})
				}
			}
			if len(all) == 0 {
				if s.Status == StatusPlaying && s.Phase != PhaseResolving && !s.StumpsPending() {
					t.Fatalf("seed %d step %d: nobody can move: %s", seed, step, mustJSON(t, s))
				}
				continue
			}
			pick := all[rng.IntN(len(all))]
			if err := e.ApplyMove(s, pick.id, pick.m); err != nil {
				t.Fatalf("seed %d: apply %s: %v", seed, pick.m, err)
			}
			if got := countCards(t, s); got != DeckSize {
				t.Fatalf("seed %d step %d: %d cards accounted for", seed, step, got)
			}
		}
		if s.Status != StatusFinished {
			t.Fatalf("seed %d: game did not finish", seed)
		}
	}
}

func TestLegalMovesRespectRoles(t *testing.T) {
	e, s := threePlayers(t)
	// attacker on an empty table: one attack per card, nothing else
	for _, m := range s.LegalMoves("A") {
		if m.Type != MoveAttack {
			t.Fatalf("unexpected %s on an empty table", m)
		}
	}
	if len(s.LegalMoves("B")) != 0 || len(s.LegalMoves("C")) != 0 {
		t.Fatalf("only the attacker moves on an empty table")
	}
	if err := e.PlayCard(s, "A", "H_7", ""); err != nil {
		t.Fatal(err)
	}
	var kinds = map[MoveType]int{}
	for _, m := range s.LegalMoves("B") {
		kinds[m.Type]++
	}
	// B holds H_10 (beats H_7), D_7 and C_7 (transfers), and may take
	if kinds[MoveDefend] == 0 || kinds[MoveTransfer] != 2 || kinds[MoveTake] != 1 || kinds[MoveAttack] != 0 {
		t.Fatalf("defender moves: %v", kinds)
	}
	kinds = map[MoveType]int{}
	for _, m := range s.LegalMoves("A") {
		kinds[m.Type]++
	}
	if kinds[MoveThrowIn] != 1 || kinds[MovePass] != 1 || kinds[MoveDefend] != 0 { // S_7 matches
		t.Fatalf("attacker moves after the opening: %v", kinds)
	}
}

func TestBotsInTheLobby(t *testing.T) {
	e := NewEngine(NewRand(1, 2))
	s := NewGameState("BOTS")
	if err := e.Join(s, "h1", "Human", ""); err != nil {
		t.Fatal(err)
	}
	if err := e.AddBot(s, "bot:1", "Нейронка"); err != nil {
		t.Fatal(err)
	}
	if b := s.Player("bot:1"); b == nil || !b.IsBot || !b.IsReady || !b.Connected {
		t.Fatalf("bot seat: %+v", b)
	}
	if s.HumanCount() != 1 {
		t.Fatalf("human count %d", s.HumanCount())
	}
	wantErr(t, e.RemoveBot(s, "h1"), ErrNotABot)
	// at least one human seat stays free: with max 6 at most 5 bots
	for i := 2; i <= 5; i++ {
		if err := e.AddBot(s, "bot:"+string(rune('0'+i)), "Bot"); err != nil {
			t.Fatalf("bot %d: %v", i, err)
		}
	}
	wantErr(t, e.AddBot(s, "bot:9", "Bot"), ErrRoomFull)
	if err := e.RemoveBot(s, "bot:5"); err != nil {
		t.Fatal(err)
	}
	// a human's ready starts the game against the (always ready) bots
	if err := e.SetReady(s, "h1", true); err != nil {
		t.Fatal(err)
	}
	if s.Status != StatusPlaying {
		t.Fatalf("status %s", s.Status)
	}
	wantErr(t, e.AddBot(s, "bot:7", "Late"), ErrGameInProgress)
}
