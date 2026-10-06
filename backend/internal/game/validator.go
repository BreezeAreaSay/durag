package game

// RuleError is returned for every rejected intent. Code is stable so that the
// client can react programmatically; Message is meant for humans.
type RuleError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *RuleError) Error() string { return e.Message }

func ruleErr(code, msg string) *RuleError { return &RuleError{Code: code, Message: msg} }

// All rule violations.
var (
	ErrNotPlaying           = ruleErr("NOT_PLAYING", "game is not in progress")
	ErrNotWaiting           = ruleErr("NOT_WAITING", "the game is not in the lobby")
	ErrUnknownPlayer        = ruleErr("UNKNOWN_PLAYER", "you are not seated at this table")
	ErrPlayerOut            = ruleErr("PLAYER_OUT", "you have already left the game")
	ErrCardNotInHand        = ruleErr("CARD_NOT_IN_HAND", "you do not hold this card")
	ErrNotYourTurn          = ruleErr("NOT_YOUR_TURN", "only the attacker may open the bout")
	ErrDefenderCannotAttack = ruleErr("DEFENDER_CANNOT_ATTACK", "the defender cannot throw cards in")
	ErrRankMismatch         = ruleErr("RANK_MISMATCH", "only ranks already on the table may be thrown in")
	ErrDefenderNoCards      = ruleErr("DEFENDER_HAS_NO_CARDS", "the defender has no cards left to beat with")
	ErrNotDefender          = ruleErr("NOT_DEFENDER", "only the defender may beat cards")
	ErrDefenderIsTaking     = ruleErr("DEFENDER_TAKING", "the defender already decided to take the cards")
	ErrTargetNotOnTable     = ruleErr("TARGET_NOT_ON_TABLE", "target card is not on the table")
	ErrTargetAlreadyBeaten  = ruleErr("TARGET_ALREADY_BEATEN", "this card is already beaten")
	ErrCannotBeat           = ruleErr("CANNOT_BEAT", "this card does not beat the target")
	ErrTableEmpty           = ruleErr("TABLE_EMPTY", "there is nothing on the table")
	ErrTransferAfterDefense = ruleErr("TRANSFER_AFTER_DEFENSE", "cannot transfer once a card has been beaten")
	ErrSuperCannotTransfer  = ruleErr("SUPER_CANNOT_TRANSFER", "the Super card cannot transfer the turn")
	ErrTransferRank         = ruleErr("TRANSFER_RANK", "a transfer card must match the rank of the attack")
	ErrAlreadyTaking        = ruleErr("ALREADY_TAKING", "you are already taking the cards")
	ErrDefenderCannotPass   = ruleErr("DEFENDER_CANNOT_PASS", "the defender cannot pass: beat, transfer or take")
	ErrAlreadyPassed        = ruleErr("ALREADY_PASSED", "you have already passed")
	ErrGameInProgress       = ruleErr("GAME_IN_PROGRESS", "the game has already started")
	ErrRoomFull             = ruleErr("ROOM_FULL", "the room is full")
	ErrNotEnoughPlayers     = ruleErr("NOT_ENOUGH_PLAYERS", "at least two players are needed")
	ErrResolving            = ruleErr("RESOLVING", "the bout is over, wait for the next one")
	ErrStumpPending         = ruleErr("STUMP_PENDING", "stumps are being taken, wait a moment")
	ErrNoStump              = ruleErr("NO_STUMP", "you have no stump to take right now")
	ErrNotResolving         = ruleErr("NOT_RESOLVING", "no bout is waiting to be resolved")
	ErrTransferTooFewCards  = ruleErr("TRANSFER_TOO_FEW_CARDS", "the next player has too few cards to take over the defence")
)

// AutoPassWhenNoLegalThrowIn makes the engine treat an attacker who holds no
// card that could legally be thrown in as having passed. This keeps the game
// flowing in an asynchronous mobile setting at the cost of a tiny information
// leak (the bout ends instantly when nobody can add a card).
const AutoPassWhenNoLegalThrowIn = true

// CanBeat reports whether the defending card beats the attacking card.
// trumpSuit is the active trump suit or "" while the trump is still hidden
// (or when there is no trump at all).
//
// Rules:
//   - the Super card beats absolutely anything and is beaten by nothing;
//   - the Red Joker beats any red card, the Black Joker any black card;
//     Jokers have no trump suit, so a trump never beats a Joker and a Joker
//     never beats the other Joker;
//   - otherwise: same suit and higher rank, or a trump against a non-trump.
func CanBeat(attack, defense Card, trumpSuit string) bool {
	if defense.IsSuper() {
		return true
	}
	if attack.IsSuper() {
		return false
	}
	if defense.IsJoker() {
		if attack.IsJoker() {
			return false
		}
		if defense.ID == RedJokerID {
			return attack.IsRed()
		}
		return attack.IsBlack()
	}
	if attack.IsJoker() {
		return false
	}
	if attack.Suit == defense.Suit {
		return defense.Rank > attack.Rank
	}
	return trumpSuit != "" && defense.Suit == trumpSuit
}

// seated checks the common preconditions of every in-game intent.
func (s *GameState) seated(playerID string) (*Player, error) {
	if s.Status != StatusPlaying {
		return nil, ErrNotPlaying
	}
	p := s.Player(playerID)
	if p == nil {
		return nil, ErrUnknownPlayer
	}
	if p.Out {
		return nil, ErrPlayerOut
	}
	if s.Phase == PhaseResolving {
		return nil, ErrResolving
	}
	if s.StumpsPending() {
		return nil, ErrStumpPending
	}
	return p, nil
}

