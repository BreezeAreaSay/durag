package game

import (
	"strings"
	"testing"
)

func TestStartDealsHandsStumpsAndHiddenTrump(t *testing.T) {
	_, s := newPlaying(t, "A", "B", "C")
	if s.Status != StatusPlaying {
		t.Fatalf("status %s", s.Status)
	}
	for _, p := range s.Players {
		if len(p.Hand) != HandSize || len(p.Stump) != StumpSize {
			t.Fatalf("%s: hand %d stump %d", p.ID, len(p.Hand), len(p.Stump))
		}
	}
	if s.TrumpCard == nil || s.TrumpCard.IsSpecial() || s.TrumpSuit != s.TrumpCard.Suit || s.TrumpRevealed {
		t.Fatalf("bad trump %+v revealed=%v", s.TrumpCard, s.TrumpRevealed)
	}
	wantDeck := DeckSize - 1 - 3*(HandSize+StumpSize)
	if len(s.Deck) != wantDeck {
		t.Fatalf("deck %d, want %d", len(s.Deck), wantDeck)
	}
	if n := countCards(t, s); n != DeckSize {
		t.Fatalf("cards in play %d, want %d", n, DeckSize)
	}
	if s.CurrentTurn == "" || s.DefenderID == "" || s.CurrentTurn == s.DefenderID {
		t.Fatalf("roles attacker=%s defender=%s", s.CurrentTurn, s.DefenderID)
	}
	if s.DefenderID != s.NextActive(s.CurrentTurn) {
		t.Fatal("defender must sit after the attacker")
	}
}

func TestStartRequiresTwoPlayers(t *testing.T) {
	e := NewEngine(NewRand(1, 1))
	s := NewGameState("r")
	_ = e.Join(s, "A", "A", "")
	wantErr(t, e.Start(s), ErrNotEnoughPlayers)
}

func TestFirstAttackerFollowsSpadeOrder(t *testing.T) {
	e := NewEngine(NewRand(1, 1))
	s := NewGameState("r")
	for _, id := range []string{"A", "B", "C"} {
		_ = e.Join(s, id, id, "")
	}
	setHand(t, s, "A", "S_3", "H_2")
	setHand(t, s, "B", "C_2", "H_3")
	setHand(t, s, "C", "S_2", "H_4")
	if got := e.firstAttacker(s); got != "C" {
		t.Fatalf("2♠ holder should start, got %s", got)
	}
	setHand(t, s, "C", "H_4")
	if got := e.firstAttacker(s); got != "A" {
		t.Fatalf("3♠ holder should start, got %s", got)
	}
	setHand(t, s, "A", "H_5")
	if got := e.firstAttacker(s); got != "B" {
		t.Fatalf("2♣ holder should start, got %s", got)
	}
	setHand(t, s, "B", "H_6")
	got := e.firstAttacker(s)
	if s.Player(got) == nil {
		t.Fatalf("fallback must pick a seated player, got %q", got)
	}
}

func TestJoinRulesAndReadyAutoStart(t *testing.T) {
	e := NewEngine(NewRand(1, 1))
	s := NewGameState("r")
	if err := e.Join(s, "A", "Alice", ""); err != nil {
		t.Fatal(err)
	}
	if s.HostID != "A" {
		t.Fatal("first player becomes host")
	}
	if err := e.Join(s, "B", "Bob", ""); err != nil {
		t.Fatal(err)
	}
	if err := e.SetReady(s, "A", true); err != nil {
		t.Fatal(err)
	}
	if s.Status != StatusWaiting {
		t.Fatal("should wait for everybody")
	}
	if err := e.SetReady(s, "B", true); err != nil {
		t.Fatal(err)
	}
	if s.Status != StatusPlaying {
		t.Fatal("all ready -> playing")
	}
	wantErr(t, e.Join(s, "C", "Carol", ""), ErrGameInProgress)
	// a seated player may reconnect
	if err := e.Join(s, "A", "Alice", ""); err != nil {
		t.Fatalf("rejoin: %v", err)
	}
	// full room
	s2 := NewGameState("full")
	for i := 0; i < MaxPlayers; i++ {
		if err := e.Join(s2, string(rune('a'+i)), "", ""); err != nil {
			t.Fatal(err)
		}
	}
	wantErr(t, e.Join(s2, "z", "", ""), ErrRoomFull)
}

