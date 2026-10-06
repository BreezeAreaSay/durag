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

// ReactPayload is the payload of REACT.
type ReactPayload struct {
	Emoji string `json:"emoji"`
}

// ReactionPayload is the payload of REACTION (server -> every client in the room).
type ReactionPayload struct {
	PlayerID string `json:"player_id"`
	Emoji    string `json:"emoji"`
	TS       int64  `json:"ts"` // unix milliseconds
}

// AllowedEmoji is the whitelist of reactions; the client renders the same set.
var AllowedEmoji = []string{"👍", "👎", "😂", "😡", "😎", "🤔", "🔥", "💀", "🤡", "❤️", "👏", "🙏"}

var allowedEmojiSet = func() map[string]bool {
	m := make(map[string]bool, len(AllowedEmoji))
	for _, e := range AllowedEmoji {
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
