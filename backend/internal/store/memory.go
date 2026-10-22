package store

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/breezeareasay/durag/backend/internal/game"
)

// Memory is an in-process Store. It is used by tests and when REDIS_ADDR is
// empty (single backend instance, state lost on restart).
type Memory struct {
	mu    sync.Mutex
	rooms map[string]*game.GameState
	locks map[string]*sync.Mutex
	subs  map[string]map[chan []byte]struct{}
	users map[string]UserStats
	games map[string]bool
}

// NewMemory creates an empty in-memory store.
func NewMemory() *Memory {
	return &Memory{
		rooms: map[string]*game.GameState{},
		locks: map[string]*sync.Mutex{},
		subs:  map[string]map[chan []byte]struct{}{},
		users: map[string]UserStats{},
		games: map[string]bool{},
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

// ListRooms implements Store.
func (m *Memory) ListRooms(_ context.Context) ([]*game.GameState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*game.GameState, 0, len(m.rooms))
	for _, s := range m.rooms {
		out = append(out, s.Clone())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out, nil
}

// UserStats implements Store.
func (m *Memory) UserStats(_ context.Context, userID string) (UserStats, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u := m.users[userID]
	u.ID = userID
	return u, nil
}

// RecordGame implements Store.
func (m *Memory) RecordGame(_ context.Context, key string, results []GameResult) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.games[key] {
		return false, nil
	}
	m.games[key] = true
	now := time.Now().UnixMilli()
	for _, r := range results {
		u := m.users[r.ID]
		u.ID = r.ID
		if r.Name != "" {
			u.Name = r.Name
		}
		u.Games++
		if r.Won {
			u.Wins++
		} else {
			u.Losses++
		}
		u.XP += XPFor(r)
		u.LastSeen = now
		m.users[r.ID] = u
	}
	return true, nil
}

// TopUsers implements Store.
func (m *Memory) TopUsers(_ context.Context, n int) ([]UserStats, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]UserStats, 0, len(m.users))
	for _, u := range m.users {
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].XP != out[j].XP {
			return out[i].XP > out[j].XP
		}
		return out[i].ID < out[j].ID
	})
	if n > 0 && len(out) > n {
		out = out[:n]
	}
	return out, nil
}

// Close implements Store.
func (m *Memory) Close() error { return nil }

// Rooms returns the number of stored rooms (for tests and metrics).
func (m *Memory) Rooms() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.rooms)
}
