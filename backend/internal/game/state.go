package game

import "time"

// Game status values.
const (
	StatusWaiting  = "waiting"  // lobby: players join and press "ready"
	StatusPlaying  = "playing"  // a game is in progress
	StatusFinished = "finished" // the game ended; a loser (or a draw) is known
)

// Bout phases. The empty string is normal play; after the last card of a
// bout is beaten (or the defender finished taking) the table stays visible
// in the resolving phase until ResolveAt, so everybody can see what beat what.
const (
	PhaseBout      = ""
	PhaseResolving = "resolving"
)

// Bout outcomes (GameState.ResolveOutcome and the "bout_end" log entry).
const (
	OutcomeBito = "bito"
	OutcomeTook = "took"
)

// Table constants.
const (
	HandSize   = 6 // cards dealt to every hand and refilled to after each bout
	StumpSize  = 2 // hidden cards of every player's "stump" (пенёк)
	MinPlayers = 2
	MaxPlayers = 6 // 6 players * (6 + 2) = 48 cards, 7 remain in the deck
	MaxLog     = 40
)

// Player is a seat at the table.
type Player struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Hand    []Card `json:"hand"`
	Stump   []Card `json:"stump"` // 2 hidden cards, taken into the hand when the deck is empty
	IsReady bool   `json:"is_ready"`

	// Extensions (not in the original data model).
	AvatarURL  string `json:"avatar_url,omitempty"`
	Connected  bool   `json:"connected"`
	Passed     bool   `json:"passed"`      // "бито" said in the current bout
	Out        bool   `json:"out"`         // got rid of all cards, left the game as a non-loser
	HandCount  int    `json:"hand_count"`  // filled by Sanitized()
	StumpCount int    `json:"stump_count"` // filled by Sanitized()
}

// LogEntry is a compact description of something that happened. The client
// uses it for toasts and the move history.
type LogEntry struct {
	Type     string `json:"type"`
	PlayerID string `json:"player_id,omitempty"`
	CardID   string `json:"card_id,omitempty"`
	TargetID string `json:"target_id,omitempty"`
	Text     string `json:"text,omitempty"`
}

// GameState is the authoritative server state of one room. The JSON
// representation stored in Redis is complete; clients only ever receive the
// result of Sanitized().
type GameState struct {
	RoomID      string            `json:"room_id"`
	Players     []Player          `json:"players"`
	Deck        []Card            `json:"deck"`
	TrumpCard   *Card             `json:"trump_card"`             // hidden until the main deck is empty, nil once drawn
	TableCards  map[string][]Card `json:"table_cards"`            // attack card ID -> defending card (0 or 1 element)
	CurrentTurn string            `json:"current_turn_player_id"` // the lead attacker of the current bout
	Status      string            `json:"status"`

	// Extensions.
	TableOrder     []string   `json:"table_order"` // attack card IDs in the order they were played
	DefenderID     string     `json:"defender_id"`
	DefenderTaking bool       `json:"defender_taking"` // defender announced "I take"; attackers may still throw in
	TrumpRevealed  bool       `json:"trump_revealed"`
	TrumpSuit      string     `json:"trump_suit"` // sanitized away until revealed
	DeckCount      int        `json:"deck_count"` // filled by Sanitized()
	DiscardCount   int        `json:"discard_count"`
	HostID         string     `json:"host_id"`
	MaxPlayers     int        `json:"max_players"`
	LoserID        string     `json:"loser_id,omitempty"`
	FinishedOrder  []string   `json:"finished_order"` // players who got rid of their cards, first to last
	Version        int64      `json:"version"`
	UpdatedAt      int64      `json:"updated_at"`
	Log            []LogEntry `json:"log"`
	ViewerID       string     `json:"viewer_id,omitempty"` // filled by Sanitized(): who this view belongs to
	TransferCount  int        `json:"transfer_count"`      // transfers in the current bout (informational)
	BoutNumber     int        `json:"bout_number"`
	Phase          string     `json:"phase"`           // "" or PhaseResolving
	ResolveAt      int64      `json:"resolve_at"`      // unix milliseconds; when the resolving phase ends
	ResolveOutcome string     `json:"resolve_outcome"` // OutcomeBito / OutcomeTook while resolving
}

