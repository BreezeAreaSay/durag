package store

import (
	"context"
	"sync"

	"github.com/breezeareasay/durag/backend/internal/game"
)

// Memory is an in-process Store. It is used by tests and when REDIS_ADDR is
// empty (single backend instance, state lost on restart).
type Memory struct {
	mu    sync.Mutex
	rooms map[string]*game.GameState
	locks map[string]*sync.Mutex
	subs  map[string]map[chan []byte]struct{}
}

// NewMemory creates an empty in-memory store.
func NewMemory() *Memory {
	return &Memory{
		rooms: map[string]*game.GameState{},
		locks: map[string]*sync.Mutex{},
		subs:  map[string]map[chan []byte]struct{}{},
	}
}

func (m *Memory) roomLock(roomID string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.locks[roomID]
	if !ok {
		l = &sync.Mutex{}
		m.locks[roomID] = l
	}
	return l
}

// Load implements Store.
func (m *Memory) Load(_ context.Context, roomID string) (*game.GameState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.rooms[roomID]
	if !ok {
		return nil, ErrNotFound
	}
	return s.Clone(), nil
}

// Update implements Store.
func (m *Memory) Update(_ context.Context, roomID string, create bool, fn UpdateFunc) (*game.GameState, error) {
	l := m.roomLock(roomID)
	l.Lock()
	defer l.Unlock()

	m.mu.Lock()
	s, ok := m.rooms[roomID]
	m.mu.Unlock()
	if !ok {
		if !create {
			return nil, ErrNotFound
		}
		s = game.NewGameState(roomID)
	}
	work := s.Clone()
	if err := fn(work); err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.rooms[roomID] = work
	m.mu.Unlock()
	return work.Clone(), nil
}

// Delete implements Store.
func (m *Memory) Delete(_ context.Context, roomID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rooms, roomID)
	return nil
}

// Publish implements Store.
func (m *Memory) Publish(_ context.Context, roomID string, payload []byte) error {
	m.mu.Lock()
	targets := make([]chan []byte, 0, len(m.subs[roomID]))
	for ch := range m.subs[roomID] {
		targets = append(targets, ch)
	}
	m.mu.Unlock()
	for _, ch := range targets {
		msg := append([]byte(nil), payload...)
		select {
		case ch <- msg:
		default:
			// A subscriber that is not draining its channel is dropped from
			// this message rather than blocking the publisher.
		}
	}
	return nil
}

// Subscribe implements Store.
func (m *Memory) Subscribe(ctx context.Context, roomID string) (<-chan []byte, error) {
	ch := make(chan []byte, 256)
	m.mu.Lock()
	if m.subs[roomID] == nil {
		m.subs[roomID] = map[chan []byte]struct{}{}
	}
	m.subs[roomID][ch] = struct{}{}
	m.mu.Unlock()
	go func() {
		<-ctx.Done()
		m.mu.Lock()
		delete(m.subs[roomID], ch)
		if len(m.subs[roomID]) == 0 {
			delete(m.subs, roomID)
		}
		m.mu.Unlock()
		close(ch)
	}()
	return ch, nil
}

// Close implements Store.
func (m *Memory) Close() error { return nil }

// Rooms returns the number of stored rooms (for tests and metrics).
func (m *Memory) Rooms() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.rooms)
}
