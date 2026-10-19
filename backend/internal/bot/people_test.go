package bot

import (
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/breezeareasay/durag/backend/internal/game"
)

func TestGreetUsesTheDossier(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 1))
	stranger := Greet(rng, Dossier{Name: "Вася"})
	if !strings.Contains(stranger, "Вася") {
		t.Fatalf("stranger greeting without the name: %q", stranger)
	}
	worst := 0
	for i := 0; i < 40; i++ {
		line := Greet(rng, Dossier{Name: "Вася", Games: 5, Losses: 1, WorstTake: 11})
		if strings.Contains(line, "11") {
			worst++
		}
		if !strings.Contains(line, "Вася") {
			t.Fatalf("greeting without the name: %q", line)
		}
	}
	if worst == 0 {
		t.Fatal("a record take of 11 cards was never mentioned in 40 greetings")
	}
	scar := 0
	for i := 0; i < 40; i++ {
		if strings.Contains(Greet(rng, Dossier{Name: "Вася", Games: 2, Losses: 1, Scar: true}), "клейм") || strings.Contains(Greet(rng, Dossier{Name: "Вася", Games: 2, Losses: 1, Scar: true}), "Дурак") {
			scar++
		}
	}
	if scar == 0 {
		t.Fatal("the scar was never mentioned")
	}
}

func TestHabitFiresOnlyOnTheFavouriteSuit(t *testing.T) {
	rng := rand.New(rand.NewPCG(2, 2))
	d := Dossier{Name: "Вася", FavSuit: game.SuitHearts}
	if Habit(rng, d, game.SuitSpades) != "" {
		t.Fatal("a habit line for the wrong suit")
	}
	line := Habit(rng, d, game.SuitHearts)
	if !strings.Contains(line, "червей") || !strings.Contains(line, "Вася") {
		t.Fatalf("habit line: %q", line)
	}
	if Habit(rng, Dossier{Name: "Вася"}, game.SuitHearts) != "" {
		t.Fatal("a habit line without a habit")
	}
}

func TestRetortNamesTheSpeaker(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 3))
	for i := 0; i < 20; i++ {
		if line := Retort(rng, "Шуруп", 7); !strings.Contains(line, "Шуруп") {
			t.Fatalf("retort without the speaker: %q", line)
		}
	}
}

func TestPromoteAndPromo(t *testing.T) {
	if Promote(LevelEasy) != LevelNormal || Promote(LevelNormal) != LevelHard || Promote(LevelHard) != LevelHard {
		t.Fatal("promotion order")
	}
	rng := rand.New(rand.NewPCG(4, 4))
	if Promo(rng, LevelHard) == "" || Promo(rng, LevelNormal) == "" {
		t.Fatal("promoted bots must have something to say")
	}
	if Promo(rng, LevelEasy) != "" {
		t.Fatal("nobody is promoted to easy")
	}
}

func TestDossierStringMentionsWhatMatters(t *testing.T) {
	if (Dossier{Name: "x"}).String() != "" {
		t.Fatal("a stranger has no dossier")
	}
	s := Dossier{Name: "Вася", Games: 9, Losses: 4, WorstTake: 11, FavSuit: game.SuitClubs, Scar: true}.String()
	for _, want := range []string{"9 игр", "4 проигрыш", "11 карт", "крестей", "клеймо"} {
		if !strings.Contains(s, want) {
			t.Fatalf("dossier %q lacks %q", s, want)
		}
	}
}
