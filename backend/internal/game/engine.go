package game

import (
	"fmt"
	"math/rand/v2"
)

// Engine mutates GameState according to the rules. It is stateless apart
// from its random source, so one Engine can serve any number of rooms.
type Engine struct {
	rng *rand.Rand
}

// NewEngine creates an engine with the given random source (nil = random seed).
func NewEngine(rng *rand.Rand) *Engine {
	if rng == nil {
		rng = rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	}
	return &Engine{rng: rng}
}

// firstMoveOrder lists the cards that decide who opens the game, in priority
// order: 2♠, 3♠, 4♠, 5♠, 6♠, then 2♣.
var firstMoveOrder = []string{"S_2", "S_3", "S_4", "S_5", "S_6", "C_2"}

// --- lobby ---------------------------------------------------------------

// Join seats a player, or marks a known player as connected again. A new
// player may only join while the room is waiting (a finished room is reset
// to the lobby first).
func (e *Engine) Join(s *GameState, id, name, avatarURL string) error {
	if p := s.Player(id); p != nil {
		p.Connected = true
		if name != "" {
			p.Name = name
		}
		if avatarURL != "" {
			p.AvatarURL = avatarURL
		}
		s.addLog(LogEntry{Type: "rejoin", PlayerID: id})
		s.touch()
		return nil
	}
	if s.Status == StatusFinished {
		e.resetToLobby(s)
	}
	if s.Status != StatusWaiting {
		return ErrGameInProgress
	}
	if s.MaxPlayers <= 0 {
		s.MaxPlayers = MaxPlayers
	}
	if len(s.Players) >= s.MaxPlayers {
		return ErrRoomFull
	}
	s.Players = append(s.Players, Player{
		ID:        id,
		Name:      name,
		Hand:      []Card{},
		Stump:     []Card{},
		Connected: true,
		AvatarURL: avatarURL,
	})
	if s.HostID == "" {
		s.HostID = id
	}
	s.addLog(LogEntry{Type: "join", PlayerID: id})
	s.touch()
	return nil
}

// Disconnect is called when a player's connection drops. In the lobby the
// seat is freed; during a game the seat is kept so the player can come back.
func (e *Engine) Disconnect(s *GameState, id string) error {
	p := s.Player(id)
	if p == nil {
		return nil
	}
	if s.Status == StatusPlaying {
		p.Connected = false
		s.addLog(LogEntry{Type: "disconnect", PlayerID: id})
		s.touch()
		return nil
	}
	e.removePlayer(s, id)
	s.addLog(LogEntry{Type: "leave", PlayerID: id})
	s.touch()
	return nil
}

// Leave removes a player for good. Leaving a running game forfeits it: the
// player's cards are discarded and, if nobody else is left, the leaver loses.
func (e *Engine) Leave(s *GameState, id string) error {
	p := s.Player(id)
	if p == nil {
		return nil
	}
	if s.Status != StatusPlaying {
		e.removePlayer(s, id)
		s.addLog(LogEntry{Type: "leave", PlayerID: id})
		s.touch()
		return nil
	}
	wasAttacker := s.CurrentTurn == id
	wasDefender := s.DefenderID == id
	nextAfter := s.NextActive(id)
	s.DiscardCount += len(p.Hand) + len(p.Stump)
	e.removePlayer(s, id)
	s.addLog(LogEntry{Type: "leave", PlayerID: id, Text: "forfeit"})

	if len(s.ActivePlayers()) < MinPlayers {
		s.finish(id)
		s.touch()
		return nil
	}
	if wasAttacker || wasDefender {
		// The bout cannot continue without one of its two protagonists.
		for _, aid := range s.TableOrder {
			s.DiscardCount += 1 + len(s.TableCards[aid])
		}
		s.clearTable()
		if wasAttacker {
			s.CurrentTurn = nextAfter
		}
		s.DefenderID = s.NextActive(s.CurrentTurn)
	}
	s.touch()
	return nil
}

// SetReady toggles readiness. When every seated player (at least two) is
// ready the game starts automatically. Pressing "ready" in a finished room
// resets it to the lobby first.
func (e *Engine) SetReady(s *GameState, id string, ready bool) error {
	if s.Status == StatusFinished && ready {
		e.resetToLobby(s)
	}
	if s.Status != StatusWaiting {
		return ErrNotWaiting
	}
	p := s.Player(id)
	if p == nil {
		return ErrUnknownPlayer
	}
	p.IsReady = ready
	s.addLog(LogEntry{Type: "ready", PlayerID: id, Text: fmt.Sprint(ready)})
	s.touch()
	if len(s.Players) >= MinPlayers && s.allReady() {
		return e.Start(s)
	}
	return nil
}