// NewGameState creates an empty waiting room.
func NewGameState(roomID string) *GameState {
	return &GameState{
		RoomID:     roomID,
		Players:    []Player{},
		Deck:       []Card{},
		TableCards: map[string][]Card{},
		TableOrder: []string{},
		Status:     StatusWaiting,
		MaxPlayers: MaxPlayers,
		UpdatedAt:  time.Now().Unix(),
	}
}

// --- lookups -------------------------------------------------------------

// PlayerIndex returns the seat index of a player or -1.
func (s *GameState) PlayerIndex(id string) int {
	for i := range s.Players {
		if s.Players[i].ID == id {
			return i
		}
	}
	return -1
}

// Player returns a pointer to the player with the given ID or nil.
func (s *GameState) Player(id string) *Player {
	i := s.PlayerIndex(id)
	if i < 0 {
		return nil
	}
	return &s.Players[i]
}

// Defender returns the current defender or nil.
func (s *GameState) Defender() *Player { return s.Player(s.DefenderID) }

// Attacker returns the lead attacker of the bout or nil.
func (s *GameState) Attacker() *Player { return s.Player(s.CurrentTurn) }

// ActivePlayers returns IDs of players still in the game (not Out), in seat order.
func (s *GameState) ActivePlayers() []string {
	ids := make([]string, 0, len(s.Players))
	for i := range s.Players {
		if !s.Players[i].Out {
			ids = append(ids, s.Players[i].ID)
		}
	}
	return ids
}

// NextActive returns the ID of the next player clockwise after the given one
// who is still in the game. It returns "" if nobody else is active.
func (s *GameState) NextActive(afterID string) string {
	n := len(s.Players)
	if n == 0 {
		return ""
	}
	start := s.PlayerIndex(afterID)
	for step := 1; step <= n; step++ {
		p := &s.Players[(start+step+n)%n]
		if !p.Out && p.ID != afterID {
			return p.ID
		}
	}
	return ""
}

// TableEmpty reports whether no attack card lies on the table.
func (s *GameState) TableEmpty() bool { return len(s.TableOrder) == 0 }

// TableRanks returns the set of ranks currently visible on the table
// (attack and defence cards alike).
func (s *GameState) TableRanks() map[int]bool {
	ranks := map[int]bool{}
	for _, id := range s.TableOrder {
		if c, ok := s.tableAttackCard(id); ok {
			ranks[c.Rank] = true
		}
		for _, d := range s.TableCards[id] {
			ranks[d.Rank] = true
		}
	}
	return ranks
}

// AllDefended reports whether every attack card on the table is beaten.
func (s *GameState) AllDefended() bool {
	for _, id := range s.TableOrder {
		if len(s.TableCards[id]) == 0 {
			return false
		}
	}
	return true
}

// AnyDefended reports whether at least one attack card is already beaten.
func (s *GameState) AnyDefended() bool {
	for _, id := range s.TableOrder {
		if len(s.TableCards[id]) > 0 {
			return true
		}
	}
	return false
}

// UndefendedCount returns the number of attack cards still waiting for a defence.
func (s *GameState) UndefendedCount() int {
	n := 0
	for _, id := range s.TableOrder {
		if len(s.TableCards[id]) == 0 {
			n++
		}
	}
	return n
}

// ActiveTrump returns the trump suit if it is already revealed, "" otherwise.
// Until the main deck is empty nobody — not even the rules — honours trumps.
func (s *GameState) ActiveTrump() string {
	if s.TrumpRevealed {
		return s.TrumpSuit
	}
	return ""
}

