package game

import (
	"testing"
	"time"
)

// --- resolving phase -------------------------------------------------------

func newResolvingGame(t *testing.T) (*Engine, *GameState, *time.Time) {
	t.Helper()
	e, s := newPlaying(t, "A", "B")
	now := time.Unix(1_700_000_000, 0)
	e.Now = func() time.Time { return now }
	e.ResolveDelay = 2 * time.Second
	setRoles(s, "A", "B")
	setHand(t, s, "A", "H_7", "S_9", "D_9", "C_11", "H_2", "C_3")
	setHand(t, s, "B", "H_10", "S_8", "D_4", "C_5", "H_6", "D_12")
	setTrump(t, s, "S_5", false)
	rebuildDeck(t, s)
	return e, s, &now
}

func TestBoutWaitsInResolvingPhase(t *testing.T) {
	e, s, now := newResolvingGame(t)
	if err := e.PlayCard(s, "A", "H_7", ""); err != nil {
		t.Fatal(err)
	}
	if err := e.PlayCard(s, "B", "H_10", "H_7"); err != nil {
		t.Fatal(err)
	}
	if s.Phase != PhaseResolving || s.ResolveOutcome != OutcomeBito {
		t.Fatalf("phase %q outcome %q", s.Phase, s.ResolveOutcome)
	}
	if s.TableEmpty() || len(s.TableCards["H_7"]) != 1 {
		t.Fatal("the table must stay visible while resolving")
	}
	if s.ResolveAt != now.Add(2*time.Second).UnixMilli() {
		t.Fatalf("resolve_at %d", s.ResolveAt)
	}
	if len(s.Player("A").Hand) != 5 || len(s.Player("B").Hand) != 5 {
		t.Fatal("hands must not be refilled before the bout is resolved")
	}
	if last := s.Log[len(s.Log)-1]; last.Type != "bout_end" || last.Text != OutcomeBito {
		t.Fatalf("last log %+v", last)
	}
	// nothing may happen on the table meanwhile
	wantErr(t, e.PlayCard(s, "B", "S_8", ""), ErrResolving)
	wantErr(t, e.Pass(s, "A"), ErrResolving)
	wantErr(t, e.TakeCards(s, "B"), ErrResolving)
	if _, err := s.ValidateTransfer("B", "D_4"); err != ErrResolving {
		t.Fatalf("transfer while resolving: %v", err)
	}
	// not due yet
	if e.ResolveIfDue(s, now.Add(time.Second)) {
		t.Fatal("resolved too early")
	}
	if !e.ResolveIfDue(s, now.Add(2500*time.Millisecond)) {
		t.Fatal("should resolve once the deadline passed")
	}
	if s.Phase != PhaseBout || !s.TableEmpty() || s.DiscardCount != 2 {
		t.Fatalf("after resolve: phase %q table %v discard %d", s.Phase, s.TableOrder, s.DiscardCount)
	}
	if len(s.Player("A").Hand) != HandSize || len(s.Player("B").Hand) != HandSize {
		t.Fatal("hands refilled after resolve")
	}
	if s.CurrentTurn != "B" || s.DefenderID != "A" || s.BoutNumber != 2 {
		t.Fatalf("roles after resolve: %s / %s bout %d", s.CurrentTurn, s.DefenderID, s.BoutNumber)
	}
	wantErr(t, e.ResolveBout(s), ErrNotResolving)
}

