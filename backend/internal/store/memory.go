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
	wins  map[string]int
	prof  map[string]Profile
}

// NewMemory creates an empty in-memory store.
func NewMemory() *Memory {
	return &Memory{
		rooms: map[string]*game.GameState{},
		locks: map[string]*sync.Mutex{},
		subs:  map[string]map[chan []byte]struct{}{},
		wins:  map[string]int{},
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

// Publish implements Store. The sends happen under the lock, which is what
// makes closing a subscriber's channel (also under the lock) safe: a timer
// or a bot may publish at the very moment the last client of a room leaves.
func (m *Memory) Publish(_ context.Context, roomID string, payload []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for ch := range m.subs[roomID] {
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
		close(ch) // under the lock: no Publish can be sending on it
		m.mu.Unlock()
	}()
	return ch, nil
}

// AddWin implements Store.
func (m *Memory) AddWin(_ context.Context, playerID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.wins[playerID]++
	return m.wins[playerID], nil
}

// Wins implements Store.
func (m *Memory) Wins(_ context.Context, playerID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.wins[playerID], nil
}

// Profile implements Store.
func (m *Memory) Profile(_ context.Context, playerID string) (Profile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.prof[playerID], nil
}

// SaveProfile implements Store.
func (m *Memory) SaveProfile(_ context.Context, playerID string, p Profile) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.prof == nil {
		m.prof = map[string]Profile{}
	}
	m.prof[playerID] = p
	return nil
}

// Close implements Store.
func (m *Memory) Close() error { return nil }

// Rooms returns the number of stored rooms (for tests and metrics).
func (m *Memory) Rooms() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.rooms)
}
