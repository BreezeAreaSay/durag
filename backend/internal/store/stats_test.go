package store

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/breezeareasay/durag/backend/internal/game"
)

func statsStores(t *testing.T) map[string]Store {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { client.Close() })
	return map[string]Store{"memory": NewMemory(), "redis": NewRedisFromClient(client, "test", time.Hour)}
}

func TestRecordGameIsIdempotentAndRanks(t *testing.T) {
	for name, st := range statsStores(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			results := []GameResult{{ID: "1", Name: "Alice", Won: true}, {ID: "2", Name: "Bob", Won: false}, {ID: "3", Name: "Cid", Won: true}}
			applied, err := st.RecordGame(ctx, "ROOM:7", results)
			if err != nil || !applied {
				t.Fatalf("first record: %v %v", applied, err)
			}
			applied, err = st.RecordGame(ctx, "ROOM:7", results)
			if err != nil || applied {
				t.Fatalf("second record with the same key must be a no-op: %v %v", applied, err)
			}
			if _, err := st.RecordGame(ctx, "ROOM:9", []GameResult{{ID: "2", Name: "Bob", Won: true}}); err != nil {
				t.Fatal(err)
			}
			a, _ := st.UserStats(ctx, "1")
			b, _ := st.UserStats(ctx, "2")
			z, _ := st.UserStats(ctx, "nobody")
			if a.XP != 2 || a.Games != 1 || a.Wins != 1 || a.Name != "Alice" || a.LastSeen == 0 {
				t.Fatalf("alice: %+v", a)
			}
			if b.XP != 3 || b.Games != 2 || b.Wins != 1 || b.Losses != 1 {
				t.Fatalf("bob: %+v", b)
			}
			if z.XP != 0 || z.ID != "nobody" {
				t.Fatalf("stranger: %+v", z)
			}
			top, err := st.TopUsers(ctx, 2)
			if err != nil || len(top) != 2 || top[0].ID != "2" || top[1].XP != 2 {
				t.Fatalf("top: %+v %v", top, err)
			}
		})
	}
}

func TestListRooms(t *testing.T) {
	for name, st := range statsStores(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			for _, id := range []string{"A1", "B2"} {
				if _, err := st.Update(ctx, id, true, func(s *game.GameState) error { return nil }); err != nil {
					t.Fatal(err)
				}
			}
			rooms, err := st.ListRooms(ctx)
			if err != nil || len(rooms) != 2 {
				t.Fatalf("rooms: %d %v", len(rooms), err)
			}
			ids := map[string]bool{rooms[0].RoomID: true, rooms[1].RoomID: true}
			if !ids["A1"] || !ids["B2"] {
				t.Fatalf("ids: %v", ids)
			}
			if err := st.Delete(ctx, "A1"); err != nil {
				t.Fatal(err)
			}
			if rooms, _ := st.ListRooms(ctx); len(rooms) != 1 {
				t.Fatalf("after delete: %d", len(rooms))
			}
		})
	}
}