func TestTakeAlsoWaitsInResolvingPhase(t *testing.T) {
	e, s, _ := newResolvingGame(t)
	_ = e.PlayCard(s, "A", "H_7", "")
	if err := e.TakeCards(s, "B"); err != nil {
		t.Fatal(err)
	}
	// A holds nothing matching: attackers are done, the bout parks itself
	if s.Phase != PhaseResolving || s.ResolveOutcome != OutcomeTook || !s.DefenderTaking {
		t.Fatalf("phase %q outcome %q taking %v", s.Phase, s.ResolveOutcome, s.DefenderTaking)
	}
	if len(s.Player("B").Hand) != 6 {
		t.Fatal("cards go to the defender only when the bout is resolved")
	}
	if err := e.ResolveBout(s); err != nil {
		t.Fatal(err)
	}
	if len(s.Player("B").Hand) != 7 || !s.TableEmpty() || s.DefenderTaking {
		t.Fatalf("after resolve: hand %d table %v taking %v", len(s.Player("B").Hand), s.TableOrder, s.DefenderTaking)
	}
	if s.CurrentTurn != "A" || s.DefenderID != "B" {
		t.Fatalf("after a take the taker defends again in a 2-player game: %s / %s", s.CurrentTurn, s.DefenderID)
	}
}

func TestLeaveDuringResolvingClearsPhase(t *testing.T) {
	e, s := newPlaying(t, "A", "B", "C")
	e.ResolveDelay = 2 * time.Second
	setRoles(s, "A", "B")
	setHand(t, s, "A", "H_7", "S_9", "D_9", "C_11", "H_2", "C_3")
	setHand(t, s, "B", "H_10", "S_8", "D_4", "C_5", "H_6", "D_12")
	setHand(t, s, "C", "C_2", "C_4", "D_6", "S_12", "H_13", "D_14")
	setTrump(t, s, "S_5", false)
	rebuildDeck(t, s)
	_ = e.PlayCard(s, "A", "H_7", "")
	_ = e.PlayCard(s, "B", "H_10", "H_7")
	if s.Phase != PhaseResolving {
		t.Fatalf("phase %q", s.Phase)
	}
	if err := e.Leave(s, "B"); err != nil {
		t.Fatal(err)
	}
	if s.Phase != PhaseBout || !s.TableEmpty() {
		t.Fatal("leaving must abort the resolving bout cleanly")
	}
}

// --- empty hands in the end-game -----------------------------------------

func endgameTable(t *testing.T) (*Engine, *GameState, *time.Time) {
	t.Helper()
	e, s := newPlaying(t, "A", "B", "C")
	now := time.Unix(1_700_000_000, 0)
	e.Now = func() time.Time { return now }
	e.StumpAutoDelay = 3 * time.Second
	setRoles(s, "A", "B")
	setHand(t, s, "A", "H_7", "S_9")
	setHand(t, s, "B", "H_10", "S_8", "D_4")
	setHand(t, s, "C", "C_7")
	setStump(t, s, "C", "D_7", "S_14")
	setStump(t, s, "A", "C_2", "C_3")
	setStump(t, s, "B", "D_2", "D_3")
	setTrump(t, s, "S_5", true)
	s.TrumpDrawnBy = "B"
	s.Deck = []Card{}
	s.DiscardCount = DeckSize - countCards(t, s)
	return e, s, &now
}