// ValidateTakeStump checks a TAKE_STUMP intent: stumps are taken strictly
// between bouts, only by players whose hand ran dry while the deck is empty.
func (s *GameState) ValidateTakeStump(playerID string) error {
	if s.Status != StatusPlaying {
		return ErrNotPlaying
	}
	p := s.Player(playerID)
	if p == nil {
		return ErrUnknownPlayer
	}
	if p.Out {
		return ErrPlayerOut
	}
	if !s.stumpPendingFor(playerID) || len(p.Stump) == 0 {
		return ErrNoStump
	}
	return nil
}

// ThrowInAllowed reports whether attackers may currently add cards to the
// table: the bout must be open, and the defender must either still hold cards
// or have announced that they take (then everything goes to them anyway).
func (s *GameState) ThrowInAllowed() bool {
	if s.TableEmpty() {
		return false
	}
	if s.DefenderTaking {
		return true
	}
	d := s.Defender()
	return d != nil && len(d.Hand) > 0
}

// CanThrowIn reports whether the player holds at least one card that could be
// thrown in right now.
func (s *GameState) CanThrowIn(p *Player) bool {
	if p == nil || p.Out || p.ID == s.DefenderID || !s.ThrowInAllowed() {
		return false
	}
	ranks := s.TableRanks()
	for _, c := range p.Hand {
		if ranks[c.Rank] {
			return true
		}
	}
	return false
}

// ValidatePlay checks a PLAY_CARD intent. With an empty targetID the card is
// an attack (first card of the bout) or a throw-in; otherwise it is a defence
// of the attack card targetID. It returns the card from the player's hand.
func (s *GameState) ValidatePlay(playerID, cardID, targetID string) (Card, error) {
	p, err := s.seated(playerID)
	if err != nil {
		return Card{}, err
	}
	card, ok := p.HandCard(cardID)
	if !ok {
		return Card{}, ErrCardNotInHand
	}
	if targetID != "" {
		return card, s.validateDefense(p, card, targetID)
	}
	return card, s.validateAttack(p, card)
}

func (s *GameState) validateAttack(p *Player, card Card) error {
	if p.ID == s.DefenderID {
		return ErrDefenderCannotAttack
	}
	if s.TableEmpty() {
		if p.ID != s.CurrentTurn {
			return ErrNotYourTurn
		}
		return nil // the lead attacker may open with any card
	}
	if !s.TableRanks()[card.Rank] {
		return ErrRankMismatch
	}
	if !s.ThrowInAllowed() {
		return ErrDefenderNoCards
	}
	return nil
}

func (s *GameState) validateDefense(p *Player, card Card, targetID string) error {
	if p.ID != s.DefenderID {
		return ErrNotDefender
	}
	if s.DefenderTaking {
		return ErrDefenderIsTaking
	}
	defense, onTable := s.TableCards[targetID]
	if !onTable {
		return ErrTargetNotOnTable
	}
	if len(defense) > 0 {
		return ErrTargetAlreadyBeaten
	}
	target, ok := CardByID(targetID)
	if !ok {
		return ErrTargetNotOnTable
	}
	if !CanBeat(target, card, s.ActiveTrump()) {
		return ErrCannotBeat
	}
	return nil
}

// ValidateTransfer checks a TRANSFER_TURN intent: the defender places a card
// of the same rank as the attack and the defence moves to the next player.
// Allowed from the very first bout. Jokers may transfer Jokers; the Super
// card may never transfer.
func (s *GameState) ValidateTransfer(playerID, cardID string) (Card, error) {
	p, err := s.seated(playerID)
	if err != nil {
		return Card{}, err
	}
	card, ok := p.HandCard(cardID)
	if !ok {
		return Card{}, ErrCardNotInHand
	}
	if p.ID != s.DefenderID {
		return Card{}, ErrNotDefender
	}
	if s.DefenderTaking {
		return Card{}, ErrDefenderIsTaking
	}
	if s.TableEmpty() {
		return Card{}, ErrTableEmpty
	}
	if s.AnyDefended() {
		return Card{}, ErrTransferAfterDefense
	}
	if card.IsSuper() {
		return Card{}, ErrSuperCannotTransfer
	}
	attacks := s.TableAttackCards()
	for _, a := range attacks {
		if a.Rank != card.Rank {
			return Card{}, ErrTransferRank
		}
	}
	// Classic rule: the next player must be able to answer every attack card,
	// i.e. hold at least as many cards as the table will have after the
	// transfer (this also forbids transferring to an empty hand).
	next := s.Player(s.NextActive(p.ID))
	if next == nil || len(next.Hand) < len(attacks)+1 {
		return Card{}, ErrTransferTooFewCards
	}
	return card, nil
}

// ValidateTake checks a TAKE_CARDS intent.
func (s *GameState) ValidateTake(playerID string) error {
	p, err := s.seated(playerID)
	if err != nil {
		return err
	}
	if p.ID != s.DefenderID {
		return ErrNotDefender
	}
	if s.TableEmpty() {
		return ErrTableEmpty
	}
	if s.DefenderTaking {
		return ErrAlreadyTaking
	}
	return nil
}

// ValidatePass checks a PASS intent ("I have nothing more to throw in").
func (s *GameState) ValidatePass(playerID string) error {
	p, err := s.seated(playerID)
	if err != nil {
		return err
	}
	if p.ID == s.DefenderID {
		return ErrDefenderCannotPass
	}
	if s.TableEmpty() {
		return ErrTableEmpty
	}
	if p.Passed {
		return ErrAlreadyPassed
	}
	return nil
}
