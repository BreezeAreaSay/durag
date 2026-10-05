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
	// Close releases resources.
	Close() error
}
