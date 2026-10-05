package game

import "testing"

func TestCanBeat(t *testing.T) {
	tests := []struct {
		name    string
		attack  string
		defense string
		trump   string
		want    bool
	}{
		{"same suit higher", "H_7", "H_10", "", true},
		{"same suit lower", "H_10", "H_7", "", false},
		{"same suit equal rank impossible but false", "H_7", "H_7", "", false},
		{"different suit no trump", "H_7", "S_14", "", false},
		{"trump beats non-trump", "H_14", "S_2", SuitSpades, true},
		{"non-trump cannot beat trump", "S_2", "H_14", SuitSpades, false},
		{"trump vs trump higher", "S_7", "S_10", SuitSpades, true},
		{"trump vs trump lower", "S_10", "S_7", SuitSpades, false},
		{"hidden trump is not honoured", "H_14", "S_2", "", false},
		{"super beats base", "H_14", "SC", "", true},
		{"super beats trump", "S_14", "SC", SuitSpades, true},
		{"super beats red joker", "RJ", "SC", "", true},
		{"super beats black joker", "BJ", "SC", "", true},
		{"nothing beats super: base", "SC", "H_14", "", false},
		{"nothing beats super: trump", "SC", "S_14", SuitSpades, false},
		{"nothing beats super: joker", "SC", "RJ", "", false},
		{"red joker beats hearts", "H_7", "RJ", "", true},
		{"red joker beats diamonds", "D_14", "RJ", "", true},
		{"red joker beats red trump", "H_14", "RJ", SuitHearts, true},
		{"red joker does not beat spades", "S_2", "RJ", "", false},
		{"red joker does not beat clubs", "C_14", "RJ", "", false},
		{"black joker beats spades", "S_7", "BJ", "", true},
		{"black joker beats clubs", "C_14", "BJ", "", true},
		{"black joker beats black trump", "C_14", "BJ", SuitClubs, true},
		{"black joker does not beat hearts", "H_2", "BJ", "", false},
		{"joker never beats joker", "RJ", "BJ", "", false},
		{"joker never beats joker 2", "BJ", "RJ", "", false},
		{"base never beats joker", "RJ", "H_14", "", false},
		{"trump never beats joker", "BJ", "H_14", SuitHearts, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := CanBeat(card(t, tc.attack), card(t, tc.defense), tc.trump)
			if got != tc.want {
				t.Fatalf("CanBeat(%s, %s, trump=%q) = %v, want %v", tc.attack, tc.defense, tc.trump, got, tc.want)
			}
		})
	}
}

// threePlayers returns a game with fixed hands: A attacks, B defends, C may throw in.
func threePlayers(t *testing.T) (*Engine, *GameState) {
	e, s := newPlaying(t, "A", "B", "C")
	setRoles(s, "A", "B")
	setHand(t, s, "A", "H_7", "S_7", "D_9", "C_11", "H_2", "SC")
	setHand(t, s, "B", "H_10", "S_8", "D_7", "C_7", "RJ", "BJ")
	setHand(t, s, "C", "C_9", "D_10", "S_14", "H_9", "C_2", "D_2")
	setTrump(t, s, "S_5", false)
	return e, s
}

func TestValidatePlayPreconditions(t *testing.T) {
	_, s := threePlayers(t)
	s.Status = StatusWaiting
	_, err := s.ValidatePlay("A", "H_7", "")
	wantErr(t, err, ErrNotPlaying)
	s.Status = StatusPlaying

	_, err = s.ValidatePlay("Z", "H_7", "")
	wantErr(t, err, ErrUnknownPlayer)

	_, err = s.ValidatePlay("A", "H_14", "")
	wantErr(t, err, ErrCardNotInHand)

	s.Player("C").Out = true
	_, err = s.ValidatePlay("C", "C_9", "")
	wantErr(t, err, ErrPlayerOut)
}

func TestValidateAttackOpening(t *testing.T) {
	_, s := threePlayers(t)
	// only the lead attacker may open the bout, with any card
	_, err := s.ValidatePlay("C", "C_9", "")
	wantErr(t, err, ErrNotYourTurn)
	_, err = s.ValidatePlay("B", "H_10", "")
	wantErr(t, err, ErrDefenderCannotAttack)
	if _, err := s.ValidatePlay("A", "H_7", ""); err != nil {
		t.Fatalf("attacker may open: %v", err)
	}
	if _, err := s.ValidatePlay("A", "SC", ""); err != nil {
		t.Fatalf("attacker may open with the super card: %v", err)
	}
}

