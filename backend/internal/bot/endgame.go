package bot

import (
	"context"
	"math/rand/v2"
	"time"

	"github.com/breezeareasay/durag/backend/internal/game"
)

// The endgame search. With the deck gone the game is nearly a game of
// perfect information: the memory knows what went to the discard and what
// somebody took, so the cards it cannot place are the opponents' hands and
// the stumps. A hard bot deals those out at random a few times
// (determinization), searches each deal and picks the move that does best
// on average. Two players left: a depth-limited alpha-beta over the real
// engine (ApplyMove on cloned states, zero delays). More: rollouts with the
// rule-based brain, which is cheaper and good enough to tell a losing move.
var (
	EndgameBudget     = 150 * time.Millisecond // wall clock per decision
	EndgameSamples    = 6                      // determinizations
	EndgameMaxUnknown = 16                     // more unknown cards: too many deals to matter
	EndgameDeckMax    = 4                      // the search starts while this many cards are still in the deck
	EndgameDepth      = 8                      // plies of alpha-beta
	EndgameNodes      = 4000                   // alpha-beta nodes per deal
	EndgameRollouts   = 4                      // rollouts per move per deal
	EndgameRolloutLen = 80                     // moves per rollout
	// EndgameNegamax searches two-player endings with alpha-beta instead of
	// rollouts (an experiment knob: rollouts proved steadier).
	EndgameNegamax = false
	// The hard level searches harder: from this many deck cards, with this
	// many deals and rollouts per move and this much wall clock.
	HardDeckMax  = 8
	HardSamples  = 8
	HardRollouts = 6
	HardBudget   = 260 * time.Millisecond
	// EvalQuality weighs the strength of the cards left, not only their count.
	EvalQuality = true
	// RolloutsCount lets everybody in a rollout play with the deal in full
	// view (the sampled world is known): sharper playouts, sharper estimates.
	RolloutsCount = true
	// NormalSearches lets the normal level use the search too (experiments switch it off).
	NormalSearches = true
)

// endgame returns the searched move, or false when the position is not
// one to search (deck alive, trump hidden, too much unknown, nothing to choose).
func (h *Heuristic) endgame(in Context) (game.Move, bool) {
	s := in.State
	hard := in.level() == LevelHard
	deckMax, budget := EndgameDeckMax, EndgameBudget
	if hard {
		deckMax, budget = HardDeckMax, HardBudget
	}
	if !hard && !NormalSearches {
		return game.Move{}, false
	}
	if s.DeckCount > deckMax || !s.TrumpRevealed || s.TrumpCard == nil || len(in.Moves) < 2 || in.Memory == nil {
		return game.Move{}, false
	}
	me := s.Player(in.BotID)
	if me == nil {
		return game.Move{}, false
	}
	pool, slots := h.unknownCards(in)
	if len(pool) > EndgameMaxUnknown || len(pool) < slots {
		return game.Move{}, false
	}
	deadline := time.Now().Add(budget)
	engine := game.NewEngine(game.NewRand(1, 1)) // never deals: the states are complete
	engine.MaxAttacks = s.MaxAttacks
	engine.Now = func() time.Time { return time.Unix(0, 0) }
	active := len(s.ActivePlayers())
	totals := make([]float64, len(in.Moves))
	counted := 0
	samples, rollouts := EndgameSamples, EndgameRollouts
	if hard {
		samples, rollouts = HardSamples, HardRollouts
	} else if samples > 2 {
		samples = (samples + 1) / 2 // the normal level looks at fewer deals
	}
	for sample := 0; sample < samples; sample++ {
		full := h.deal(in, pool)
		if full == nil {
			break
		}
		sr := &searcher{engine: engine, bot: in.BotID, deadline: deadline, rng: h.rng, playouts: rollouts}
		for i, m := range in.Moves {
			child := full.Clone()
			if err := engine.ApplyMove(child, in.BotID, m); err != nil {
				totals[i] -= 2 // the deal made the move illegal: a bad sign for it
				continue
			}
			sr.nodes = 0 // every candidate gets the same budget
			if active <= 2 && EndgameNegamax {
				totals[i] += sr.negamax(child, EndgameDepth, -2, 2)
			} else {
				totals[i] += sr.rollouts(child)
			}
		}
		counted++
		if time.Now().After(deadline) {
			break
		}
	}
	if counted == 0 {
		return game.Move{}, false
	}
	best := 0
	for i := range totals {
		if totals[i] > totals[best] {
			best = i
		}
	}
	return in.Moves[best], true
}