func TestStumpIsTakenAsSeparateStepAfterTheBout(t *testing.T) {
	e, s, now := endgameTable(t)
	_ = e.PlayCard(s, "A", "H_7", "")
	if err := e.PlayCard(s, "C", "C_7", ""); err != nil {
		t.Fatal(err)
	}
	c := s.Player("C")
	if len(c.Hand) != 0 || len(c.Stump) != 2 || s.StumpsPending() {
		t.Fatalf("nothing happens to the stump during the bout: hand %v stump %v pending %v", c.Hand, c.Stump, s.StumpPending)
	}
	// the hand is empty: C cannot take the stump mid-bout
	wantErr(t, e.TakeStump(s, "C"), ErrNoStump)
	// B beats both sevens; nobody can throw in -> bout over
	if err := e.PlayCard(s, "B", "H_10", "H_7"); err != nil {
		t.Fatal(err)
	}
	if err := e.PlayCard(s, "B", "S_8", "C_7"); err != nil {
		t.Fatal(err)
	}
	if !s.TableEmpty() {
		t.Fatalf("bout should be over, table %v", s.TableOrder)
	}
	if len(s.StumpPending) != 1 || s.StumpPending[0] != "C" || s.StumpDeadline != now.Add(3*time.Second).UnixMilli() {
		t.Fatalf("C must be asked to take the stump: pending %v deadline %d", s.StumpPending, s.StumpDeadline)
	}
	if len(c.Hand) != 0 || len(c.Stump) != 2 {
		t.Fatal("the stump is not taken automatically before the delay")
	}
	// roles are assigned but the next bout waits for the stump
	if s.CurrentTurn != "B" || s.DefenderID != "C" {
		t.Fatalf("roles %s / %s", s.CurrentTurn, s.DefenderID)
	}
	wantErr(t, e.PlayCard(s, "B", "D_4", ""), ErrStumpPending)
	if s.TurnDeadline != 0 {
		t.Fatal("no turn timer while stumps are pending")
	}
	// A has cards: not allowed to take anything
	wantErr(t, e.TakeStump(s, "A"), ErrNoStump)
	if err := e.TakeStump(s, "C"); err != nil {
		t.Fatal(err)
	}
	if len(c.Hand) != 2 || len(c.Stump) != 0 || s.StumpsPending() || s.StumpDeadline != 0 {
		t.Fatalf("after taking: hand %v stump %v pending %v", c.Hand, c.Stump, s.StumpPending)
	}
	if err := e.PlayCard(s, "B", "D_4", ""); err != nil {
		t.Fatalf("the next bout may start: %v", err)
	}
	if n := countCards(t, s); n != DeckSize {
		t.Fatalf("cards %d", n)
	}
}

func TestPendingStumpsAreTakenAutomaticallyAfterTheDelay(t *testing.T) {
	e, s, now := endgameTable(t)
	_ = e.PlayCard(s, "A", "H_7", "")
	_ = e.PlayCard(s, "C", "C_7", "")
	_ = e.PlayCard(s, "B", "H_10", "H_7")
	_ = e.PlayCard(s, "B", "S_8", "C_7")
	if !s.StumpsPending() {
		t.Fatal("expected a pending stump")
	}
	if e.StumpsIfDue(s, now.Add(time.Second)) {
		t.Fatal("too early")
	}
	if !e.StumpsIfDue(s, now.Add(4*time.Second)) {
		t.Fatal("should be taken after the delay")
	}
	if len(s.Player("C").Hand) != 2 || s.StumpsPending() {
		t.Fatalf("auto-take failed: %v", s.Player("C").Hand)
	}
}

func TestStumpTakenImmediatelyWithoutDelay(t *testing.T) {
	e, s, _ := endgameTable(t)
	e.StumpAutoDelay = 0
	_ = e.PlayCard(s, "A", "H_7", "")
	_ = e.PlayCard(s, "C", "C_7", "")
	_ = e.PlayCard(s, "B", "H_10", "H_7")
	_ = e.PlayCard(s, "B", "S_8", "C_7")
	if s.StumpsPending() || len(s.Player("C").Hand) != 2 {
		t.Fatalf("with no delay the stump goes straight to the hand: pending %v hand %v", s.StumpPending, s.Player("C").Hand)
	}
}

func TestDefenderWithoutCardsMustTakeTheTable(t *testing.T) {
	e, s := newPlaying(t, "A", "B")
	setRoles(s, "A", "B")
	setHand(t, s, "A", "H_7", "S_7", "D_9")
	setHand(t, s, "B", "H_10")
	setStump(t, s, "B", "S_10", "D_2")
	setStump(t, s, "A")
	setTrump(t, s, "C_5", true)
	s.Deck = []Card{}
	s.DiscardCount = DeckSize - countCards(t, s)
	_ = e.PlayCard(s, "A", "H_7", "")
	_ = e.PlayCard(s, "A", "S_7", "")
	if err := e.PlayCard(s, "B", "H_10", "H_7"); err != nil {
		t.Fatal(err)
	}
	b := s.Player("B")
	if len(b.Hand) != 0 || len(b.Stump) != 2 {
		t.Fatalf("the stump stays put mid-bout: hand %v stump %v", b.Hand, b.Stump)
	}
	wantErr(t, e.TakeStump(s, "B"), ErrNoStump)
	// nothing left to beat with: the only option is to take
	if err := e.TakeCards(s, "B"); err != nil {
		t.Fatal(err)
	}
	if !s.TableEmpty() || len(b.Hand) != 3 {
		t.Fatalf("B took the table: %v", b.Hand)
	}
	if s.StumpsPending() {
		t.Fatal("B has cards again, no stump step")
	}
}