// TableAttackCards returns the attack cards on the table in play order.
func (s *GameState) TableAttackCards() []Card {
	cards := make([]Card, 0, len(s.TableOrder))
	for _, id := range s.TableOrder {
		if c, ok := s.tableAttackCard(id); ok {
			cards = append(cards, c)
		}
	}
	return cards
}

// tableAttackCard reconstructs an attack card from its ID. Attack cards are
// stored by ID as map keys, so the full card is kept in the attackCards index.
func (s *GameState) tableAttackCard(id string) (Card, bool) {
	c, ok := cardByID(id)
	return c, ok
}

// cardByID rebuilds a card from its ID. All IDs are derived deterministically
// from suit and rank, so no lookup table is needed.
func cardByID(id string) (Card, bool) {
	switch id {
	case RedJokerID:
		return RedJoker, true
	case BlackJokerID:
		return BlackJoker, true
	case SuperCardID:
		return SuperCard, true
	}
	if len(id) < 3 || id[1] != '_' {
		return Card{}, false
	}
	var suit string
	switch id[0] {
	case 'H':
		suit = SuitHearts
	case 'D':
		suit = SuitDiamonds
	case 'C':
		suit = SuitClubs
	case 'S':
		suit = SuitSpades
	default:
		return Card{}, false
	}
	rank := 0
	for _, ch := range id[2:] {
		if ch < '0' || ch > '9' {
			return Card{}, false
		}
		rank = rank*10 + int(ch-'0')
	}
	if rank < RankTwo || rank > RankAce {
		return Card{}, false
	}
	return NewCard(suit, rank), true
}

// CardByID is the exported form of cardByID.
func CardByID(id string) (Card, bool) { return cardByID(id) }

// --- hand helpers --------------------------------------------------------

// HandCard finds a card in a player's hand.
func (p *Player) HandCard(cardID string) (Card, bool) {
	for _, c := range p.Hand {
		if c.ID == cardID {
			return c, true
		}
	}
	return Card{}, false
}

// removeFromHand removes a card from the hand and returns it.
func (p *Player) removeFromHand(cardID string) (Card, bool) {
	for i, c := range p.Hand {
		if c.ID == cardID {
			p.Hand = append(p.Hand[:i], p.Hand[i+1:]...)
			return c, true
		}
	}
	return Card{}, false
}

// HasRank reports whether the hand holds a card of the given rank.
func (p *Player) HasRank(rank int) bool {
	for _, c := range p.Hand {
		if c.Rank == rank {
			return true
		}
	}
	return false
}

// --- bookkeeping ---------------------------------------------------------

func (s *GameState) addLog(e LogEntry) {
	s.Log = append(s.Log, e)
	if len(s.Log) > MaxLog {
		s.Log = s.Log[len(s.Log)-MaxLog:]
	}
}

func (s *GameState) touch() {
	s.Version++
	s.UpdatedAt = time.Now().Unix()
}

// Clone returns a deep copy of the state.
func (s *GameState) Clone() *GameState {
	out := *s
	out.Players = make([]Player, len(s.Players))
	for i, p := range s.Players {
		out.Players[i] = p
		out.Players[i].Hand = append([]Card{}, p.Hand...)
		out.Players[i].Stump = append([]Card{}, p.Stump...)
	}
	out.Deck = append([]Card{}, s.Deck...)
	if s.TrumpCard != nil {
		t := *s.TrumpCard
		out.TrumpCard = &t
	}
	out.TableCards = make(map[string][]Card, len(s.TableCards))
	for k, v := range s.TableCards {
		out.TableCards[k] = append([]Card{}, v...)
	}
	out.TableOrder = append([]string{}, s.TableOrder...)
	out.FinishedOrder = append([]string{}, s.FinishedOrder...)
	out.Log = append([]LogEntry{}, s.Log...)
	return &out
}