// unknownCards lists the cards the bot cannot place and how many such cards
// the hidden hands and stumps hold.
func (h *Heuristic) unknownCards(in Context) ([]game.Card, int) {
	s := in.State
	placed := map[string]bool{}
	for _, id := range s.TableOrder {
		placed[id] = true
		for _, d := range s.TableCards[id] {
			placed[d.ID] = true
		}
	}
	for _, c := range s.Player(in.BotID).Hand {
		placed[c.ID] = true
	}
	if s.TrumpCard != nil {
		placed[s.TrumpCard.ID] = true // drawn by somebody: it is in a hand or gone, the memory knows which
	}
	known := map[string]int{}
	var pool []game.Card
	for _, c := range game.NewDeck() {
		if placed[c.ID] || in.Memory.seen[c.ID] {
			continue
		}
		if who, ok := in.Memory.holder[c.ID]; ok {
			known[who]++
			continue
		}
		pool = append(pool, c)
	}
	slots := s.DeckCount // what is still in the deck is unknown too
	for _, p := range s.Players {
		if p.Out {
			continue
		}
		slots += p.StumpCount
		if p.ID != in.BotID {
			if n := p.HandCount - known[p.ID]; n > 0 {
				slots += n
			}
		}
	}
	return pool, slots
}

// deal completes the sanitized view into a full state: known cards go to
// their holders, the rest of the unknown pool is shuffled into the hidden
// hands and stumps. nil when the counts cannot be met.
func (h *Heuristic) deal(in Context, pool []game.Card) *game.GameState {
	s := in.State.Clone()
	cards := append([]game.Card{}, pool...)
	h.rng.Shuffle(len(cards), func(i, j int) { cards[i], cards[j] = cards[j], cards[i] })
	take := func(n int) []game.Card {
		if n > len(cards) {
			return nil
		}
		out := append([]game.Card{}, cards[:n]...)
		cards = cards[n:]
		return out
	}
	for i := range s.Players {
		p := &s.Players[i]
		if p.ID != in.BotID {
			p.Hand = []game.Card{}
			for id, who := range in.Memory.holder {
				if who == p.ID {
					if c, ok := game.CardByID(id); ok {
						p.Hand = append(p.Hand, c)
					}
				}
			}
			if need := p.HandCount - len(p.Hand); need > 0 {
				more := take(need)
				if more == nil {
					return nil
				}
				p.Hand = append(p.Hand, more...)
			}
		}
		stump := take(p.StumpCount)
		if stump == nil {
			return nil
		}
		p.Stump = stump
	}
	deck := take(s.DeckCount)
	if deck == nil {
		return nil
	}
	s.Deck = deck
	return s
}

type searcher struct {
	engine   *game.Engine
	bot      string
	deadline time.Time
	rng      *rand.Rand
	nodes    int
	playouts int
}

// outcome scores a finished game from the bot's seat: +1 out and not the
// fool, -1 the fool, 0 a draw.
func (sr *searcher) outcome(s *game.GameState) float64 {
	switch {
	case s.LoserID == sr.bot:
		return -1
	case s.LoserID == "":
		return 0
	}
	return 1
}

// eval scores an unfinished position: cards still to shed, mine against
// the average of the others.
func (sr *searcher) eval(s *game.GameState) float64 {
	mine, others, n := 0, 0, 0
	myQ, otherQ := 0.0, 0.0
	trump := s.ActiveTrump()
	for _, p := range s.Players {
		if p.Out {
			continue
		}
		cards := len(p.Hand) + len(p.Stump)
		q := 0.0
		if EvalQuality {
			for _, c := range p.Hand {
				q += float64(weight(c, trump))
			}
		}
		if p.ID == sr.bot {
			mine, myQ = cards, q
		} else {
			others += cards
			otherQ += q
			n++
		}
	}
	if n == 0 {
		return 0
	}
	v := (float64(others)/float64(n) - float64(mine)) / 12
	if EvalQuality {
		// what is left in the hands: the side holding the trumps and the
		// specials is the side that sheds its cards last and wins the bouts
		v += (myQ - otherQ/float64(n)) / 300
	}
	if v > 0.9 {
		v = 0.9
	}
	if v < -0.9 {
		v = -0.9
	}
	return v
}

