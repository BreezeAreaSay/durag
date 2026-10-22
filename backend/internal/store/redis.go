package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/breezeareasay/durag/backend/internal/game"
)

// Redis is a Store backed by Redis: room state lives in a JSON string with a
// TTL, updates are serialised with a short-lived lock key and change
// notifications travel over Redis Pub/Sub so that every backend instance
// sees them.
type Redis struct {
	client  *redis.Client
	prefix  string
	ttl     time.Duration
	lockTTL time.Duration
	lockMax time.Duration
}

// RedisOptions configures NewRedis.
type RedisOptions struct {
	Addr     string
	Password string
	DB       int
	Prefix   string        // key prefix, default "durag"
	RoomTTL  time.Duration // default 24h
}

// ErrLockTimeout is returned when a room lock could not be acquired in time.
var ErrLockTimeout = errors.New("store: could not acquire room lock")

// NewRedis connects to Redis and pings it.
func NewRedis(ctx context.Context, opts RedisOptions) (*Redis, error) {
	if opts.Prefix == "" {
		opts.Prefix = "durag"
	}
	if opts.RoomTTL <= 0 {
		opts.RoomTTL = 24 * time.Hour
	}
	client := redis.NewClient(&redis.Options{Addr: opts.Addr, Password: opts.Password, DB: opts.DB})
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("store: redis ping: %w", err)
	}
	return &Redis{
		client:  client,
		prefix:  opts.Prefix,
		ttl:     opts.RoomTTL,
		lockTTL: 5 * time.Second,
		lockMax: 3 * time.Second,
	}, nil
}