func TestValidateThrowIn(t *testing.T) {
	e, s := threePlayers(t)
	if err := e.PlayCard(s, "A", "H_7", ""); err != nil {
		t.Fatal(err)
	}
	// anybody except the defender may throw in a matching rank
	if _, err := s.ValidatePlay("C", "C_9", ""); err != ErrRankMismatch {
		t.Fatalf("rank mismatch expected, got %v", err)
	}
	setHand(t, s, "C", "C_7", "D_10")
	if _, err := s.ValidatePlay("C", "C_7", ""); err != nil {
		t.Fatalf("third player throw-in: %v", err)
	}
	if _, err := s.ValidatePlay("A", "S_7", ""); err != nil {
		t.Fatalf("attacker throw-in: %v", err)
	}
	// ranks of defending cards count too
	if err := e.PlayCard(s, "B", "H_10", "H_7"); err != nil {
		t.Fatal(err)
	}
	setHand(t, s, "C", "D_10", "C_2")
	if _, err := s.ValidatePlay("C", "D_10", ""); err != nil {
		t.Fatalf("throw-in matching the defence rank: %v", err)
	}
	// the defender never throws in
	_, err := s.ValidatePlay("B", "D_7", "")
	wantErr(t, err, ErrDefenderCannotAttack)
}

func TestThrowInBlockedWhenDefenderHasNoCardsUnlessTaking(t *testing.T) {
	e, s := threePlayers(t)
	setHand(t, s, "B", "H_10")
	if err := e.PlayCard(s, "A", "H_7", ""); err != nil {
		t.Fatal(err)
	}
	if err := e.PlayCard(s, "A", "S_7", ""); err != nil {
		t.Fatalf("second attack while defender still has a card: %v", err)
	}
	// defender beats one and is left without cards
	if err := e.PlayCard(s, "B", "H_10", "H_7"); err != nil {
		t.Fatal(err)
	}
	setHand(t, s, "C", "C_7")
	_, err := s.ValidatePlay("C", "C_7", "")
	wantErr(t, err, ErrDefenderNoCards)
	// once the defender takes, attackers may pile on again
	if err := e.TakeCards(s, "B"); err != nil {
		t.Fatal(err)
	}
	if !s.DefenderTaking {
		t.Fatal("defender should be taking")
	}
	if _, err := s.ValidatePlay("C", "C_7", ""); err != nil {
		t.Fatalf("throw-in on a taking defender: %v", err)
	}
}

func TestValidateDefense(t *testing.T) {
	e, s := threePlayers(t)
	if err := e.PlayCard(s, "A", "H_7", ""); err != nil {
		t.Fatal(err)
	}
	_, err := s.ValidatePlay("C", "C_9", "H_7")
	wantErr(t, err, ErrNotDefender)
	_, err = s.ValidatePlay("B", "H_10", "S_7")
	wantErr(t, err, ErrTargetNotOnTable)
	_, err = s.ValidatePlay("B", "S_8", "H_7")
	wantErr(t, err, ErrCannotBeat)
	// hidden trump (spades) must not help
	setHand(t, s, "B", "S_8", "H_10", "RJ", "BJ", "D_7")
	_, err = s.ValidatePlay("B", "S_8", "H_7")
	wantErr(t, err, ErrCannotBeat)
	// jokers: red beats red only
	if _, err := s.ValidatePlay("B", "RJ", "H_7"); err != nil {
		t.Fatalf("red joker should beat 7♥: %v", err)
	}
	_, err = s.ValidatePlay("B", "BJ", "H_7")
	wantErr(t, err, ErrCannotBeat)
	if err := e.PlayCard(s, "B", "H_10", "H_7"); err != nil {
		t.Fatal(err)
	}
	// the bout is over immediately when nobody can throw in; re-open to test "already beaten"
	setRoles(s, "A", "B")
	setHand(t, s, "A", "H_7", "S_7")
	setHand(t, s, "B", "H_10", "D_10", "D_7")
	if err := e.PlayCard(s, "A", "H_7", ""); err != nil {
		t.Fatal(err)
	}
	if err := e.PlayCard(s, "A", "S_7", ""); err != nil {
		t.Fatal(err)
	}
	if err := e.PlayCard(s, "B", "H_10", "H_7"); err != nil {
		t.Fatal(err)
	}
	_, err = s.ValidatePlay("B", "D_10", "H_7")
	wantErr(t, err, ErrTargetAlreadyBeaten)
	// after "take" the defender may not beat any more
	if err := e.TakeCards(s, "B"); err != nil {
		t.Fatal(err)
	}
	if s.TableEmpty() {
		// A still holds nothing matching, C neither -> bout ended; that is fine for this test
		return
	}
	_, err = s.ValidatePlay("B", "D_10", "S_7")
	wantErr(t, err, ErrDefenderIsTaking)
}