// actor picks who moves next in a node: the bot when it may, else the
// first player who has to act.
func (sr *searcher) actor(s *game.GameState) string {
	actors := append([]string{}, s.Actors()...)
	actors = append(actors, s.StumpPending...)
	for _, id := range actors {
		if id == sr.bot {
			return id
		}
	}
	if len(actors) == 0 {
		return ""
	}
	return actors[0]
}

// negamax for two players: the bot maximises, the other minimises.
func (sr *searcher) negamax(s *game.GameState, depth int, alpha, beta float64) float64 {
	if s.Status == game.StatusFinished {
		return sr.outcome(s)
	}
	sr.nodes++
	if depth == 0 || sr.nodes > EndgameNodes || time.Now().After(sr.deadline) {
		return sr.eval(s)
	}
	id := sr.actor(s)
	if id == "" {
		return sr.eval(s)
	}
	moves := s.LegalMoves(id)
	if len(moves) == 0 {
		return sr.eval(s)
	}
	maximising := id == sr.bot
	best := -2.0
	if !maximising {
		best = 2
	}
	for _, m := range moves {
		child := s.Clone()
		if err := sr.engine.ApplyMove(child, id, m); err != nil {
			continue
		}
		v := sr.negamax(child, depth-1, alpha, beta)
		if maximising {
			if v > best {
				best = v
			}
			if best > alpha {
				alpha = best
			}
		} else {
			if v < best {
				best = v
			}
			if best < beta {
				beta = best
			}
		}
		if alpha >= beta {
			break
		}
	}
	if best == -2 || best == 2 {
		return sr.eval(s)
	}
	return best
}

// rollouts plays the position out with the rule-based brain for everybody
// a few times and averages the outcomes (three or more players).
func (sr *searcher) rollouts(s *game.GameState) float64 {
	total := 0.0
	n := sr.playouts
	if n <= 0 {
		n = EndgameRollouts
	}
	var mem *Memory
	if RolloutsCount {
		mem = fullMemory(s)
	}
	for r := 0; r < n; r++ {
		brain := NewHeuristic(rand.New(rand.NewPCG(sr.rng.Uint64(), sr.rng.Uint64())))
		cur := s.Clone()
		steps := 0
		for cur.Status == game.StatusPlaying && steps < EndgameRolloutLen && time.Now().Before(sr.deadline) {
			steps++
			id := sr.actor(cur)
			if id == "" {
				break
			}
			moves := cur.LegalMoves(id)
			if len(moves) == 0 {
				break
			}
			var m *Memory
			if mem != nil {
				m = mem
				mem.refresh(cur)
			}
			d, _ := brain.Decide(context.Background(), Context{State: cur.Sanitized(id), BotID: id, Moves: moves, Level: LevelNormal, Memory: m})
			if err := sr.engine.ApplyMove(cur, id, d.Move); err != nil {
				break
			}
		}
		if cur.Status == game.StatusFinished {
			total += sr.outcome(cur)
		} else {
			total += sr.eval(cur)
		}
	}
	return total / float64(n)
}

// fullMemory builds a memory that places every card of a complete state:
// inside a sampled world there is nothing unknown.
func fullMemory(s *game.GameState) *Memory {
	m := NewMemory()
	m.refresh(s)
	return m
}

// refresh re-reads the hands of a complete state into the memory's holder
// map (cards that left the hands are no longer placed).
func (m *Memory) refresh(s *game.GameState) {
	for id := range m.holder {
		delete(m.holder, id)
	}
	for _, p := range s.Players {
		for _, c := range p.Hand {
			m.holder[c.ID] = p.ID
		}
	}
}