// NewRedisFromClient wraps an existing client (tests).
func NewRedisFromClient(client *redis.Client, prefix string, ttl time.Duration) *Redis {
	if prefix == "" {
		prefix = "durag"
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &Redis{client: client, prefix: prefix, ttl: ttl, lockTTL: 5 * time.Second, lockMax: 3 * time.Second}
}

func (r *Redis) roomKey(id string) string { return r.prefix + ":room:" + id }
func (r *Redis) userKey(id string) string { return r.prefix + ":user:" + id }
func (r *Redis) gameKey(k string) string  { return r.prefix + ":game:" + k }
func (r *Redis) xpKey() string            { return r.prefix + ":xp" }
func (r *Redis) lockKey(id string) string { return r.prefix + ":lock:" + id }
func (r *Redis) channel(id string) string { return r.prefix + ":events:" + id }
func (r *Redis) Client() *redis.Client    { return r.client }

// Load implements Store.
func (r *Redis) Load(ctx context.Context, roomID string) (*game.GameState, error) {
	raw, err := r.client.Get(ctx, r.roomKey(roomID)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var s game.GameState
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("store: decode room %s: %w", roomID, err)
	}
	normalize(&s)
	return &s, nil
}

// releaseScript deletes the lock only if we still own it.
var releaseScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0`)

func (r *Redis) acquire(ctx context.Context, roomID string) (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf)
	deadline := time.Now().Add(r.lockMax)
	wait := 5 * time.Millisecond
	for {
		ok, err := r.client.SetNX(ctx, r.lockKey(roomID), token, r.lockTTL).Result()
		if err != nil {
			return "", err
		}
		if ok {
			return token, nil
		}
		if time.Now().After(deadline) {
			return "", ErrLockTimeout
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(wait):
		}
		if wait < 50*time.Millisecond {
			wait *= 2
		}
	}
}

func (r *Redis) release(ctx context.Context, roomID, token string) {
	_ = releaseScript.Run(ctx, r.client, []string{r.lockKey(roomID)}, token).Err()
}

// Update implements Store.
func (r *Redis) Update(ctx context.Context, roomID string, create bool, fn UpdateFunc) (*game.GameState, error) {
	token, err := r.acquire(ctx, roomID)
	if err != nil {
		return nil, err
	}
	defer r.release(ctx, roomID, token)

	s, err := r.Load(ctx, roomID)
	if errors.Is(err, ErrNotFound) {
		if !create {
			return nil, ErrNotFound
		}
		s = game.NewGameState(roomID)
	} else if err != nil {
		return nil, err
	}
	if err := fn(s); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	if err := r.client.Set(ctx, r.roomKey(roomID), raw, r.ttl).Err(); err != nil {
		return nil, err
	}
	return s.Clone(), nil
}

// ListRooms implements Store: every room key, newest first.
func (r *Redis) ListRooms(ctx context.Context) ([]*game.GameState, error) {
	var keys []string
	iter := r.client.Scan(ctx, 0, r.prefix+":room:*", 200).Iterator()
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}
	out := make([]*game.GameState, 0, len(keys))
	if len(keys) == 0 {
		return out, nil
	}
	values, err := r.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}
	for _, v := range values {
		raw, ok := v.(string)
		if !ok {
			continue
		}
		var s game.GameState
		if err := json.Unmarshal([]byte(raw), &s); err != nil {
			continue
		}
		normalize(&s)
		out = append(out, &s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out, nil
}

// UserStats implements Store.
func (r *Redis) UserStats(ctx context.Context, userID string) (UserStats, error) {
	h, err := r.client.HGetAll(ctx, r.userKey(userID)).Result()
	if err != nil {
		return UserStats{}, err
	}
	return statsFromHash(userID, h), nil
}

func statsFromHash(id string, h map[string]string) UserStats {
	atoi := func(k string) int {
		n, _ := strconv.Atoi(h[k])
		return n
	}
	last, _ := strconv.ParseInt(h["last_seen"], 10, 64)
	return UserStats{ID: id, Name: h["name"], Games: atoi("games"), Wins: atoi("wins"), Losses: atoi("losses"), XP: atoi("xp"), LastSeen: last}
}

// RecordGame implements Store.
func (r *Redis) RecordGame(ctx context.Context, key string, results []GameResult) (bool, error) {
	ok, err := r.client.SetNX(ctx, r.gameKey(key), "1", 7*24*time.Hour).Result()
	if err != nil || !ok {
		return false, err
	}
	now := time.Now().UnixMilli()
	pipe := r.client.TxPipeline()
	for _, res := range results {
		k := r.userKey(res.ID)
		pipe.HIncrBy(ctx, k, "games", 1)
		if res.Won {
			pipe.HIncrBy(ctx, k, "wins", 1)
		} else {
			pipe.HIncrBy(ctx, k, "losses", 1)
		}
		pipe.HIncrBy(ctx, k, "xp", int64(XPFor(res)))
		fields := map[string]any{"last_seen": now}
		if res.Name != "" {
			fields["name"] = res.Name
		}
		pipe.HSet(ctx, k, fields)
		pipe.ZIncrBy(ctx, r.xpKey(), float64(XPFor(res)), res.ID)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return false, err
	}
	return true, nil
}

// TopUsers implements Store.
func (r *Redis) TopUsers(ctx context.Context, n int) ([]UserStats, error) {
	if n <= 0 {
		n = 50
	}
	ids, err := r.client.ZRevRange(ctx, r.xpKey(), 0, int64(n-1)).Result()
	if err != nil {
		return nil, err
	}
	out := make([]UserStats, 0, len(ids))
	for _, id := range ids {
		h, err := r.client.HGetAll(ctx, r.userKey(id)).Result()
		if err != nil {
			return nil, err
		}
		out = append(out, statsFromHash(id, h))
	}
	return out, nil
}

// Delete implements Store.
func (r *Redis) Delete(ctx context.Context, roomID string) error {
	return r.client.Del(ctx, r.roomKey(roomID)).Err()
}

// Publish implements Store.
func (r *Redis) Publish(ctx context.Context, roomID string, payload []byte) error {
	return r.client.Publish(ctx, r.channel(roomID), payload).Err()
}

// Subscribe implements Store.
func (r *Redis) Subscribe(ctx context.Context, roomID string) (<-chan []byte, error) {
	ps := r.client.Subscribe(ctx, r.channel(roomID))
	// Wait for the subscription to be confirmed so that no message published
	// right after Subscribe returns is lost.
	if _, err := ps.Receive(ctx); err != nil {
		_ = ps.Close()
		return nil, err
	}
	out := make(chan []byte, 256)
	in := ps.Channel()
	go func() {
		defer close(out)
		defer ps.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-in:
				if !ok {
					return
				}
				select {
				case out <- []byte(msg.Payload):
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

// Close implements Store.
func (r *Redis) Close() error { return r.client.Close() }

// normalize replaces nil slices/maps that JSON decoding may produce so the
// engine never has to nil-check them.
func normalize(s *game.GameState) {
	if s.Players == nil {
		s.Players = []game.Player{}
	}
	for i := range s.Players {
		if s.Players[i].Hand == nil {
			s.Players[i].Hand = []game.Card{}
		}
		if s.Players[i].Stump == nil {
			s.Players[i].Stump = []game.Card{}
		}
	}
	if s.Deck == nil {
		s.Deck = []game.Card{}
	}
	if s.TableCards == nil {
		s.TableCards = map[string][]game.Card{}
	}
	if s.TableOrder == nil {
		s.TableOrder = []string{}
	}
	if s.FinishedOrder == nil {
		s.FinishedOrder = []string{}
	}
	if s.StumpPending == nil {
		s.StumpPending = []string{}
	}
	if s.TurnActors == nil {
		s.TurnActors = []string{}
	}
}
