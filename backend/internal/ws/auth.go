package ws

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/breezeareasay/durag/backend/internal/telegram"
)

// Identity is who a connection belongs to.
type Identity struct {
	ID        string
	Name      string
	AvatarURL string
}

// Authenticator turns a JOIN_ROOM payload into an Identity.
type Authenticator interface {
	Authenticate(p JoinPayload) (Identity, error)
}

// ErrUnauthorized is returned when neither Telegram nor dev credentials are acceptable.
var ErrUnauthorized = errors.New("unauthorized")

// TelegramAuth validates Telegram initData (HMAC-SHA256 with the bot token)
// and optionally accepts development identities.
type TelegramAuth struct {
	BotToken string
	MaxAge   time.Duration
	AllowDev bool
}

var devIDPattern = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// Authenticate implements Authenticator.
func (a TelegramAuth) Authenticate(p JoinPayload) (Identity, error) {
	if strings.TrimSpace(p.TgInitData) != "" {
		if a.BotToken == "" {
			return Identity{}, errors.New("telegram login is not configured on the server")
		}
		d, err := telegram.Validate(p.TgInitData, a.BotToken, a.MaxAge)
		if err != nil {
			return Identity{}, err
		}
		if d.User == nil {
			return Identity{}, errors.New("telegram init data has no user")
		}
		return Identity{
			ID:        strconv.FormatInt(d.User.ID, 10),
			Name:      d.DisplayName(),
			AvatarURL: d.User.PhotoURL,
		}, nil
	}
	if a.AllowDev && p.DevUser != nil && strings.TrimSpace(p.DevUser.ID) != "" {
		id := devIDPattern.ReplaceAllString(strings.TrimSpace(p.DevUser.ID), "")
		if id == "" {
			return Identity{}, ErrUnauthorized
		}
		if len(id) > 32 {
			id = id[:32]
		}
		name := strings.TrimSpace(p.DevUser.Name)
		if name == "" {
			name = "Guest " + id
		}
		if len(name) > 32 {
			name = name[:32]
		}
		return Identity{ID: "dev:" + id, Name: name, AvatarURL: p.DevUser.AvatarURL}, nil
	}
	return Identity{}, ErrUnauthorized
}