func TestPlayerWithoutCardsLeavesAtTheEndOfTheBout(t *testing.T) {
	e, s := newPlaying(t, "A", "B", "C")
	setRoles(s, "A", "B")
	setHand(t, s, "A", "H_7")
	setHand(t, s, "B", "H_10", "S_8")
	setHand(t, s, "C", "C_9", "D_10")
	setStump(t, s, "A")
	setStump(t, s, "B")
	setStump(t, s, "C")
	setTrump(t, s, "C_5", true) // clubs are trumps: S_8 cannot beat D_10
	s.Deck = []Card{}
	s.DiscardCount = DeckSize - countCards(t, s)
	if err := e.PlayCard(s, "A", "H_7", ""); err != nil {
		t.Fatal(err)
	}
	if s.Player("A").Out {
		t.Fatal("players leave at the end of the bout, not mid-bout")
	}
	if err := e.PlayCard(s, "B", "H_10", "H_7"); err != nil {
		t.Fatal(err)
	}
	if err := e.PlayCard(s, "C", "D_10", ""); err != nil {
		t.Fatalf("C throws in: %v", err)
	}
	if err := e.TakeCards(s, "B"); err != nil {
		t.Fatal(err)
	}
	if !s.Player("A").Out || len(s.FinishedOrder) != 1 || s.FinishedOrder[0] != "A" {
		t.Fatalf("A is out once the bout ended: %+v", s.Player("A"))
	}
	if s.Status != StatusPlaying || s.CurrentTurn != "C" || s.DefenderID != "B" {
		t.Fatalf("game continues between B and C: %s %s/%s", s.Status, s.CurrentTurn, s.DefenderID)
	}
	if n := countCards(t, s); n != DeckSize {
		t.Fatalf("cards %d", n)
	}
}

func TestLastCardDrawIsStillPossible(t *testing.T) {
	// Both run out in the final bout: the defender beats the attacker's last
	// card with their own last card -> draw, nobody is the fool.
	e, s := newPlaying(t, "A", "B")
	setRoles(s, "A", "B")
	setHand(t, s, "A", "H_7")
	setHand(t, s, "B", "H_10")
	setStump(t, s, "A")
	setStump(t, s, "B")
	setTrump(t, s, "S_5", true)
	s.Deck = []Card{}
	s.DiscardCount = DeckSize - 2
	_ = e.PlayCard(s, "A", "H_7", "")
	if s.Status != StatusPlaying {
		t.Fatal("the game must wait for B's answer")
	}
	_ = e.PlayCard(s, "B", "H_10", "H_7")
	if s.Status != StatusFinished || s.LoserID != "" {
		t.Fatalf("expected a draw, got status %s loser %q", s.Status, s.LoserID)
	}
}

