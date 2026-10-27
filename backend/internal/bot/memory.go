package bot

import (
	"math"
	"sort"

	"github.com/breezeareasay/durag/backend/internal/game"
)

// Memory is what the bots of one room remember about the cards: a bot sees
// only counts of the other hands, but it reads the table log. Everything
// that went to the discard is gone for good; everything a player took is
// known to sit in that hand until it shows up on the table again; and a
// bot remembers who threw it the cards it took.
//
// The memory is fed every state the room publishes (Observe) and is read
// through a Snapshot, so a bot thinking outside the room lock never races
// with the next state. It follows the log by sequence number, so a bout
// that was cleared between two looks is still filed in full.
type Memory struct {
	seq      int64             // the last log entry processed
	bout     []string          // cards put on the table since the last bout ended
	seen     map[string]bool   // card id -> gone to the discard
	holder   map[string]string // card id -> player known to hold it
	origin   map[string]string // card id -> who last put it on the table
	tookFrom map[string]string // card id -> the human who threw it before a bot took it
	plans    map[string]Plan   // bot id -> the plan it is following
	// Recall is the last card a human played that the memory had already
	// seen in that hand: the thing to needle about ("that one again?").
	Recall Recall
}

// Recall is a card a human just played that the bot knew they held.
type Recall struct {
	Card     game.Card
	PlayerID string
	Name     string
	Version  int64 // the state it happened in: stale on any later state
}

// NewMemory starts with an empty table.
func NewMemory() *Memory {
	m := &Memory{}
	m.Reset()
	return m
}

// Reset forgets everything: a new game, a new deck.
func (m *Memory) Reset() {
	m.seq = 0
	m.bout = nil
	m.seen = map[string]bool{}
	m.holder = map[string]string{}
	m.origin = map[string]string{}
	m.tookFrom = map[string]string{}
	m.plans = map[string]Plan{}
	m.Recall = Recall{}
}

// Observe updates the memory with a state (any view: the table and the log
// are the same for everybody). Entries already processed are skipped, so
// the same state can be observed twice.
func (m *Memory) Observe(s *game.GameState) {
	if s.Status != game.StatusPlaying {
		return
	}
	for _, e := range s.Log {
		if e.Seq <= m.seq {
			continue
		}
		m.seq = e.Seq
		switch e.Type {
		case "attack", "throw_in", "defend", "transfer", "timeout":
			if e.CardID != "" {
				m.played(s, e.CardID, e.PlayerID)
			}
		case "took":
			m.record(s, game.OutcomeTook, e.PlayerID)
		case "bito":
			m.record(s, game.OutcomeBito, e.PlayerID)
		case "trump_revealed":
			if e.CardID != "" && e.PlayerID != "" {
				m.holder[e.CardID] = e.PlayerID
			}
		case "start", "lobby":
			m.bout = nil
		}
	}
}

// played files a card that landed on the table: who put it there, and was
// it known to sit in that hand.
func (m *Memory) played(s *game.GameState, id, who string) {
	m.origin[id] = who
	if h, known := m.holder[id]; known && h == who {
		if p := s.Player(who); p != nil && !p.IsBot {
			if c, ok := game.CardByID(id); ok {
				m.Recall = Recall{Card: c, PlayerID: who, Name: p.Name, Version: s.Version}
			}
		}
	}
	delete(m.holder, id)
	m.bout = append(m.bout, id)
}

// record files the cards of the finished bout: taken into a hand or gone.
func (m *Memory) record(s *game.GameState, outcome, defenderID string) {
	defender := s.Player(defenderID)
	for _, id := range m.bout {
		switch outcome {
		case game.OutcomeTook:
			m.holder[id] = defenderID
			if defender != nil && defender.IsBot {
				if from := m.origin[id]; from != "" && from != defenderID {
					if p := s.Player(from); p != nil && !p.IsBot {
						m.tookFrom[id] = from
					}
				}
			}
		case game.OutcomeBito:
			m.seen[id] = true
			delete(m.tookFrom, id)
		}
	}
	m.bout = nil
}

// Snapshot copies the memory for a bot to think on outside the lock.
func (m *Memory) Snapshot() *Memory {
	c := &Memory{seq: m.seq, Recall: m.Recall}
	c.bout = append([]string{}, m.bout...)
	c.seen = copyBools(m.seen)
	c.holder = copyStrings(m.holder)
	c.origin = copyStrings(m.origin)
	c.tookFrom = copyStrings(m.tookFrom)
	c.plans = map[string]Plan{}
	for k, v := range m.plans {
		c.plans[k] = v
	}
	return c
}

func copyBools(in map[string]bool) map[string]bool {
	out := make(map[string]bool, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func copyStrings(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// Known lists the cards the memory places in a player's hand, in deck order.
func (m *Memory) Known(playerID string) []game.Card {
	var out []game.Card
	for id, h := range m.holder {
		if h != playerID {
			continue
		}
		if c, ok := game.CardByID(id); ok {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// TookFrom names the human who threw this card before a bot took it, or "".
func (m *Memory) TookFrom(cardID string) string {
	if m == nil {
		return ""
	}
	return m.tookFrom[cardID]
}

// Gone is how many cards are out of the game for good.
func (m *Memory) Gone() int { return len(m.seen) }

// Plan returns the plan a bot followed last time.
func (m *Memory) Plan(botID string) Plan { return m.plans[botID] }

// SetPlan records the plan a bot follows now.
func (m *Memory) SetPlan(botID string, p Plan) { m.plans[botID] = p }

// BeatProb estimates how likely player `who` can beat `attack` with a card in
// hand, as seen by the viewer of s (whose own hand is public to them).
//
// The defender's unknown cards are a uniformly random h-subset of the U
// cards the viewer cannot place (not in their hand, not on the table, not
// in the discard, not known to sit in any hand). With B of those U beating
// the attack, the chance that none was dealt to the defender is the
// hypergeometric tail Π_{i<h} (U−B−i)/(U−i); a known card that beats the
// attack makes the answer 1 outright. No memory (nil) means nothing is
// known and the estimate uses the whole deck.
func (m *Memory) BeatProb(s *game.GameState, who string, attack game.Card) float64 {
	p := s.Player(who)
	if p == nil {
		return 1
	}
	trump := s.ActiveTrump()
	placed := map[string]bool{}
	for _, id := range s.TableOrder {
		placed[id] = true
		for _, d := range s.TableCards[id] {
			placed[d.ID] = true
		}
	}
	if me := s.Player(s.ViewerID); me != nil {
		for _, c := range me.Hand {
			placed[c.ID] = true
		}
	}
	known := 0
	if m != nil {
		for id, h := range m.holder {
			if h != who {
				continue
			}
			known++
			if c, ok := game.CardByID(id); ok && game.CanBeat(attack, c, trump) {
				return 1
			}
		}
	}
	U, B := 0, 0
	for _, c := range game.NewDeck() {
		if placed[c.ID] {
			continue
		}
		if m != nil {
			if m.seen[c.ID] {
				continue
			}
			if _, held := m.holder[c.ID]; held {
				continue
			}
		}
		U++
		if game.CanBeat(attack, c, trump) {
			B++
		}
	}
	h := p.HandCount - known
	if h <= 0 || U == 0 {
		return 0
	}
	if h > U {
		h = U
	}
	q := 1.0
	for i := 0; i < h; i++ {
		if U-B-i <= 0 {
			return 1
		}
		q *= float64(U-B-i) / float64(U-i)
	}
	return math.Min(1, math.Max(0, 1-q))
}
