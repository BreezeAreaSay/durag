// Package store persists room state and fans out change notifications.
//
// Two implementations exist: an in-memory one (tests, single-process dev) and
// a Redis one (production, any number of backend instances). The WebSocket
// hub only ever talks to the Store interface.
package store

import (
	"context"
	"errors"

	"github.com/breezeareasay/durag/backend/internal/game"
)

// ErrNotFound is returned when a room does not exist.
var ErrNotFound = errors.New("store: room not found")

// UpdateFunc mutates a room state in place. Returning an error aborts the
// update: nothing is persisted and the error is passed through.
type UpdateFunc func(s *game.GameState) error

// UserStats is a player's lifetime record. XP unlocks the reactions with
// effects: 1 point per finished game, one more for not being the loser.
type UserStats struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Games    int    `json:"games"`
	Wins     int    `json:"wins"`
	Losses   int    `json:"losses"`
	XP       int    `json:"xp"`
	LastSeen int64  `json:"last_seen"` // unix milliseconds
}

// GameResult is one player's outcome of a finished game.
type GameResult struct {
	ID   string
	Name string
	Won  bool // everybody but the loser wins; in a draw everybody does
}

// XPFor returns the experience a result is worth.
func XPFor(r GameResult) int {
	if r.Won {
		return 2
	}
	return 1
}

// Store is the persistence and messaging layer for rooms.
type Store interface {
	// Load returns a copy of the room state or ErrNotFound.
	Load(ctx context.Context, roomID string) (*game.GameState, error)
	// Update runs fn on the current state under a per-room lock and persists
	// the result. With create=true a missing room is created first; otherwise
	// ErrNotFound is returned. The returned state is a private copy.
	Update(ctx context.Context, roomID string, create bool, fn UpdateFunc) (*game.GameState, error)
	// Delete removes a room.
	Delete(ctx context.Context, roomID string) error
	// Publish broadcasts a payload to every subscriber of the room (on every
	// backend instance).
	Publish(ctx context.Context, roomID string, payload []byte) error
	// Subscribe returns a channel of payloads published for the room. The
	// channel is closed when ctx is cancelled.
	Subscribe(ctx context.Context, roomID string) (<-chan []byte, error)
	// ListRooms returns copies of every stored room (monitoring).
	ListRooms(ctx context.Context) ([]*game.GameState, error)
	// UserStats returns a player's record (the zero value for a stranger).
	UserStats(ctx context.Context, userID string) (UserStats, error)
	// RecordGame applies the results of one finished game exactly once per
	// key (room id + state version); it reports whether anything was applied.
	RecordGame(ctx context.Context, key string, results []GameResult) (bool, error)
	// TopUsers lists the n best players by XP.
	TopUsers(ctx context.Context, n int) ([]UserStats, error)
	// AddWin increments a player's lifetime win counter and returns the new value.
	AddWin(ctx context.Context, playerID string) (int, error)
	// AddXP credits a player with n experience points (a caught fly) and
	// returns the new total.
	AddXP(ctx context.Context, playerID string, n int) (int, error)
	// Wins returns a player's lifetime win counter (0 for an unknown player).
	Wins(ctx context.Context, playerID string) (int, error)
	// Profile returns what the table remembers about a player (zero for a stranger).
	Profile(ctx context.Context, playerID string) (Profile, error)
	// SaveProfile stores a player's profile.
	SaveProfile(ctx context.Context, playerID string, p Profile) error
	// Close releases resources.
	Close() error
}

// Profile is what the table remembers about a person between games: the
// bots read it to greet and to needle, the scar is shown on the sticker.
type Profile struct {
	Games     int            `json:"games"`
	Losses    int            `json:"losses"`
	Took      int            `json:"took"`       // cards taken, lifetime
	WorstTake int            `json:"worst_take"` // the biggest single take ever
	Leads     map[string]int `json:"leads"`      // opening attacks by suit, lifetime
	Scar      bool           `json:"scar"`       // lost the last game they finished
	LastSeen  int64          `json:"last_seen"`  // unix seconds
}

// Fold adds one finished game to the profile.
func (p *Profile) Fold(stats game.Stats, lost, won bool, now int64) {
	p.Games++
	if lost {
		p.Losses++
		p.Scar = true
	}
	if won {
		p.Scar = false
	}
	p.Took += stats.Took
	if stats.WorstTake > p.WorstTake {
		p.WorstTake = stats.WorstTake
	}
	if p.Leads == nil {
		p.Leads = map[string]int{}
	}
	for suit, n := range stats.Leads {
		p.Leads[suit] += n
	}
	p.LastSeen = now
}

// FavouriteSuit is the suit the player opens with most, "" when there is no clear habit.
func (p Profile) FavouriteSuit() string {
	best, n, total := "", 0, 0
	for suit, c := range p.Leads {
		total += c
		if c > n {
			best, n = suit, c
		}
	}
	if total < 6 || n*2 < total { // a habit is at least half of a handful of leads
		return ""
	}
	return best
}