func TestTurnTimerPlaysDefaultMoves(t *testing.T) {
	e, s := newPlaying(t, "A", "B")
	now := time.Unix(1_700_000_000, 0)
	e.Now = func() time.Time { return now }
	e.TurnTimeout = 30 * time.Second
	setRoles(s, "A", "B")
	setHand(t, s, "A", "H_7", "S_9", "D_9", "C_11", "H_2", "C_3")
	setHand(t, s, "B", "H_10", "S_8", "D_4", "C_5", "H_6", "D_12")
	setTrump(t, s, "S_5", false)
	rebuildDeck(t, s)
	e.armTurn(s)
	if len(s.TurnActors) != 1 || s.TurnActors[0] != "A" || s.TurnDeadline != now.Add(30*time.Second).UnixMilli() || s.TurnTimeoutMs != 30000 {
		t.Fatalf("attacker must be on the clock: %v %d", s.TurnActors, s.TurnDeadline)
	}
	if e.ExpireTurn(s, now.Add(10*time.Second)) {
		t.Fatal("not expired yet")
	}
	// attacker idles: the lowest card (2♥) is led for them
	now = now.Add(31 * time.Second)
	if !e.ExpireTurn(s, now) {
		t.Fatal("timer should fire")
	}
	if len(s.TableOrder) != 1 || s.TableOrder[0] != "H_2" {
		t.Fatalf("auto attack with the lowest card expected, table %v", s.TableOrder)
	}
	if len(s.TurnActors) != 1 || s.TurnActors[0] != "B" {
		t.Fatalf("defender must be on the clock now: %v", s.TurnActors)
	}
	// defender idles: takes
	now = now.Add(31 * time.Second)
	if !e.ExpireTurn(s, now) {
		t.Fatal("timer should fire for the defender")
	}
	if !s.DefenderTaking && !s.TableEmpty() {
		t.Fatalf("auto take expected: taking=%v table=%v", s.DefenderTaking, s.TableOrder)
	}
	// nobody holds a matching card, so the bout resolved immediately (no delay in this engine)
	if !s.TableEmpty() {
		t.Fatalf("bout should have ended, table %v", s.TableOrder)
	}
	if len(s.Player("B").Hand) != 7 {
		t.Fatalf("B took the 2♥: %d cards", len(s.Player("B").Hand))
	}
	if len(s.TurnActors) != 1 || s.TurnActors[0] != "A" {
		t.Fatalf("A attacks again after the take: %v", s.TurnActors)
	}
	found := 0
	for _, entry := range s.Log {
		if entry.Type == "timeout" {
			found++
		}
	}
	if found != 2 {
		t.Fatalf("expected two timeout log entries, got %d", found)
	}
}

func TestTurnTimerPassesIdleThrowers(t *testing.T) {
	e, s := newPlaying(t, "A", "B", "C")
	now := time.Unix(1_700_000_000, 0)
	e.Now = func() time.Time { return now }
	e.TurnTimeout = 20 * time.Second
	setRoles(s, "A", "B")
	setHand(t, s, "A", "H_7", "S_7", "D_9", "C_11", "H_2", "C_3")
	setHand(t, s, "B", "H_10", "S_8", "D_4", "C_5", "H_6", "D_12")
	setHand(t, s, "C", "C_7", "D_10", "S_14", "H_9", "C_2", "D_2")
	setTrump(t, s, "C_6", false)
	rebuildDeck(t, s)
	_ = e.PlayCard(s, "A", "H_7", "")
	_ = e.PlayCard(s, "B", "H_10", "H_7")
	// all defended; A (7♠) and C (7♣, 10♦) could still throw in
	if len(s.TurnActors) != 2 {
		t.Fatalf("both potential throwers on the clock: %v", s.TurnActors)
	}
	now = now.Add(21 * time.Second)
	if !e.ExpireTurn(s, now) {
		t.Fatal("timer should fire")
	}
	if !s.TableEmpty() || s.DiscardCount != 2 {
		t.Fatalf("idle throwers pass and the bout ends: table %v discard %d", s.TableOrder, s.DiscardCount)
	}
}

func TestNoTurnTimerWhenDisabled(t *testing.T) {
	_, s := newPlaying(t, "A", "B")
	if s.TurnDeadline != 0 || len(s.TurnActors) != 0 {
		t.Fatalf("timer disabled by default: %d %v", s.TurnDeadline, s.TurnActors)
	}
}

