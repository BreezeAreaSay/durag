package store

import (
	"context"
	"testing"

	"github.com/breezeareasay/durag/backend/internal/game"
)

func TestProfileFoldAndFavouriteSuit(t *testing.T) {
	var p Profile
	p.Fold(game.Stats{Took: 7, WorstTake: 7, Leads: map[string]int{game.SuitHearts: 3, game.SuitSpades: 1}}, true, false, 100)
	if p.Games != 1 || p.Losses != 1 || !p.Scar || p.Took != 7 || p.WorstTake != 7 {
		t.Fatalf("after a loss: %+v", p)
	}
	if p.FavouriteSuit() != "" {
		t.Fatalf("four leads are not a habit yet: %q", p.FavouriteSuit())
	}
	p.Fold(game.Stats{Took: 2, WorstTake: 2, Leads: map[string]int{game.SuitHearts: 3}}, false, true, 200)
	if p.Games != 2 || p.Losses != 1 || p.Scar || p.Took != 9 || p.WorstTake != 7 || p.LastSeen != 200 {
		t.Fatalf("after a win: %+v", p)
	}
	if p.FavouriteSuit() != game.SuitHearts {
		t.Fatalf("six of seven leads with hearts is a habit, got %q", p.FavouriteSuit())
	}
	// a draw-ish game: neither lost nor won keeps the scar as it was
	p.Fold(game.Stats{}, false, false, 300)
	if p.Scar {
		t.Fatal("a game neither lost nor won must not mark")
	}
}

func TestMemoryProfilesRoundTrip(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	p, err := m.Profile(ctx, "nobody")
	if err != nil || p.Games != 0 {
		t.Fatalf("stranger: %+v %v", p, err)
	}
	if err := m.SaveProfile(ctx, "a", Profile{Games: 3, Scar: true}); err != nil {
		t.Fatal(err)
	}
	p, _ = m.Profile(ctx, "a")
	if p.Games != 3 || !p.Scar {
		t.Fatalf("round trip: %+v", p)
	}
}