func (s *GameState) allReady() bool {
	for i := range s.Players {
		if !s.Players[i].IsReady {
			return false
		}
	}
	return true
}

// Start deals a new game: 6 cards and a 2-card stump to everybody, a random
// base card becomes the hidden trump at the bottom of the deck, and the
// holder of 2♠ (3♠, 4♠, 5♠, 6♠, 2♣ as fallbacks) opens the game.
func (e *Engine) Start(s *GameState) error {
	if s.Status != StatusWaiting {
		return ErrNotWaiting
	}
	if len(s.Players) < MinPlayers {
		return ErrNotEnoughPlayers
	}
	deck := NewDeck()
	Shuffle(deck, e.rng)

	// The trump is a random *base* card so that a trump suit always exists.
	baseIdx := make([]int, 0, DeckSize)
	for i, c := range deck {
		if !c.IsSpecial() {
			baseIdx = append(baseIdx, i)
		}
	}
	ti := baseIdx[e.rng.IntN(len(baseIdx))]
	trump := deck[ti]
	deck = append(deck[:ti], deck[ti+1:]...)

	for i := range s.Players {
		p := &s.Players[i]
		p.Hand = []Card{}
		p.Stump = []Card{}
		p.Out = false
		p.Passed = false
	}
	for r := 0; r < HandSize; r++ {
		for i := range s.Players {
			s.Players[i].Hand = append(s.Players[i].Hand, deck[0])
			deck = deck[1:]
		}
	}
	for r := 0; r < StumpSize; r++ {
		for i := range s.Players {
			s.Players[i].Stump = append(s.Players[i].Stump, deck[0])
			deck = deck[1:]
		}
	}
	s.Deck = deck
	s.TrumpCard = &trump
	s.TrumpSuit = trump.Suit
	s.TrumpRevealed = false
	s.TableCards = map[string][]Card{}
	s.TableOrder = []string{}
	s.DefenderTaking = false
	s.DiscardCount = 0
	s.FinishedOrder = []string{}
	s.LoserID = ""
	s.TransferCount = 0
	s.BoutNumber = 1
	s.Status = StatusPlaying

	s.CurrentTurn = e.firstAttacker(s)
	s.DefenderID = s.NextActive(s.CurrentTurn)
	s.addLog(LogEntry{Type: "start", PlayerID: s.CurrentTurn})
	s.touch()
	return nil
}

func (e *Engine) firstAttacker(s *GameState) string {
	for _, cardID := range firstMoveOrder {
		for i := range s.Players {
			if _, ok := s.Players[i].HandCard(cardID); ok {
				return s.Players[i].ID
			}
		}
	}
	return s.Players[e.rng.IntN(len(s.Players))].ID
}

// --- moves ---------------------------------------------------------------

// PlayCard handles PLAY_CARD: an attack / throw-in (targetID == "") or a
// defence of the attack card targetID.
func (e *Engine) PlayCard(s *GameState, playerID, cardID, targetID string) error {
	card, err := s.ValidatePlay(playerID, cardID, targetID)
	if err != nil {
		return err
	}
	p := s.Player(playerID)
	p.removeFromHand(cardID)
	if targetID != "" {
		s.TableCards[targetID] = []Card{card}
		s.addLog(LogEntry{Type: "defend", PlayerID: playerID, CardID: cardID, TargetID: targetID})
	} else {
		s.TableCards[card.ID] = []Card{}
		s.TableOrder = append(s.TableOrder, card.ID)
		kind := "attack"
		if len(s.TableOrder) > 1 {
			kind = "throw_in"
		}
		s.addLog(LogEntry{Type: kind, PlayerID: playerID, CardID: cardID})
	}
	s.resetPasses()
	e.checkBoutEnd(s)
	s.touch()
	return nil
}

// TransferTurn handles TRANSFER_TURN: the defender adds a card of the same
// rank and the next player becomes the defender. The transferring player
// becomes the lead attacker of the bout.
func (e *Engine) TransferTurn(s *GameState, playerID, cardID string) error {
	card, err := s.ValidateTransfer(playerID, cardID)
	if err != nil {
		return err
	}
	p := s.Player(playerID)
	p.removeFromHand(cardID)
	s.TableCards[card.ID] = []Card{}
	s.TableOrder = append(s.TableOrder, card.ID)
	oldDefender := s.DefenderID
	s.DefenderID = s.NextActive(oldDefender)
	s.CurrentTurn = oldDefender
	s.TransferCount++
	s.resetPasses()
	s.addLog(LogEntry{Type: "transfer", PlayerID: playerID, CardID: cardID, TargetID: s.DefenderID})
	e.checkBoutEnd(s)
	s.touch()
	return nil
}