func TestStumpWaitsWhileDeckHasCards(t *testing.T) {
	e, s := newPlaying(t, "A", "B", "C")
	setRoles(s, "A", "B")
	setHand(t, s, "A", "H_7")
	setHand(t, s, "B", "H_10", "S_8")
	setHand(t, s, "C", "C_9")
	setTrump(t, s, "S_5", false)
	rebuildDeck(t, s) // plenty of cards left
	_ = e.PlayCard(s, "A", "H_7", "")
	a := s.Player("A")
	if len(a.Hand) != 0 || len(a.Stump) != 2 || a.Out {
		t.Fatalf("with cards in the deck nothing happens mid-bout: %+v", a)
	}
}

func TestTransferNeedsEnoughCardsInNextHand(t *testing.T) {
	e, s := newPlaying(t, "A", "B")
	setRoles(s, "A", "B")
	setHand(t, s, "A", "H_7")
	setHand(t, s, "B", "S_7", "D_3")
	setTrump(t, s, "C_5", false)
	rebuildDeck(t, s)
	_ = e.PlayCard(s, "A", "H_7", "")
	// A has no cards left at all
	if _, err := s.ValidateTransfer("B", "S_7"); err != ErrTransferTooFewCards {
		t.Fatalf("got %v", err)
	}
	// one card is not enough to answer two attack cards after the transfer
	setHand(t, s, "A", "C_2")
	if _, err := s.ValidateTransfer("B", "S_7"); err != ErrTransferTooFewCards {
		t.Fatalf("got %v", err)
	}
	// two cards: exactly enough
	setHand(t, s, "A", "C_2", "C_4")
	if _, err := s.ValidateTransfer("B", "S_7"); err != nil {
		t.Fatalf("transfer with enough cards: %v", err)
	}
	// three attack cards would need three cards in the next hand
	setHand(t, s, "A", "H_7", "D_7", "C_7", "C_2", "C_4")
	setHand(t, s, "B", "S_7", "D_3", "H_2", "S_3", "D_4", "C_5")
	setRoles(s, "A", "B")
	_ = e.PlayCard(s, "A", "H_7", "")
	_ = e.PlayCard(s, "A", "D_7", "")
	setHand(t, s, "A", "C_7", "C_2") // 2 cards vs 3 attacks after transfer
	if _, err := s.ValidateTransfer("B", "S_7"); err != ErrTransferTooFewCards {
		t.Fatalf("got %v", err)
	}
	setHand(t, s, "A", "C_7", "C_2", "C_4")
	if _, err := s.ValidateTransfer("B", "S_7"); err != nil {
		t.Fatalf("got %v", err)
	}
}

func TestRandomPlayoutsWithResolvingPhase(t *testing.T) {
	for seed := uint64(100); seed < 120; seed++ {
		rng := NewRand(seed, seed+1)
		e := NewEngine(rng)
		e.ResolveDelay = time.Second
		e.StumpAutoDelay = 3 * time.Second
		clock := time.Unix(1_700_000_000, 0)
		e.Now = func() time.Time { return clock }
		s := NewGameState("r")
		n := 2 + int(seed%5)
		for i := 0; i < n; i++ {
			_ = e.Join(s, string(rune('A'+i)), "", "")
		}
		if err := e.Start(s); err != nil {
			t.Fatal(err)
		}
		for step := 0; step < 20000 && s.Status != StatusFinished; step++ {
			if n := countCards(t, s); n != DeckSize {
				t.Fatalf("seed %d: %d cards", seed, n)
			}
			if s.Phase == PhaseResolving {
				if len(legalMoves(s)) != 0 {
					t.Fatalf("seed %d: moves allowed while resolving", seed)
				}
				if e.ResolveIfDue(s, clock) {
					t.Fatalf("seed %d: resolved before the deadline", seed)
				}
				clock = clock.Add(2 * time.Second)
				if !e.ResolveIfDue(s, clock) {
					t.Fatalf("seed %d: not resolved after the deadline", seed)
				}
				continue
			}
			if s.StumpsPending() {
				if len(legalMoves(s)) != 0 {
					t.Fatalf("seed %d: moves allowed while stumps are pending", seed)
				}
				// half of the time the players take their stumps themselves
				if seed%2 == 0 {
					for _, id := range append([]string{}, s.StumpPending...) {
						if err := e.TakeStump(s, id); err != nil {
							t.Fatalf("seed %d: take stump %s: %v", seed, id, err)
						}
					}
				} else {
					clock = clock.Add(5 * time.Second)
					if !e.StumpsIfDue(s, clock) {
						t.Fatalf("seed %d: stumps not taken after the deadline", seed)
					}
				}
				continue
			}
			ms := legalMoves(s)
			if len(ms) == 0 {
				t.Fatalf("seed %d step %d: dead-lock\n%s", seed, step, mustJSON(t, s))
			}
			m := ms[rng.IntN(len(ms))]
			var err error
			switch m.kind {
			case "play":
				err = e.PlayCard(s, m.player, m.card, m.target)
			case "transfer":
				err = e.TransferTurn(s, m.player, m.card)
			case "take":
				err = e.TakeCards(s, m.player)
			case "pass":
				err = e.Pass(s, m.player)
			}
			if err != nil {
				t.Fatalf("seed %d: %+v rejected: %v", seed, m, err)
			}
		}
		if s.Status != StatusFinished {
			t.Fatalf("seed %d: game did not finish", seed)
		}
		if n := countCards(t, s); n != DeckSize {
			t.Fatalf("seed %d: %d cards at the end", seed, n)
		}
	}
}

