// Package telegram validates Telegram Mini App init data.
//
// See https://core.telegram.org/bots/webapps#validating-data-received-via-the-mini-app
// The signature is HMAC-SHA256 over the sorted "key=value" pairs joined with
// "\n", keyed with HMAC-SHA256("WebAppData", botToken).
package telegram

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Errors returned by Validate.
var (
	ErrEmpty            = errors.New("telegram: empty init data")
	ErrMissingHash      = errors.New("telegram: init data has no hash")
	ErrInvalidSignature = errors.New("telegram: invalid signature")
	ErrExpired          = errors.New("telegram: init data expired")
	ErrNoBotToken       = errors.New("telegram: bot token is not configured")
)

// User mirrors the WebAppUser object.
type User struct {
	ID              int64  `json:"id"`
	IsBot           bool   `json:"is_bot,omitempty"`
	FirstName       string `json:"first_name"`
	LastName        string `json:"last_name,omitempty"`
	Username        string `json:"username,omitempty"`
	LanguageCode    string `json:"language_code,omitempty"`
	IsPremium       bool   `json:"is_premium,omitempty"`
	PhotoURL        string `json:"photo_url,omitempty"`
	AllowsWriteToPM bool   `json:"allows_write_to_pm,omitempty"`
}

// InitData is the parsed and verified init data.
type InitData struct {
	QueryID      string
	User         *User
	Receiver     *User
	AuthDate     time.Time
	StartParam   string
	ChatType     string
	ChatInstance string
	Hash         string
	Raw          url.Values
}

// DisplayName returns the user's first name (what the spec asks for), falling
// back to the username or the numeric ID.
func (d *InitData) DisplayName() string {
	if d.User == nil {
		return ""
	}
	if n := strings.TrimSpace(d.User.FirstName); n != "" {
		return n
	}
	if d.User.Username != "" {
		return "@" + d.User.Username
	}
	return strconv.FormatInt(d.User.ID, 10)
}

// SecretKey derives the HMAC key for a bot token.
func SecretKey(botToken string) []byte {
	mac := hmac.New(sha256.New, []byte("WebAppData"))
	mac.Write([]byte(botToken))
	return mac.Sum(nil)
}

// DataCheckString builds the string that is signed: every pair except "hash",
// sorted by key, joined with "\n".
func DataCheckString(values url.Values) string {
	keys := make([]string, 0, len(values))
	for k := range values {
		if k == "hash" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+values.Get(k))
	}
	return strings.Join(parts, "\n")
}

// Sign computes the hex HMAC for the given values (used by tests and tools).
func Sign(values url.Values, botToken string) string {
	mac := hmac.New(sha256.New, SecretKey(botToken))
	mac.Write([]byte(DataCheckString(values)))
	return hex.EncodeToString(mac.Sum(nil))
}

// Validate parses initData (the raw query string from Telegram.WebApp.initData),
// verifies the signature against botToken and, if maxAge > 0, rejects data
// older than maxAge.
func Validate(initData, botToken string, maxAge time.Duration) (*InitData, error) {
	return validateAt(initData, botToken, maxAge, time.Now())
}

func validateAt(initData, botToken string, maxAge time.Duration, now time.Time) (*InitData, error) {
	if strings.TrimSpace(initData) == "" {
		return nil, ErrEmpty
	}
	if botToken == "" {
		return nil, ErrNoBotToken
	}
	values, err := url.ParseQuery(initData)
	if err != nil {
		return nil, err
	}
	hash := values.Get("hash")
	if hash == "" {
		return nil, ErrMissingHash
	}
	expected := Sign(values, botToken)
	if subtle.ConstantTimeCompare([]byte(strings.ToLower(hash)), []byte(expected)) != 1 {
		return nil, ErrInvalidSignature
	}

	d := &InitData{
		QueryID:      values.Get("query_id"),
		StartParam:   values.Get("start_param"),
		ChatType:     values.Get("chat_type"),
		ChatInstance: values.Get("chat_instance"),
		Hash:         hash,
		Raw:          values,
	}
	if ad := values.Get("auth_date"); ad != "" {
		secs, err := strconv.ParseInt(ad, 10, 64)
		if err != nil {
			return nil, errors.New("telegram: bad auth_date")
		}
		d.AuthDate = time.Unix(secs, 0)
		if maxAge > 0 && now.Sub(d.AuthDate) > maxAge {
			return nil, ErrExpired
		}
	}
	if raw := values.Get("user"); raw != "" {
		var u User
		if err := json.Unmarshal([]byte(raw), &u); err != nil {
			return nil, errors.New("telegram: bad user json")
		}
		d.User = &u
	}
	if raw := values.Get("receiver"); raw != "" {
		var u User
		if err := json.Unmarshal([]byte(raw), &u); err == nil {
			d.Receiver = &u
		}
	}
	return d, nil
}
