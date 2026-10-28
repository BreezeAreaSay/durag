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

func TestTheTableRespectsWinners(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 3))
	streak := Dossier{Name: "Вика", Games: 6, Wins: 4, Losses: 1, Streak: 3, BestStreak: 3}
	if !streak.Respected() {
		t.Fatal("three in a row deserves respect")
	}
	s := streak.String()
	for _, want := range []string{"4 побед", "3 раз подряд", "17%", "уважают"} {
		if !strings.Contains(s, want) {
			t.Fatalf("dossier %q lacks %q", s, want)
		}
	}
	mentioned := 0
	for i := 0; i < 40; i++ {
		line := GreetAs(nil, rng, streak)
		if strings.Contains(line, "3") {
			mentioned++
		}
	}
	if mentioned < 20 {
		t.Fatalf("a streak of 3 was mentioned only %d times in 40 greetings", mentioned)
	}
	record := Dossier{Name: "Гоша", Games: 10, Wins: 7, Losses: 1}
	if !record.Respected() {
		t.Fatal("seven of ten deserves respect")
	}
	stats := 0
	for i := 0; i < 40; i++ {
		line := GreetAs(nil, rng, record)
		if strings.Contains(line, "10%") || strings.Contains(line, "7 ") {
			stats++
		}
	}
	if stats < 15 {
		t.Fatalf("a record was mentioned only %d times in 40 greetings", stats)
	}
	if (Dossier{Name: "x", Games: 6, Wins: 1, Losses: 3}).Respected() {
		t.Fatal("one of six is nothing to respect")
	}
}

func TestFinishLinesCountTheWinJustScored(t *testing.T) {
	rng := rand.New(rand.NewPCG(4, 4))
	// two wins on record, this one makes three: the streak line counts it
	line := Finish(rng, Dossier{Name: "Вика", Games: 4, Wins: 2, Streak: 2}, Dossier{Name: "Вася", Games: 4, Losses: 2}, false, false)
	if !strings.Contains(line, "Вика") || !strings.Contains(line, "3") {
		t.Fatalf("streak line: %q", line)
	}
	// the fool had a run going: that is the story
	line = Finish(rng, Dossier{Name: "Вася"}, Dossier{Name: "Вика", Games: 5, Wins: 4, Streak: 4}, false, false)
	if !strings.Contains(line, "Вика") || !strings.Contains(line, "4") {
		t.Fatalf("broken streak line: %q", line)
	}
	// a first win gets a word, a bot's win none
	if line = Finish(rng, Dossier{Name: "Гоша"}, Dossier{Name: "Вася"}, false, false); !strings.Contains(line, "Гоша") {
		t.Fatalf("first win: %q", line)
	}
	if line = Finish(rng, Dossier{Name: "Крест"}, Dossier{Name: "Вася"}, true, false); line != "" {
		t.Fatalf("a bot's own win is not an occasion: %q", line)
	}
	// the speaker is the fool: it owns up sometimes
	owned := 0
	for i := 0; i < 30; i++ {
		if l := Finish(rng, Dossier{Name: "Гоша", Wins: 2, Games: 3}, Dossier{Name: "Крест"}, false, true); strings.Contains(l, "Я дурак") || strings.Contains(l, "Дурак — я") || strings.Contains(l, "клеймо") {
			owned++
		}
	}
	if owned == 0 {
		t.Fatal("the fool never owned up in 30 games")
	}
}
