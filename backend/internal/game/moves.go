package game

import "fmt"

// MoveType names an intent the engine understands.
type MoveType string

const (
	MoveAttack    MoveType = "attack"   // first card of a bout
	MoveThrowIn   MoveType = "throw_in" // another attack card on a busy table
	MoveDefend    MoveType = "defend"   // beat TargetID with CardID
	MoveTransfer  MoveType = "transfer" // pass the defence on with CardID
	MoveTake      MoveType = "take"     // the defender takes the table
	MovePass      MoveType = "pass"     // "nothing more to throw in"
	MoveTakeStump MoveType = "take_stump"
)

// Move is one legal action of a player: what the bots choose from and what
// the LLM has to pick by number.
type Move struct {
	Type     MoveType `json:"type"`
	CardID   string   `json:"card_id,omitempty"`
	TargetID string   `json:"target_id,omitempty"`
}

func (m Move) String() string {
	switch m.Type {
	case MoveDefend:
		return fmt.Sprintf("%s %s->%s", m.Type, m.CardID, m.TargetID)
	case MoveAttack, MoveThrowIn, MoveTransfer:
		return fmt.Sprintf("%s %s", m.Type, m.CardID)
	}
	return string(m.Type)
}

// LegalMoves enumerates everything playerID may do right now, straight from
// the validators, so it can never disagree with what the engine accepts.
func (s *GameState) LegalMoves(playerID string) []Move {
	var moves []Move
	p := s.Player(playerID)
	if p == nil || s.Status != StatusPlaying {
		return moves
	}
	if s.ValidateTakeStump(playerID) == nil {
		return []Move{{Type: MoveTakeStump}}
	}
	if s.Phase == PhaseResolving || s.StumpsPending() {
		return moves
	}
	for _, c := range p.Hand {
		if _, err := s.ValidatePlay(playerID, c.ID, ""); err == nil {
			kind := MoveThrowIn
			if s.TableEmpty() {
				kind = MoveAttack
			}
			moves = append(moves, Move{Type: kind, CardID: c.ID})
		}
		for _, target := range s.TableOrder {
			if len(s.TableCards[target]) > 0 {
				continue
			}
			if _, err := s.ValidatePlay(playerID, c.ID, target); err == nil {
				moves = append(moves, Move{Type: MoveDefend, CardID: c.ID, TargetID: target})
			}
		}
		if _, err := s.ValidateTransfer(playerID, c.ID); err == nil {
			moves = append(moves, Move{Type: MoveTransfer, CardID: c.ID})
		}
	}
	if s.ValidateTake(playerID) == nil {
		moves = append(moves, Move{Type: MoveTake})
	}
	if s.ValidatePass(playerID) == nil {
		moves = append(moves, Move{Type: MovePass})
	}
	return moves
}

// ApplyMove performs a Move through the regular engine entry points.
func (e *Engine) ApplyMove(s *GameState, playerID string, m Move) error {
	switch m.Type {
	case MoveAttack, MoveThrowIn:
		return e.PlayCard(s, playerID, m.CardID, "")
	case MoveDefend:
		return e.PlayCard(s, playerID, m.CardID, m.TargetID)
	case MoveTransfer:
		return e.TransferTurn(s, playerID, m.CardID)
	case MoveTake:
		return e.TakeCards(s, playerID)
	case MovePass:
		return e.Pass(s, playerID)
	case MoveTakeStump:
		return e.TakeStump(s, playerID)
	}
	return fmt.Errorf("unknown move %q", m.Type)
}

// --- bots in the lobby ---------------------------------------------------------

// ErrNotABot is returned when REMOVE_BOT names a human seat.
var ErrNotABot = ruleErr("NOT_A_BOT", "that seat is not a bot")

// AddBot seats a server-driven player. Bots are always ready; a room keeps
// at least one human seat free so the game is never bots only.
func (e *Engine) AddBot(s *GameState, id, name string) error {
	if s.Status == StatusFinished {
		e.resetToLobby(s)
	}
	if s.Status != StatusWaiting {
		return ErrGameInProgress
	}
	if s.MaxPlayers <= 0 {
		s.MaxPlayers = MaxPlayers
	}
	if len(s.Players) >= s.MaxPlayers || len(s.Players)-s.HumanCount() >= s.MaxPlayers-1 {
		return ErrRoomFull
	}
	if s.Player(id) != nil {
		return nil
	}
	s.Players = append(s.Players, Player{
		ID:        id,
		Name:      name,
		Hand:      []Card{},
		Stump:     []Card{},
		Connected: true,
		IsReady:   true,
		IsBot:     true,
	})
	if s.HostID == "" {
		s.HostID = id
	}
	s.addLog(LogEntry{Type: "join", PlayerID: id, Text: "bot"})
	s.touch()
	return nil
}

// RemoveBot frees a bot's seat while the room is waiting.
func (e *Engine) RemoveBot(s *GameState, id string) error {
	if s.Status != StatusWaiting {
		return ErrNotWaiting
	}
	p := s.Player(id)
	if p == nil {
		return ErrUnknownPlayer
	}
	if !p.IsBot {
		return ErrNotABot
	}
	e.removePlayer(s, id)
	s.addLog(LogEntry{Type: "leave", PlayerID: id, Text: "bot"})
	s.touch()
	return nil
}