// TakeCards handles TAKE_CARDS: the defender gives up. Attackers may still
// throw in matching cards; everything goes to the defender once they are done.
func (e *Engine) TakeCards(s *GameState, playerID string) error {
	if err := s.ValidateTake(playerID); err != nil {
		return err
	}
	s.DefenderTaking = true
	s.resetPasses()
	s.addLog(LogEntry{Type: "take", PlayerID: playerID})
	e.checkBoutEnd(s)
	s.touch()
	return nil
}

// Pass handles PASS: an attacker declares they have nothing more to add.
func (e *Engine) Pass(s *GameState, playerID string) error {
	if err := s.ValidatePass(playerID); err != nil {
		return err
	}
	s.Player(playerID).Passed = true
	s.addLog(LogEntry{Type: "pass", PlayerID: playerID})
	e.checkBoutEnd(s)
	s.touch()
	return nil
}

// --- bout lifecycle --------------------------------------------------------

func (s *GameState) resetPasses() {
	for i := range s.Players {
		s.Players[i].Passed = false
	}
}

// attackersDone reports whether every potential attacker has passed, has no
// cards, or (optionally) has nothing legal to throw in.
func (s *GameState) attackersDone() bool {
	for i := range s.Players {
		p := &s.Players[i]
		if p.Out || p.ID == s.DefenderID {
			continue
		}
		if p.Passed || len(p.Hand) == 0 {
			continue
		}
		if AutoPassWhenNoLegalThrowIn && !s.CanThrowIn(p) {
			continue
		}
		return false
	}
	return true
}

func (e *Engine) checkBoutEnd(s *GameState) {
	if s.TableEmpty() {
		return
	}
	if s.DefenderTaking {
		if s.attackersDone() {
			e.endBout(s, true)
		}
		return
	}
	if !s.AllDefended() {
		return
	}
	d := s.Defender()
	if d == nil || len(d.Hand) == 0 || s.attackersDone() {
		e.endBout(s, false)
	}
}

func (s *GameState) tableCards() []Card {
	cards := []Card{}
	for _, id := range s.TableOrder {
		if c, ok := CardByID(id); ok {
			cards = append(cards, c)
		}
		cards = append(cards, s.TableCards[id]...)
	}
	return cards
}

func (s *GameState) clearTable() {
	s.TableCards = map[string][]Card{}
	s.TableOrder = []string{}
	s.DefenderTaking = false
	s.TransferCount = 0
	s.resetPasses()
}

// endBout finishes the current bout: the defender either takes everything or
// the cards are discarded ("бито"); then hands are refilled, stumps picked
// up, finished players leave and the next roles are assigned.
func (e *Engine) endBout(s *GameState, took bool) {
	defenderID := s.DefenderID
	attackerID := s.CurrentTurn
	cards := s.tableCards()
	d := s.Player(defenderID)
	if took {
		d.Hand = append(d.Hand, cards...)
		s.addLog(LogEntry{Type: "took", PlayerID: defenderID, Text: fmt.Sprint(len(cards))})
	} else {
		s.DiscardCount += len(cards)
		s.addLog(LogEntry{Type: "bito", PlayerID: defenderID, Text: fmt.Sprint(len(cards))})
	}
	s.clearTable()

	s.refill(attackerID, defenderID)
	s.pickupStumps()
	s.markOuts()
	if s.finishIfOver() {
		return
	}

	var nextAttacker string
	switch {
	case took:
		nextAttacker = s.NextActive(defenderID)
	case !d.Out:
		nextAttacker = defenderID
	default:
		nextAttacker = s.NextActive(defenderID)
	}
	s.CurrentTurn = nextAttacker
	s.DefenderID = s.NextActive(nextAttacker)
	s.BoutNumber++
}

// draw takes the top card of the deck. When the main deck runs out the trump
// is revealed; the trump card itself is the very last card to be drawn.
func (s *GameState) draw() (Card, bool) {
	if len(s.Deck) > 0 {
		c := s.Deck[0]
		s.Deck = s.Deck[1:]
		if len(s.Deck) == 0 {
			s.revealTrump()
		}
		return c, true
	}
	if s.TrumpCard != nil {
		c := *s.TrumpCard
		s.TrumpCard = nil
		return c, true
	}
	return Card{}, false
}

