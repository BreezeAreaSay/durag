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
	// Close releases resources.
	Close() error
}