func TestDisconnectInLobbyFreesSeatButKeepsItInGame(t *testing.T) {
	e := NewEngine(NewRand(1, 1))
	s := NewGameState("r")
	_ = e.Join(s, "A", "A", "")
	_ = e.Join(s, "B", "B", "")
	_ = e.Disconnect(s, "B")
	if len(s.Players) != 1 {
		t.Fatal("lobby disconnect should free the seat")
	}
	_ = e.Join(s, "B", "B", "")
	_ = e.SetReady(s, "A", true)
	_ = e.SetReady(s, "B", true)
	_ = e.Disconnect(s, "B")
	if len(s.Players) != 2 || s.Player("B").Connected {
		t.Fatal("in-game disconnect should keep the seat and mark it disconnected")
	}
}

func TestBoutBeatenGoesToDiscardAndRotates(t *testing.T) {
	e, s := newPlaying(t, "A", "B")
	setRoles(s, "A", "B")
	setHand(t, s, "A", "H_7", "S_9", "D_9", "C_11", "H_2", "C_3")
	setHand(t, s, "B", "H_10", "S_8", "D_4", "C_5", "H_6", "D_12")
	setTrump(t, s, "S_5", false)
	rebuildDeck(t, s)
	deckBefore := len(s.Deck)
	if err := e.PlayCard(s, "A", "H_7", ""); err != nil {
		t.Fatal(err)
	}
	if err := e.PlayCard(s, "B", "H_10", "H_7"); err != nil {
		t.Fatal(err)
	}
	// A holds nothing of rank 7 or 10 -> auto pass -> bout over
	if !s.TableEmpty() {
		t.Fatalf("table should be cleared, got %v", s.TableOrder)
	}
	if s.DiscardCount != 2 {
		t.Fatalf("discard %d", s.DiscardCount)
	}
	if len(s.Player("A").Hand) != HandSize || len(s.Player("B").Hand) != HandSize {
		t.Fatal("hands must be refilled to 6")
	}
	if len(s.Deck) != deckBefore-2 {
		t.Fatalf("deck %d, want %d", len(s.Deck), deckBefore-2)
	}
	if s.CurrentTurn != "B" || s.DefenderID != "A" {
		t.Fatalf("successful defender attacks next: attacker=%s defender=%s", s.CurrentTurn, s.DefenderID)
	}
	if s.BoutNumber != 2 {
		t.Fatalf("bout number %d", s.BoutNumber)
	}
	if n := countCards(t, s); n != DeckSize {
		t.Fatalf("cards %d", n)
	}
}

func TestExplicitPassEndsBoutWhenThrowInWasPossible(t *testing.T) {
	e, s := newPlaying(t, "A", "B")
	setRoles(s, "A", "B")
	setHand(t, s, "A", "H_7", "S_7", "D_9", "C_11", "H_2", "C_3")
	setHand(t, s, "B", "H_10", "S_8", "D_4", "C_5", "H_6", "D_12")
	setTrump(t, s, "S_5", false)
	_ = e.PlayCard(s, "A", "H_7", "")
	_ = e.PlayCard(s, "B", "H_10", "H_7")
	if s.TableEmpty() {
		t.Fatal("A still holds 7♠, the bout must stay open")
	}
	if err := e.Pass(s, "A"); err != nil {
		t.Fatal(err)
	}
	if !s.TableEmpty() || s.DiscardCount != 2 {
		t.Fatal("pass should end the bout")
	}
}

func TestTakeGivesWholeTableAndSkipsDefender(t *testing.T) {
	e, s := newPlaying(t, "A", "B", "C")
	setRoles(s, "A", "B")
	setHand(t, s, "A", "H_7", "S_7", "D_9", "C_11", "H_2", "C_3")
	setHand(t, s, "B", "H_3", "S_8", "D_4", "C_5", "H_6", "D_12")
	setHand(t, s, "C", "C_7", "D_7", "S_14", "H_9", "C_2", "D_2")
	setTrump(t, s, "S_5", false)
	rebuildDeck(t, s)
	_ = e.PlayCard(s, "A", "H_7", "")
	_ = e.PlayCard(s, "C", "C_7", "")
	if err := e.TakeCards(s, "B"); err != nil {
		t.Fatal(err)
	}
	if s.TableEmpty() {
		t.Fatal("attackers with matching cards may still throw in after 'take'")
	}
	// unlimited pile-on: more cards than the defender could ever beat
	if err := e.PlayCard(s, "A", "S_7", ""); err != nil {
		t.Fatal(err)
	}
	if err := e.PlayCard(s, "C", "D_7", ""); err != nil {
		t.Fatal(err)
	}
	if !s.TableEmpty() {
		t.Fatalf("nobody can add more -> bout ends, table %v", s.TableOrder)
	}
	if got := len(s.Player("B").Hand); got != 6+4 {
		t.Fatalf("defender should hold 10 cards, has %d", got)
	}
	if s.CurrentTurn != "C" || s.DefenderID != "A" {
		t.Fatalf("after a take the defender skips: attacker=%s defender=%s", s.CurrentTurn, s.DefenderID)
	}
	if n := countCards(t, s); n != DeckSize {
		t.Fatalf("cards %d", n)
	}
}