func (s *GameState) revealTrump() {
	if s.TrumpRevealed {
		return
	}
	s.TrumpRevealed = true
	cardID := ""
	if s.TrumpCard != nil {
		cardID = s.TrumpCard.ID
	}
	s.addLog(LogEntry{Type: "trump_revealed", CardID: cardID, Text: s.TrumpSuit})
}

// refill tops hands up to HandSize: lead attacker first, then the other
// attackers clockwise, the defender last.
func (s *GameState) refill(attackerID, defenderID string) {
	order := []string{attackerID}
	cur := attackerID
	for i := 0; i < len(s.Players); i++ {
		cur = s.NextActive(cur)
		if cur == "" || cur == attackerID {
			break
		}
		if cur != defenderID {
			order = append(order, cur)
		}
	}
	if defenderID != attackerID {
		order = append(order, defenderID)
	}
	for _, id := range order {
		p := s.Player(id)
		if p == nil || p.Out {
			continue
		}
		for len(p.Hand) < HandSize {
			c, ok := s.draw()
			if !ok {
				return
			}
			p.Hand = append(p.Hand, c)
		}
	}
}

// pickupStumps gives a player their hidden stump once the main deck is empty
// and their hand is empty.
func (s *GameState) pickupStumps() {
	if len(s.Deck) > 0 {
		return
	}
	for i := range s.Players {
		p := &s.Players[i]
		if p.Out || len(p.Hand) > 0 || len(p.Stump) == 0 {
			continue
		}
		p.Hand = append(p.Hand, p.Stump...)
		p.Stump = []Card{}
		s.addLog(LogEntry{Type: "stump", PlayerID: p.ID})
	}
}

// markOuts removes players who have no cards anywhere once nothing is left
// to draw. They finish the game as winners (in order).
func (s *GameState) markOuts() {
	if len(s.Deck) > 0 || s.TrumpCard != nil {
		return
	}
	for i := range s.Players {
		p := &s.Players[i]
		if p.Out || len(p.Hand) > 0 || len(p.Stump) > 0 {
			continue
		}
		p.Out = true
		s.FinishedOrder = append(s.FinishedOrder, p.ID)
		s.addLog(LogEntry{Type: "out", PlayerID: p.ID})
	}
}

// finishIfOver ends the game when at most one player still holds cards.
func (s *GameState) finishIfOver() bool {
	active := s.ActivePlayers()
	if len(active) > 1 {
		return false
	}
	loser := ""
	if len(active) == 1 {
		loser = active[0]
	}
	s.finish(loser)
	return true
}

func (s *GameState) finish(loserID string) {
	s.Status = StatusFinished
	s.LoserID = loserID
	s.CurrentTurn = ""
	s.DefenderID = ""
	s.clearTable()
	for i := range s.Players {
		s.Players[i].IsReady = false
	}
	s.addLog(LogEntry{Type: "finish", PlayerID: loserID})
}

// --- helpers -------------------------------------------------------------

func (e *Engine) removePlayer(s *GameState, id string) {
	i := s.PlayerIndex(id)
	if i < 0 {
		return
	}
	s.Players = append(s.Players[:i], s.Players[i+1:]...)
	if s.HostID == id {
		s.HostID = ""
		if len(s.Players) > 0 {
			s.HostID = s.Players[0].ID
		}
	}
}

// resetToLobby throws away the finished game and keeps the connected players.
func (e *Engine) resetToLobby(s *GameState) {
	kept := s.Players[:0]
	for _, p := range s.Players {
		if !p.Connected {
			continue
		}
		p.Hand = []Card{}
		p.Stump = []Card{}
		p.Out = false
		p.Passed = false
		p.IsReady = false
		kept = append(kept, p)
	}
	s.Players = kept
	if s.HostID == "" || s.PlayerIndex(s.HostID) < 0 {
		s.HostID = ""
		if len(s.Players) > 0 {
			s.HostID = s.Players[0].ID
		}
	}
	s.Deck = []Card{}
	s.TrumpCard = nil
	s.TrumpSuit = ""
	s.TrumpRevealed = false
	s.clearTable()
	s.CurrentTurn = ""
	s.DefenderID = ""
	s.LoserID = ""
	s.FinishedOrder = []string{}
	s.DiscardCount = 0
	s.BoutNumber = 0
	s.Status = StatusWaiting
	s.addLog(LogEntry{Type: "lobby"})
}