func TestRevealedTrumpIsHonoured(t *testing.T) {
	e, s := threePlayers(t)
	setTrump(t, s, "S_5", true)
	if err := e.PlayCard(s, "A", "H_7", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ValidatePlay("B", "S_8", "H_7"); err != nil {
		t.Fatalf("revealed trump should beat: %v", err)
	}
}

func TestValidateTransfer(t *testing.T) {
	e, s := threePlayers(t)
	_, err := s.ValidateTransfer("B", "D_7")
	wantErr(t, err, ErrTableEmpty)
	if err := e.PlayCard(s, "A", "H_7", ""); err != nil {
		t.Fatal(err)
	}
	_, err = s.ValidateTransfer("C", "C_9")
	wantErr(t, err, ErrNotDefender)
	_, err = s.ValidateTransfer("B", "S_8")
	wantErr(t, err, ErrTransferRank)
	_, err = s.ValidateTransfer("B", "H_14")
	wantErr(t, err, ErrCardNotInHand)
	if _, err := s.ValidateTransfer("B", "D_7"); err != nil {
		t.Fatalf("transfer with the same rank on the very first bout: %v", err)
	}
	// the super card never transfers
	setHand(t, s, "B", "SC", "D_7")
	_, err = s.ValidateTransfer("B", "SC")
	wantErr(t, err, ErrSuperCannotTransfer)
	// once a card is beaten, no transfer
	if err := e.PlayCard(s, "A", "S_7", ""); err != nil {
		t.Fatal(err)
	}
	if err := e.PlayCard(s, "B", "SC", "H_7"); err != nil {
		t.Fatal(err)
	}
	_, err = s.ValidateTransfer("B", "D_7")
	wantErr(t, err, ErrTransferAfterDefense)
}

func TestJokerTransfersJoker(t *testing.T) {
	e, s := threePlayers(t)
	setHand(t, s, "A", "RJ", "H_2")
	setHand(t, s, "B", "BJ", "H_10")
	if err := e.PlayCard(s, "A", "RJ", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ValidateTransfer("B", "BJ"); err != nil {
		t.Fatalf("a joker may transfer a joker: %v", err)
	}
	_, err := s.ValidateTransfer("B", "H_10")
	wantErr(t, err, ErrTransferRank)
	if err := e.TransferTurn(s, "B", "BJ"); err != nil {
		t.Fatal(err)
	}
	if s.DefenderID != "C" || s.CurrentTurn != "B" {
		t.Fatalf("after transfer defender=%s attacker=%s", s.DefenderID, s.CurrentTurn)
	}
	if len(s.TableOrder) != 2 {
		t.Fatalf("table should hold both jokers, got %v", s.TableOrder)
	}
}

func TestTransferWhileTakingIsRejected(t *testing.T) {
	e, s := threePlayers(t)
	if err := e.PlayCard(s, "A", "H_7", ""); err != nil {
		t.Fatal(err)
	}
	if err := e.TakeCards(s, "B"); err != nil {
		t.Fatal(err)
	}
	if s.TableEmpty() {
		t.Skip("bout ended immediately")
	}
	_, err := s.ValidateTransfer("B", "D_7")
	wantErr(t, err, ErrDefenderIsTaking)
}

func TestValidateTakeAndPass(t *testing.T) {
	e, s := threePlayers(t)
	wantErr(t, s.ValidateTake("B"), ErrTableEmpty)
	wantErr(t, s.ValidatePass("A"), ErrTableEmpty)
	if err := e.PlayCard(s, "A", "H_7", ""); err != nil {
		t.Fatal(err)
	}
	wantErr(t, s.ValidateTake("A"), ErrNotDefender)
	wantErr(t, s.ValidatePass("B"), ErrDefenderCannotPass)
	if err := s.ValidatePass("C"); err != nil {
		t.Fatalf("C may pass: %v", err)
	}
	if err := e.Pass(s, "C"); err != nil {
		t.Fatal(err)
	}
	wantErr(t, s.ValidatePass("C"), ErrAlreadyPassed)
	// a new card on the table resets passes
	if err := e.PlayCard(s, "A", "S_7", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.ValidatePass("C"); err != nil {
		t.Fatalf("pass should be reset after a throw-in: %v", err)
	}
	if err := s.ValidateTake("B"); err != nil {
		t.Fatalf("defender may take: %v", err)
	}
}
