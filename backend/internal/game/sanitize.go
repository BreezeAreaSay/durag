package game

// Sanitized returns a deep copy of the state that is safe to send to the
// given player: the deck, every stump (including the viewer's own — it is
// hidden by definition) and all other players' hands are replaced by counts,
// and the trump stays secret until somebody has drawn it.
func (s *GameState) Sanitized(viewerID string) *GameState {
	out := s.Clone()
	out.DeckCount = len(s.Deck)
	out.Deck = []Card{}
	if !s.TrumpRevealed {
		out.TrumpCard = nil
		out.TrumpSuit = ""
		out.TrumpDrawnBy = ""
	}
	for i := range out.Players {
		p := &out.Players[i]
		p.HandCount = len(p.Hand)
		p.StumpCount = len(p.Stump)
		p.Stump = []Card{}
		if p.ID != viewerID {
			p.Hand = []Card{}
		}
	}
	out.ViewerID = viewerID
	return out
}