func TestThrowInsNeverExceedDefenderHand(t *testing.T) {
	// No six-card cap, but the defender must hold a card for every attack
	// still waiting: with two cards in hand the table takes two attacks.
	e, s := newPlaying(t, "A", "B", "C")
	setRoles(s, "A", "B")
	setHand(t, s, "A", "H_7", "S_7", "D_7", "C_7")
	setHand(t, s, "B", "H_10", "S_8")
	setHand(t, s, "C", "C_10", "D_10", "S_10")
	setTrump(t, s, "C_5", false)
	for _, id := range []string{"H_7", "S_7"} {
		if err := e.PlayCard(s, "A", id, ""); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
	}
	wantErr(t, e.PlayCard(s, "A", "D_7", ""), ErrTooManyAttacks)
	if len(s.TableOrder) != 2 {
		t.Fatalf("two attacks expected, got %d", len(s.TableOrder))
	}
	// beating one frees nothing: one card left, one attack waiting
	if err := e.PlayCard(s, "B", "H_10", "H_7"); err != nil {
		t.Fatal(err)
	}
	wantErr(t, e.PlayCard(s, "C", "C_10", ""), ErrTooManyAttacks)
	// a defender with a bigger hand can be piled on, no cap at six
	e2, s2 := newPlaying(t, "A", "B", "C")
	setRoles(s2, "A", "B")
	setHand(t, s2, "A", "H_7", "S_7", "D_7", "C_7")
	setHand(t, s2, "B", "H_10", "S_8", "D_4", "C_5", "H_6", "D_12", "C_13", "S_2")
	setHand(t, s2, "C", "C_10", "D_10", "S_10")
	setTrump(t, s2, "C_5", false)
	for _, id := range []string{"H_7", "S_7", "D_7", "C_7"} {
		if err := e2.PlayCard(s2, "A", id, ""); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
	}
	if err := e2.PlayCard(s2, "B", "H_10", "H_7"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"C_10", "D_10", "S_10"} {
		if err := e2.PlayCard(s2, "C", id, ""); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
	}
	if len(s2.TableOrder) != 7 {
		t.Fatalf("seven attacks expected, got %d", len(s2.TableOrder))
	}
}

func TestTransferMovesDefenceToNextPlayer(t *testing.T) {
	e, s := newPlaying(t, "A", "B", "C")
	setRoles(s, "A", "B")
	setHand(t, s, "A", "H_7", "S_9", "D_9", "C_11", "H_2", "C_3")
	setHand(t, s, "B", "S_7", "S_8", "D_4", "C_5", "H_6", "D_12")
	setHand(t, s, "C", "C_9", "D_10", "H_14", "H_9", "C_2", "D_7")
	setTrump(t, s, "C_6", false)
	rebuildDeck(t, s)
	_ = e.PlayCard(s, "A", "H_7", "")
	if err := e.TransferTurn(s, "B", "S_7"); err != nil {
		t.Fatal(err)
	}
	if s.DefenderID != "C" || s.CurrentTurn != "B" {
		t.Fatalf("defender=%s attacker=%s", s.DefenderID, s.CurrentTurn)
	}
	if s.TransferCount != 1 || len(s.TableOrder) != 2 {
		t.Fatal("transfer card must join the table")
	}
	// C can transfer again (nothing beaten yet) -> back to A
	if err := e.TransferTurn(s, "C", "D_7"); err != nil {
		t.Fatal(err)
	}
	if s.DefenderID != "A" || s.CurrentTurn != "C" {
		t.Fatalf("defender=%s attacker=%s", s.DefenderID, s.CurrentTurn)
	}
	// A beats everything? A has nothing suitable -> takes; takes all three 7s
	if err := e.TakeCards(s, "A"); err != nil {
		t.Fatal(err)
	}
	if !s.TableEmpty() {
		t.Fatalf("table %v", s.TableOrder)
	}
	if got := len(s.Player("A").Hand); got != 5+3 {
		t.Fatalf("A hand %d", got)
	}
	if s.CurrentTurn != "B" || s.DefenderID != "C" {
		t.Fatalf("attacker=%s defender=%s", s.CurrentTurn, s.DefenderID)
	}
}

