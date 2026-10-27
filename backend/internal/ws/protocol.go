// Package ws implements the WebSocket transport: clients send intents, the
// server validates them against the authoritative state and pushes
// sanitized STATE_UPDATE messages to every connection in the room.
package ws

import (
	"encoding/json"
	"fmt"
)

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
	TypeSay          = "SAY"          // a quick phrase from the list (unlocked by wins)
	TypeCatchFly     = "CATCH_FLY"    // the player slapped the fly
)

// Server -> client messages.
const (
	TypeStateUpdate = "STATE_UPDATE"
	TypeError       = "ERROR"
	TypePong        = "PONG"
	TypeReaction    = "REACTION"
	TypeFly         = "FLY" // a fly appears over the table, or was caught
)

// JoinPayload is the payload of JOIN_ROOM.
type JoinPayload struct {
	RoomID     string   `json:"room_id"`
	TgInitData string   `json:"tg_init_data"`
	Create     bool     `json:"create,omitempty"`   // create the room if it does not exist
	NoTimer    bool     `json:"no_timer,omitempty"` // with Create: the new table plays without a turn timer
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

// AddBotPayload is the (optional) payload of ADD_BOT.
type AddBotPayload struct {
	Level string `json:"level"` // easy / normal / hard; empty = normal
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

// AllowedEmoji is the whitelist of reactions; the client renders the same set.
// 👊 is a fist on the table (every client shakes its cards), 🚬 lights a
// cigarette by the sender's avatar instead of flying to the table.
var AllowedEmoji = []string{"👍", "👎", "😂", "😡", "😎", "🤔", "🔥", "💀", "🤡", "❤️", "👏", "🙏", "👊", "🚬"}

// SayPayload is the payload of SAY: one of AllowedPhrases.
type SayPayload struct {
	Text string `json:"text"`
}

// AllowedPhrases are the quick lines a human can drop on the table; they
// unlock at PhraseWins lifetime wins (admins have them at once).
var AllowedPhrases = []string{"easy", "бери давай", "ну и ход", "не спи", "это всё?", "ха", "заберёшь", "ой"}

// PhraseWins is how many wins open the quick phrases.
const PhraseWins = 10

var allowedPhraseSet = func() map[string]bool {
	m := make(map[string]bool, len(AllowedPhrases))
	for _, p := range AllowedPhrases {
		m[p] = true
	}
	return m
}()

// CatchFlyPayload is the payload of CATCH_FLY.
type CatchFlyPayload struct {
	ID string `json:"id"`
}

// FlyPayload is the payload of FLY: a fly takes off over the table (Seed
// draws its path), or somebody caught it (CaughtBy, with their new XP).
type FlyPayload struct {
	ID       string `json:"id"`
	Seed     int64  `json:"seed"`
	TS       int64  `json:"ts"`
	CaughtBy string `json:"caught_by,omitempty"`
	XP       int    `json:"xp,omitempty"`
}

// Premium is a reaction with an effect (and a sound) that experience (XP)
// or lifetime wins unlock: whichever threshold is set.
type Premium struct {
	XP     int    `json:"xp,omitempty"`
	Wins   int    `json:"wins,omitempty"`
	Effect string `json:"effect"`
}

// Unlocked tells whether a player with xp and wins (or an admin) has it.
func (p Premium) Unlocked(xp, wins int, admin bool) bool {
	if admin {
		return true
	}
	return (p.XP > 0 && xp >= p.XP) || (p.Wins > 0 && wins >= p.Wins)
}

// Locked describes what the reaction still needs.
func (p Premium) Locked() string {
	if p.Wins > 0 {
		return fmt.Sprintf("this reaction unlocks at %d wins", p.Wins)
	}
	return fmt.Sprintf("this reaction unlocks at %d XP", p.XP)
}

// PremiumEmoji maps the reactions with effects to what they need: the XP
// page (crying, the finger, the victory fanfare, the laugh, the bomb at
// 3..15 XP) and the wins page (the handshake, the firecracker, the rooster,
// the crown, the glasses at 5..30 wins).
var PremiumEmoji = map[string]Premium{
	"😭": {XP: 3, Effect: "cry"},
	"🖕": {XP: 6, Effect: "finger"},
	"🏆": {XP: 9, Effect: "fanfare"},
	"🤣": {XP: 12, Effect: "laugh"},
	"💣": {XP: 15, Effect: "boom"},
	"🤝": {Wins: 5, Effect: "shake"},
	"🧨": {Wins: 10, Effect: "firecracker"},
	"🐓": {Wins: 15, Effect: "rooster"},
	"👑": {Wins: 20, Effect: "crown"},
	"🥂": {Wins: 30, Effect: "clink"},
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