func TestTrumpStaysSecretUntilSomebodyDrawsIt(t *testing.T) {
	// The main deck runs dry while every hand is full: the trump card stays
	// face down at the bottom and the trump suit is NOT active yet.
	e, s := newPlaying(t, "A", "B")
	setRoles(s, "A", "B")
	setHand(t, s, "A", "H_7", "S_9", "D_9", "C_11", "H_2", "C_3")
	setHand(t, s, "B", "H_10", "S_8", "D_4", "C_5", "H_6", "D_12")
	setTrump(t, s, "S_5", false)
	rebuildDeck(t, s)
	s.Deck = s.Deck[:2] // exactly what the two players draw back
	s.DiscardCount = DeckSize - countCards(t, s)
	_ = e.PlayCard(s, "A", "H_7", "")
	// a spade does not beat a heart while the trump is unknown
	if _, err := s.ValidatePlay("B", "S_8", "H_7"); err != ErrCannotBeat {
		t.Fatalf("hidden trump must not count: %v", err)
	}
	if err := e.PlayCard(s, "B", "H_10", "H_7"); err != nil {
		t.Fatal(err)
	}
	if len(s.Deck) != 0 || !s.TrumpInDeck() || s.TrumpRevealed {
		t.Fatalf("deck %d inDeck %v revealed %v", len(s.Deck), s.TrumpInDeck(), s.TrumpRevealed)
	}
	if s.ActiveTrump() != "" {
		t.Fatal("trump suit must stay inactive until the card is drawn")
	}
	// next bout: B leads, A beats, both draw -> B (attacker) gets the trump
	setRoles(s, "B", "A")
	_ = e.PlayCard(s, "B", "S_8", "")
	if err := e.PlayCard(s, "A", "S_9", "S_8"); err != nil {
		t.Fatal(err)
	}
	if !s.TrumpRevealed || s.TrumpDrawnBy != "B" {
		t.Fatalf("revealed=%v by %q", s.TrumpRevealed, s.TrumpDrawnBy)
	}
	if _, ok := s.Player("B").HandCard("S_5"); !ok {
		t.Fatal("B must hold the trump card")
	}
	if s.ActiveTrump() != SuitSpades {
		t.Fatalf("active trump %q", s.ActiveTrump())
	}
	view := s.Sanitized("A")
	if view.TrumpCard == nil || view.TrumpCard.ID != "S_5" || view.TrumpDrawnBy != "B" || view.TrumpSuit != SuitSpades {
		t.Fatalf("the drawn trump is public knowledge: %+v", view)
	}
	if n := countCards(t, s); n != DeckSize {
		t.Fatalf("cards %d", n)
	}
}