func TestSuperCardBeatsEverythingAndIsUnbeatable(t *testing.T) {
	e, s := newPlaying(t, "A", "B")
	setRoles(s, "A", "B")
	setHand(t, s, "A", "RJ", "H_14", "S_3")
	setHand(t, s, "B", "SC", "H_2", "C_4")
	setTrump(t, s, "C_6", true)
	_ = e.PlayCard(s, "A", "RJ", "")
	if err := e.PlayCard(s, "B", "SC", "RJ"); err != nil {
		t.Fatalf("super beats joker: %v", err)
	}
	// next bout: B leads with nothing special; then A attacks with SC later
	setRoles(s, "A", "B")
	setHand(t, s, "A", "SC", "S_3")
	setHand(t, s, "B", "C_14", "H_2", "RJ")
	_ = e.PlayCard(s, "A", "SC", "")
	for _, id := range []string{"C_14", "H_2", "RJ"} {
		if _, err := s.ValidatePlay("B", id, "SC"); err != ErrCannotBeat {
			t.Fatalf("%s should not beat the super card, got %v", id, err)
		}
	}
	if _, err := s.ValidateTransfer("B", "RJ"); err != ErrTransferRank {
		t.Fatalf("got %v", err)
	}
}

func TestDeckExhaustionRevealsTrumpThenStumpsThenOut(t *testing.T) {
	e, s := newPlaying(t, "A", "B")
	setRoles(s, "A", "B")
	setHand(t, s, "A", "H_7")
	setHand(t, s, "B", "H_10")
	setStump(t, s, "A", "C_2", "C_3")
	setStump(t, s, "B", "D_2", "D_3")
	setTrump(t, s, "S_5", false)
	s.Deck = cards(t, "S_9") // one card left in the main deck
	s.DiscardCount = DeckSize - countCards(t, s) + s.DiscardCount

	_ = e.PlayCard(s, "A", "H_7", "")
	if err := e.PlayCard(s, "B", "H_10", "H_7"); err != nil {
		t.Fatal(err)
	}
	// bout over: A (attacker) draws S_9 -> deck empty -> A still needs cards
	// and draws the trump itself, which reveals it to everybody; B gets
	// nothing and picks up the stump.
	if !s.TrumpRevealed || s.TrumpDrawnBy != "A" {
		t.Fatalf("trump must be revealed by the player who drew it: revealed=%v by %q", s.TrumpRevealed, s.TrumpDrawnBy)
	}
	if s.TrumpCard == nil || s.TrumpCard.ID != "S_5" || s.TrumpInDeck() {
		t.Fatalf("trump card stays public after the draw: %+v inDeck=%v", s.TrumpCard, s.TrumpInDeck())
	}
	if _, ok := s.Player("A").HandCard("S_5"); !ok {
		t.Fatal("A must hold the trump card")
	}
	if last := s.Log[len(s.Log)-2]; last.Type != "trump_revealed" && s.Log[len(s.Log)-1].Type != "trump_revealed" {
		// the reveal is logged (position depends on the stump pickup that follows)
		found := false
		for _, e := range s.Log {
			if e.Type == "trump_revealed" && e.PlayerID == "A" && e.CardID == "S_5" {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing trump_revealed log entry: %+v", s.Log)
		}
	}
	a, b := s.Player("A"), s.Player("B")
	if len(a.Hand) != 2 || len(a.Stump) != 2 {
		t.Fatalf("A hand %d stump %d", len(a.Hand), len(a.Stump))
	}
	if len(b.Hand) != 2 || len(b.Stump) != 0 {
		t.Fatalf("B should have picked up the stump: hand %d stump %d", len(b.Hand), len(b.Stump))
	}
	if s.ActiveTrump() != SuitSpades {
		t.Fatalf("active trump %q", s.ActiveTrump())
	}
	if n := countCards(t, s); n != DeckSize {
		t.Fatalf("cards %d", n)
	}
	if s.Status != StatusPlaying {
		t.Fatal("game continues")
	}
}

func TestGameFinishesWithLoserAndDraw(t *testing.T) {
	// loser case: B keeps a card
	e, s := newPlaying(t, "A", "B")
	setRoles(s, "A", "B")
	setHand(t, s, "A", "H_7")
	setHand(t, s, "B", "H_10", "S_3")
	setStump(t, s, "A")
	setStump(t, s, "B")
	s.Deck = []Card{}
	s.TrumpCard = nil
	s.TrumpRevealed = true
	s.TrumpSuit = SuitSpades
	s.DiscardCount = DeckSize - 4
	_ = e.PlayCard(s, "A", "H_7", "")
	if err := e.PlayCard(s, "B", "H_10", "H_7"); err != nil {
		t.Fatal(err)
	}
	if s.Status != StatusFinished || s.LoserID != "B" {
		t.Fatalf("status %s loser %q", s.Status, s.LoserID)
	}
	if len(s.FinishedOrder) != 1 || s.FinishedOrder[0] != "A" {
		t.Fatalf("finished order %v", s.FinishedOrder)
	}
	if s.Player("A").IsReady || s.Player("B").IsReady {
		t.Fatal("readiness resets after a game")
	}
	// draw case: both run out together
	e, s = newPlaying(t, "A", "B")
	setRoles(s, "A", "B")
	setHand(t, s, "A", "H_7")
	setHand(t, s, "B", "H_10")
	setStump(t, s, "A")
	setStump(t, s, "B")
	s.Deck = []Card{}
	s.TrumpCard = nil
	s.TrumpRevealed = true
	s.DiscardCount = DeckSize - 2
	_ = e.PlayCard(s, "A", "H_7", "")
	_ = e.PlayCard(s, "B", "H_10", "H_7")
	if s.Status != StatusFinished || s.LoserID != "" {
		t.Fatalf("status %s loser %q (want draw)", s.Status, s.LoserID)
	}
	// ready in a finished room resets it to the lobby
	if err := e.SetReady(s, "A", true); err != nil {
		t.Fatal(err)
	}
	if s.Status != StatusWaiting || len(s.Players[0].Hand) != 0 || s.LoserID != "" {
		t.Fatal("ready after finish should reset the room")
	}
}

func TestDefenderWithoutCardsEndsBoutAfterBeatingEverything(t *testing.T) {
	e, s := newPlaying(t, "A", "B", "C")
	setRoles(s, "A", "B")
	setHand(t, s, "A", "H_7", "H_9", "S_7")
	setHand(t, s, "B", "H_10")
	setHand(t, s, "C", "C_7", "D_7")
	setTrump(t, s, "S_5", false)
	_ = e.PlayCard(s, "A", "H_7", "")
	if err := e.PlayCard(s, "B", "H_10", "H_7"); err != nil {
		t.Fatal(err)
	}
	if !s.TableEmpty() {
		t.Fatal("defender has no cards left: nothing more can be thrown, bout ends")
	}
	if len(s.Player("B").Hand) != HandSize {
		t.Fatal("defender refilled")
	}
}

func TestLeaveDuringGameForfeits(t *testing.T) {
	e, s := newPlaying(t, "A", "B", "C")
	setRoles(s, "A", "B")
	_ = e.PlayCard(s, "A", s.Player("A").Hand[0].ID, "")
	if err := e.Leave(s, "B"); err != nil {
		t.Fatal(err)
	}
	if s.PlayerIndex("B") >= 0 {
		t.Fatal("leaver removed")
	}
	if s.Status != StatusPlaying || !s.TableEmpty() {
		t.Fatal("bout aborted, game continues with two players")
	}
	if s.CurrentTurn != "A" || s.DefenderID != "C" {
		t.Fatalf("attacker=%s defender=%s", s.CurrentTurn, s.DefenderID)
	}
	if err := e.Leave(s, "C"); err != nil {
		t.Fatal(err)
	}
	if s.Status != StatusFinished || s.LoserID != "C" {
		t.Fatalf("last leaver loses: status=%s loser=%s", s.Status, s.LoserID)
	}
}

func TestSanitizedHidesPrivateInformation(t *testing.T) {
	_, s := newPlaying(t, "A", "B", "C")
	view := s.Sanitized("A")
	if len(view.Deck) != 0 || view.DeckCount != len(s.Deck) {
		t.Fatal("deck must be replaced by a count")
	}
	if view.TrumpCard != nil || view.TrumpSuit != "" {
		t.Fatal("hidden trump leaked")
	}
	if view.ViewerID != "A" {
		t.Fatal("viewer id")
	}
	for _, p := range view.Players {
		if len(p.Stump) != 0 || p.StumpCount != StumpSize {
			t.Fatalf("%s stump leaked", p.ID)
		}
		if p.ID == "A" {
			if len(p.Hand) != HandSize || p.HandCount != HandSize {
				t.Fatal("own hand must be visible")
			}
			continue
		}
		if len(p.Hand) != 0 || p.HandCount != HandSize {
			t.Fatalf("%s hand leaked", p.ID)
		}
	}
	js := mustJSON(t, view)
	for _, p := range s.Players {
		if p.ID == "A" {
			continue
		}
		for _, c := range p.Hand {
			if strings.Contains(js, `"`+c.ID+`"`) {
				t.Fatalf("card %s of %s found in A's view", c.ID, p.ID)
			}
		}
	}
	for _, c := range s.Deck {
		if strings.Contains(js, `"`+c.ID+`"`) {
			t.Fatalf("deck card %s found in view", c.ID)
		}
	}
	if strings.Contains(js, `"`+s.TrumpCard.ID+`"`) {
		t.Fatal("trump card id found in view")
	}
	// the original is untouched
	if len(s.Deck) == 0 || s.TrumpCard == nil {
		t.Fatal("sanitize must not mutate the source")
	}
	// revealed trump is public
	s.TrumpRevealed = true
	view = s.Sanitized("B")
	if view.TrumpCard == nil || view.TrumpSuit != s.TrumpSuit {
		t.Fatal("revealed trump must be visible")
	}
}

// --- random play-outs: the engine must never dead-lock or lose cards -----

type move struct {
	kind, player, card, target string
}

func legalMoves(s *GameState) []move {
	var ms []move
	for _, p := range s.Players {
		if p.Out {
			continue
		}
		for _, c := range p.Hand {
			if _, err := s.ValidatePlay(p.ID, c.ID, ""); err == nil {
				ms = append(ms, move{"play", p.ID, c.ID, ""})
			}
			for _, aid := range s.TableOrder {
				if _, err := s.ValidatePlay(p.ID, c.ID, aid); err == nil {
					ms = append(ms, move{"play", p.ID, c.ID, aid})
				}
			}
			if _, err := s.ValidateTransfer(p.ID, c.ID); err == nil {
				ms = append(ms, move{"transfer", p.ID, c.ID, ""})
			}
		}
		if err := s.ValidateTake(p.ID); err == nil {
			ms = append(ms, move{"take", p.ID, "", ""})
		}
		if err := s.ValidatePass(p.ID); err == nil {
			ms = append(ms, move{"pass", p.ID, "", ""})
		}
	}
	return ms
}

func TestRandomPlayoutsTerminateAndConserveCards(t *testing.T) {
	for seed := uint64(1); seed <= 40; seed++ {
		rng := NewRand(seed, seed*7)
		e := NewEngine(rng)
		s := NewGameState("r")
		n := 2 + int(seed%5) // 2..6 players
		for i := 0; i < n; i++ {
			if err := e.Join(s, string(rune('A'+i)), "", ""); err != nil {
				t.Fatal(err)
			}
		}
		if err := e.Start(s); err != nil {
			t.Fatal(err)
		}
		for step := 0; step < 20000; step++ {
			if s.Status == StatusFinished {
				break
			}
			if n := countCards(t, s); n != DeckSize {
				t.Fatalf("seed %d step %d: %d cards", seed, step, n)
			}
			for _, id := range s.ActivePlayers() {
				if len(s.Player(id).Hand) == 0 && s.TableEmpty() {
					t.Fatalf("seed %d step %d: active player %s has no cards at bout start", seed, step, id)
				}
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
				t.Fatalf("seed %d: legal move %+v rejected: %v", seed, m, err)
			}
		}
		if s.Status != StatusFinished {
			t.Fatalf("seed %d: game did not finish", seed)
		}
		if n := countCards(t, s); n != DeckSize {
			t.Fatalf("seed %d: %d cards at the end", seed, n)
		}
		active := s.ActivePlayers()
		if len(active) > 1 {
			t.Fatalf("seed %d: %d active players after finish", seed, len(active))
		}
		if (len(active) == 1 && s.LoserID != active[0]) || (len(active) == 0 && s.LoserID != "") {
			t.Fatalf("seed %d: loser %q active %v", seed, s.LoserID, active)
		}
		if len(s.FinishedOrder)+len(active) != n {
			t.Fatalf("seed %d: finished %v active %v", seed, s.FinishedOrder, active)
		}
	}
}
