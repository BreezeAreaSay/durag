package store

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/breezeareasay/durag/backend/internal/game"
)

func stores(t *testing.T) map[string]Store {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return map[string]Store{
		"memory": NewMemory(),
		"redis":  NewRedisFromClient(client, "test", time.Hour),
	}
}

func TestLoadUpdateDelete(t *testing.T) {
	for name, st := range stores(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			if _, err := st.Load(ctx, "nope"); err != ErrNotFound {
				t.Fatalf("load missing: %v", err)
			}
			if _, err := st.Update(ctx, "nope", false, func(*game.GameState) error { return nil }); err != ErrNotFound {
				t.Fatalf("update missing without create: %v", err)
			}
			e := game.NewEngine(game.NewRand(1, 2))
			s, err := st.Update(ctx, "r1", true, func(s *game.GameState) error {
				return e.Join(s, "A", "Alice", "")
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(s.Players) != 1 || s.RoomID != "r1" {
				t.Fatalf("state %+v", s)
			}
			// a failing update is not persisted
			_, err = st.Update(ctx, "r1", false, func(s *game.GameState) error {
				_ = e.Join(s, "B", "Bob", "")
				return game.ErrNotPlaying
			})
			if err != game.ErrNotPlaying {
				t.Fatalf("expected rule error back, got %v", err)
			}
			loaded, err := st.Load(ctx, "r1")
			if err != nil {
				t.Fatal(err)
			}
			if len(loaded.Players) != 1 {
				t.Fatal("failed update leaked")
			}
			// mutating the returned copy must not affect the store
			loaded.Players[0].Name = "Mallory"
			again, _ := st.Load(ctx, "r1")
			if again.Players[0].Name != "Alice" {
				t.Fatal("Load must return a copy")
			}
			// a full game survives a round trip
			if _, err := st.Update(ctx, "r1", false, func(s *game.GameState) error {
				if err := e.Join(s, "B", "Bob", ""); err != nil {
					return err
				}
				return e.Start(s)
			}); err != nil {
				t.Fatal(err)
			}
			g, _ := st.Load(ctx, "r1")
			if g.Status != game.StatusPlaying || g.TrumpCard == nil || len(g.Deck) == 0 || len(g.TableCards) != 0 {
				t.Fatalf("round trip lost data: %+v", g)
			}
			if err := st.Delete(ctx, "r1"); err != nil {
				t.Fatal(err)
			}
			if _, err := st.Load(ctx, "r1"); err != ErrNotFound {
				t.Fatalf("after delete: %v", err)
			}
		})
	}
}

func TestConcurrentUpdatesAreSerialised(t *testing.T) {
	for name, st := range stores(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			const n = 25
			var wg sync.WaitGroup
			for i := 0; i < n; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, err := st.Update(ctx, "c", true, func(s *game.GameState) error {
						s.BoutNumber++
						return nil
					})
					if err != nil {
						t.Error(err)
					}
				}()
			}
			wg.Wait()
			s, err := st.Load(ctx, "c")
			if err != nil {
				t.Fatal(err)
			}
			if s.BoutNumber != n {
				t.Fatalf("lost updates: %d != %d", s.BoutNumber, n)
			}
		})
	}
}

func TestPublishSubscribe(t *testing.T) {
	for name, st := range stores(t) {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ch1, err := st.Subscribe(ctx, "room")
			if err != nil {
				t.Fatal(err)
			}
			ch2, err := st.Subscribe(ctx, "room")
			if err != nil {
				t.Fatal(err)
			}
			other, err := st.Subscribe(ctx, "other")
			if err != nil {
				t.Fatal(err)
			}
			if err := st.Publish(ctx, "room", []byte("hello")); err != nil {
				t.Fatal(err)
			}
			for _, ch := range []<-chan []byte{ch1, ch2} {
				select {
				case msg := <-ch:
					if string(msg) != "hello" {
						t.Fatalf("got %q", msg)
					}
				case <-time.After(2 * time.Second):
					t.Fatal("timeout waiting for message")
				}
			}
			select {
			case msg := <-other:
				t.Fatalf("other room received %q", msg)
			case <-time.After(50 * time.Millisecond):
			}
			cancel()
			select {
			case _, ok := <-ch1:
				if ok {
					t.Fatal("expected closed channel")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("channel not closed after cancel")
			}
		})
	}
}