func TestHiddenTrumpIsSanitized(t *testing.T) {
	_, s := newPlaying(t, "A", "B")
	s.Deck = []Card{} // main deck empty, trump still face down
	view := s.Sanitized("A")
	if view.TrumpCard != nil || view.TrumpSuit != "" || view.TrumpDrawnBy != "" {
		t.Fatalf("hidden trump leaked: %+v", view)
	}
}

func TestUnreachedStumpCarriesOverAndGrows(t *testing.T) {
	e, s := newPlaying(t, "A", "B")
	setRoles(s, "A", "B")
	setHand(t, s, "A", "H_7", "D_9")
	setHand(t, s, "B", "H_10")
	setStump(t, s, "A", "C_2", "C_3")
	setStump(t, s, "B")
	setTrump(t, s, "S_5", true)
	s.TrumpDrawnBy = "B"
	s.Deck = []Card{}
	s.DiscardCount = DeckSize - countCards(t, s)
	_ = e.PlayCard(s, "A", "H_7", "")
	if err := e.PlayCard(s, "B", "H_10", "H_7"); err != nil {
		t.Fatal(err)
	}
	if s.Status != StatusFinished || s.LoserID != "A" {
		t.Fatalf("status %s loser %q", s.Status, s.LoserID)
	}
	if len(s.Player("A").Stump) != 2 {
		t.Fatal("A never reached the stump")
	}
	// next game
	if err := e.SetReady(s, "A", true); err != nil {
		t.Fatal(err)
	}
	if len(s.Player("A").Stump) != 2 || len(s.Player("B").Stump) != 0 {
		t.Fatalf("lobby must keep the unreached stump: A %d B %d", len(s.Player("A").Stump), len(s.Player("B").Stump))
	}
	if err := e.SetReady(s, "B", true); err != nil {
		t.Fatal(err)
	}
	if s.Status != StatusPlaying {
		t.Fatalf("status %s", s.Status)
	}
	a, b := s.Player("A"), s.Player("B")
	if len(a.Stump) != 3 || len(b.Stump) != StumpSize {
		t.Fatalf("A stump %d (want 3), B stump %d (want 2)", len(a.Stump), len(b.Stump))
	}
	ids := map[string]bool{}
	for _, c := range a.Stump {
		ids[c.ID] = true
	}
	if !ids["C_2"] || !ids["C_3"] {
		t.Fatalf("the old stump cards must stay: %v", a.Stump)
	}
	if len(a.Hand) != HandSize || len(b.Hand) != HandSize {
		t.Fatal("hands dealt")
	}
	if want := DeckSize - 1 - 2*HandSize - 3 - StumpSize; len(s.Deck) != want {
		t.Fatalf("deck %d, want %d", len(s.Deck), want)
	}
	if n := countCards(t, s); n != DeckSize {
		t.Fatalf("cards %d", n)
	}
	found := false
	for _, entry := range s.Log {
		if entry.Type == "stump_grow" && entry.PlayerID == "A" && entry.Text == "3" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing stump_grow log: %+v", s.Log)
	}
}

func TestCarriedStumpsDissolveWhenCardsRunShort(t *testing.T) {
	e := NewEngine(NewRand(9, 9))
	s := NewGameState("r")
	for i := 0; i < MaxPlayers; i++ {
		_ = e.Join(s, string(rune('A'+i)), "", "")
	}
	// six players with 4-card stumps would need 1 + 36 + 6*5 = 67 cards
	deck := NewDeck()
	for i := range s.Players {
		s.Players[i].Stump = append([]Card{}, deck[i*4:i*4+4]...)
	}
	if err := e.Start(s); err != nil {
		t.Fatal(err)
	}
	for _, p := range s.Players {
		if len(p.Stump) != StumpSize || len(p.Hand) != HandSize {
			t.Fatalf("%s: hand %d stump %d", p.ID, len(p.Hand), len(p.Stump))
		}
	}
	if n := countCards(t, s); n != DeckSize {
		t.Fatalf("cards %d", n)
	}
}
