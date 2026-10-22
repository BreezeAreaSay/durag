// Package ws implements the WebSocket transport: clients send intents, the
// server validates them against the authoritative state and pushes
// sanitized STATE_UPDATE messages to every connection in the room.
package ws

import "encoding/json"

// Envelope is the wire format in both directions: {"type": ..., "payload": ...}.
type Envelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Client -> server intents.
const (
	TypeJoinRoom     = "JOIN_ROOM"
	TypePlayCard     = "PLAY_CARD"
	TypeTransferTurn = "TRANSFER_TURN"
	TypeTakeCards    = "TAKE_CARDS"
	TypePass         = "PASS"         // extension: "I have nothing more to throw in"
	TypeReady        = "READY"        // extension: lobby readiness toggle
	TypeLeaveRoom    = "LEAVE_ROOM"   // extension
	TypePing         = "PING"         // extension: application level keep-alive
	TypeReact        = "REACT"        // emoji reaction (alias of SEND_EMOJI)
	TypeSendEmoji    = "SEND_EMOJI"   // v10 name of REACT
	TypeTakeStump    = "TAKE_STUMP"   // v10: pick up the stump between bouts
	TypeResolveBout  = "RESOLVE_BOUT" // v10 name of PASS: "nothing more to throw in, resolve the bout"
	TypeAddBot       = "ADD_BOT"      // v10 §2: seat a server-driven opponent (lobby only)
	TypeRemoveBot    = "REMOVE_BOT"   // v10 §2: free a bot's seat (lobby only)
)

// Server -> client messages.
const (
	TypeStateUpdate = "STATE_UPDATE"
	TypeError       = "ERROR"
	TypePong        = "PONG"
	TypeReaction    = "REACTION"
)

// JoinPayload is the payload of JOIN_ROOM.
type JoinPayload struct {
	RoomID     string   `json:"room_id"`
	TgInitData string   `json:"tg_init_data"`
	Create     bool     `json:"create,omitempty"`   // create the room if it does not exist
	DevUser    *DevUser `json:"dev_user,omitempty"` // only honoured when ALLOW_DEV_AUTH=true
}

// DevUser is a fake identity for local development without Telegram.
type DevUser struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url,omitempty"`
}

// PlayCardPayload is the payload of PLAY_CARD.
type PlayCardPayload struct {
	CardID       string `json:"card_id"`
	TargetCardID string `json:"target_card_id,omitempty"`
}

// TransferPayload is the payload of TRANSFER_TURN.
type TransferPayload struct {
	CardID string `json:"card_id"`
}

// ReadyPayload is the payload of READY.
type ReadyPayload struct {
	Ready bool `json:"ready"`
}

// RemoveBotPayload is the payload of REMOVE_BOT.
type RemoveBotPayload struct {
	PlayerID string `json:"player_id"`
}

// ReactPayload is the payload of REACT.
type ReactPayload struct {
	Emoji string `json:"emoji"`
}

// ReactionPayload is the payload of REACTION (server -> every client in the
// room). Bots may add a short line (Text) with or without an emoji.
type ReactionPayload struct {
	PlayerID string `json:"player_id"`
	Emoji    string `json:"emoji"`
	Text     string `json:"text,omitempty"`
	Effect   string `json:"effect,omitempty"` // premium reaction: the effect (and sound) to play
	TS       int64  `json:"ts"`               // unix milliseconds
}

// AllowedEmoji is the whitelist of free reactions; the client renders the same set.
var AllowedEmoji = []string{"👍", "👎", "😂", "😡", "😎", "🤔", "🔥", "💀", "🤡", "❤️", "👏", "🙏"}

// Premium is a reaction with an effect (and a sound) that experience unlocks.
type Premium struct {
	XP     int    `json:"xp"`
	Effect string `json:"effect"`
}

// PremiumEmoji maps the reactions with effects to the XP they need: crying,
// the finger, the victory fanfare, the laugh, the bomb (thresholds 3..15).
var PremiumEmoji = map[string]Premium{
	"😭": {XP: 3, Effect: "cry"},
	"🖕": {XP: 6, Effect: "finger"},
	"🏆": {XP: 9, Effect: "fanfare"},
	"🤣": {XP: 12, Effect: "laugh"},
	"💣": {XP: 15, Effect: "boom"},
}

// VictoryLines are what the fanfare says when a player with the tier wins.
var VictoryLines = []string{
	"Дурак определён. Админ доволен.",
	"Победа! Пенёк даже не понадобился.",
	"GG. Козыри были настоящие.",
	"Кто-то сейчас разглядывает 20 карт в руке…",
	"Ещё партию? Колода уже тасуется.",
}

var allowedEmojiSet = func() map[string]bool {
	m := make(map[string]bool, len(AllowedEmoji)+len(PremiumEmoji))
	for _, e := range AllowedEmoji {
		m[e] = true
	}
	for e := range PremiumEmoji {
		m[e] = true
	}
	return m
}()

// ErrorPayload is the payload of ERROR. CardID, when present, tells the client
// which card to animate back into the hand.
type ErrorPayload struct {
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
	CardID  string `json:"card_id,omitempty"`
}

func encode(msgType string, payload any) []byte {
	var raw json.RawMessage
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			b = []byte("null")
		}
		raw = b
	}
	out, _ := json.Marshal(Envelope{Type: msgType, Payload: raw})
	return out
}
