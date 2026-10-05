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

func TestThrowerPicksUpStumpImmediatelyWhenDeckIsEmpty(t *testing.T) {
	e, s := newPlaying(t, "A", "B", "C")
	setRoles(s, "A", "B")
	setHand(t, s, "A", "H_7", "S_9")
	setHand(t, s, "B", "H_10", "S_8", "D_4")
	setHand(t, s, "C", "C_7")
	setStump(t, s, "C", "D_7", "S_14")
	setStump(t, s, "A", "C_2", "C_3")
	setStump(t, s, "B", "D_2", "D_3")
	setTrump(t, s, "S_5", true)
	s.Deck = []Card{}
	s.TrumpCard = nil
	s.DiscardCount = DeckSize - countCards(t, s)
	_ = e.PlayCard(s, "A", "H_7", "")
	if err := e.PlayCard(s, "C", "C_7", ""); err != nil {
		t.Fatal(err)
	}
	c := s.Player("C")
	if len(c.Hand) != 2 || len(c.Stump) != 0 || c.Out {
		t.Fatalf("C should hold the stump right away: hand %v stump %v out %v", c.Hand, c.Stump, c.Out)
	}
	// ...and may keep playing with it in the same bout
	if err := e.PlayCard(s, "C", "D_7", ""); err != nil {
		t.Fatalf("throw in from the stump: %v", err)
	}
	if n := countCards(t, s); n != DeckSize {
		t.Fatalf("cards %d", n)
	}
}

func TestDefenderPicksUpStumpAndKeepsDefending(t *testing.T) {
	e, s := newPlaying(t, "A", "B")
	setRoles(s, "A", "B")
	setHand(t, s, "A", "H_7", "S_7", "D_9")
	setHand(t, s, "B", "H_10")
	setStump(t, s, "B", "S_10", "D_2")
	setStump(t, s, "A")
	setTrump(t, s, "C_5", true)
	s.Deck = []Card{}
	s.TrumpCard = nil
	s.DiscardCount = DeckSize - countCards(t, s)
	_ = e.PlayCard(s, "A", "H_7", "")
	_ = e.PlayCard(s, "A", "S_7", "")
	if err := e.PlayCard(s, "B", "H_10", "H_7"); err != nil {
		t.Fatal(err)
	}
	b := s.Player("B")
	if len(b.Hand) != 2 || len(b.Stump) != 0 {
		t.Fatalf("defender should pick up the stump mid-bout: hand %v stump %v", b.Hand, b.Stump)
	}
	if s.TableEmpty() {
		t.Fatal("bout continues: S_7 is still undefended")
	}
	if err := e.PlayCard(s, "B", "S_10", "S_7"); err != nil {
		t.Fatalf("defend with a stump card: %v", err)
	}
	if !s.TableEmpty() || s.Status != StatusPlaying {
		t.Fatalf("bout should be over, game continues: table %v status %s", s.TableOrder, s.Status)
	}
	if len(b.Hand) != 1 || b.Out {
		t.Fatalf("B keeps D_2: %v out %v", b.Hand, b.Out)
	}
}

func TestPlayerWithoutCardsLeavesImmediately(t *testing.T) {
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
	s.TrumpCard = nil
	s.DiscardCount = DeckSize - countCards(t, s)
	if err := e.PlayCard(s, "A", "H_7", ""); err != nil {
		t.Fatal(err)
	}
	a := s.Player("A")
	if !a.Out || len(s.FinishedOrder) != 1 || s.FinishedOrder[0] != "A" {
		t.Fatalf("A must be out the moment the last card leaves the hand: %+v", a)
	}
	if s.Status != StatusPlaying || s.TableEmpty() {
		t.Fatal("the bout goes on without A: B still has to answer")
	}
	// B beats; C may throw in a 10; nobody holds a 7 -> bito
	if err := e.PlayCard(s, "B", "H_10", "H_7"); err != nil {
		t.Fatal(err)
	}
	if err := e.PlayCard(s, "C", "D_10", ""); err != nil {
		t.Fatalf("C throws in: %v", err)
	}
	if err := e.PlayCard(s, "B", "S_8", "D_10"); err != ErrCannotBeat {
		t.Fatalf("expected cannot beat, got %v", err)
	}
	if err := e.TakeCards(s, "B"); err != nil {
		t.Fatal(err)
	}
	if !s.TableEmpty() || s.Status != StatusPlaying {
		t.Fatalf("B took, game continues between B and C: %v %s", s.TableOrder, s.Status)
	}
	if s.CurrentTurn != "C" || s.DefenderID != "B" {
		t.Fatalf("roles %s / %s", s.CurrentTurn, s.DefenderID)
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
	s.TrumpCard = nil
	s.DiscardCount = DeckSize - 2
	_ = e.PlayCard(s, "A", "H_7", "")
	if !s.Player("A").Out {
		t.Fatal("A is out immediately")
	}
	if s.Status != StatusPlaying {
		t.Fatal("the game must wait for B's answer")
	}
	_ = e.PlayCard(s, "B", "H_10", "H_7")
	if s.Status != StatusFinished || s.LoserID != "" {
		t.Fatalf("expected a draw, got status %s loser %q", s.Status, s.LoserID)
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
