package bot

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"testing"
	"time"

	"github.com/breezeareasay/durag/backend/internal/game"
)

// playMatch plays one two-seat game: the bot "b1" at level a against the
// seat "h" at level b (both driven by the heuristic). It returns the loser.
func playMatch(seed uint64, a, b Level, mem bool) string {
	e := game.NewEngine(game.NewRand(seed, 11))
	s := game.NewGameState("STR")
	_ = e.AddBot(s, "b1", "b1", "")
	_ = e.Join(s, "h", "Human", "")
	_ = e.SetReady(s, "h", true)
	brainA := NewHeuristic(rand.New(rand.NewPCG(seed, 5)))
	brainB := NewHeuristic(rand.New(rand.NewPCG(seed, 7)))
	memory := NewMemory()
	steps := 0
	for s.Status == game.StatusPlaying && steps < 4000 {
		steps++
		e.ResolveIfDue(s, time.Now().Add(time.Hour))
		e.StumpsIfDue(s, time.Now().Add(time.Hour))
		if mem {
			memory.Observe(s)
		}
		actors := append([]string{}, s.Actors()...)
		actors = append(actors, s.StumpPending...)
		if len(actors) == 0 {
			continue
		}
		id := actors[steps%len(actors)]
		moves := s.LegalMoves(id)
		if len(moves) == 0 {
			continue
		}
		level, brain := a, brainA
		if id == "h" {
			level, brain = b, brainB
		}
		var snap *Memory
		if mem {
			snap = memory.Snapshot()
		}
		var d Decision
		if level == "random" {
			d = Decision{Move: moves[brain.rng.IntN(len(moves))]}
		} else {
			in := Context{State: s.Sanitized(id), BotID: id, Moves: moves, Level: level, Memory: snap, Plan: ChoosePlan(s.Sanitized(id), id)}
			d, _ = brain.Decide(context.Background(), in)
		}
		if err := e.ApplyMove(s, id, d.Move); err != nil {
			panic(fmt.Sprintf("seed %d: %v", seed, err))
		}
	}
	return s.LoserID
}

// TestLevelsDiffer pits the hard level against the easy one over a fixed
// set of deals: the sloppy player must lose clearly more often than not,
// and both must beat a player who moves at random.
func TestLevelsDiffer(t *testing.T) {
	const n = 120
	// a small search keeps the test quick; the forced-win test covers the full one
	samples, nodes, budget := EndgameSamples, EndgameNodes, EndgameBudget
	hs, hr, hb := HardSamples, HardRollouts, HardBudget
	EndgameSamples, EndgameNodes, EndgameBudget = 2, 300, 15*time.Millisecond
	HardSamples, HardRollouts, HardBudget = 2, 2, 15*time.Millisecond
	defer func() {
		EndgameSamples, EndgameNodes, EndgameBudget = samples, nodes, budget
		HardSamples, HardRollouts, HardBudget = hs, hr, hb
	}()
	lost := func(a, b Level) int {
		k := 0
		for seed := uint64(1); seed <= n; seed++ {
			if playMatch(seed, a, b, true) == "b1" {
				k++
			}
		}
		return k
	}
	if l := lost(LevelHard, LevelEasy); l > n*45/100 {
		t.Fatalf("hard lost %d of %d games to easy", l, n)
	}
	if l := lost(LevelEasy, "random"); l > n*30/100 {
		t.Fatalf("easy lost %d of %d games to a random player", l, n)
	}
	if os.Getenv("STRENGTH") != "" {
		for _, pair := range [][2]Level{{LevelHard, LevelEasy}, {LevelHard, LevelNormal}, {LevelNormal, LevelEasy}, {LevelHard, "random"}} {
			fmt.Printf("%s vs %s: %s loses %d/%d\n", pair[0], pair[1], pair[0], lost(pair[0], pair[1]), n)
		}
	}
}

func TestEndgameSearchExperiment(t *testing.T) {
	if os.Getenv("STRENGTH") == "" {
		t.Skip("set STRENGTH=1")
	}
	const n = 40
	run := func(label string) {
		lost := 0
		for seed := uint64(1); seed <= n; seed++ {
			if playMatch(seed, LevelHard, LevelEasy, true) == "b1" {
				lost++
			}
		}
		fmt.Printf("%s: hard loses %d/%d\n", label, lost, n)
	}
	samples := EndgameSamples
	EndgameSamples = 0
	run("no search")
	EndgameSamples = samples
	EndgameNegamax = true
	run("negamax")
	EndgameNegamax = false
	run("rollouts")
}
