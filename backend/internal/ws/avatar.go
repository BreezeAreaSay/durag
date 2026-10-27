package ws

import "strings"

// Nobody's Telegram photo reaches the table: every person gets a dish for
// a face, picked from the id so it stays the same from game to game.
var FoodAvatars = []string{"🍕", "🍔", "🌭", "🥟", "🍩", "🍞", "🥨", "🧀", "🍗", "🍖", "🥓", "🍳", "🥞", "🧇", "🍜", "🍝", "🍲", "🥘", "🍱", "🍣", "🍤", "🍙", "🍚", "🥐", "🥖", "🧆", "🌮", "🌯", "🥙", "🥗", "🍅", "🥒", "🥕", "🌽", "🥔", "🍠", "🧄", "🧅", "🍄", "🥜", "🍎", "🍐", "🍊", "🍋", "🍌", "🍉", "🍇", "🍓", "🍒", "🍑", "🥭", "🍍", "🥝", "🍆", "🥑", "🍫", "🍬", "🍭", "🍰", "🧁", "🥧", "🍦", "🍪", "🥛", "☕", "🍵", "🧃", "🥤", "🍺", "🍷", "🥃", "🍾", "🫕", "🥫", "🧈", "🍯", "🥯", "🫔", "🫓", "🥪"}

// foodAvatar maps a player id to its dish (an FNV-1a hash into the list).
func foodAvatar(id string) string {
	var h uint32 = 2166136261
	for i := 0; i < len(id); i++ {
		h ^= uint32(id[i])
		h *= 16777619
	}
	return FoodAvatars[h%uint32(len(FoodAvatars))]
}

// nickname cleans the name a person typed for the table: 2..20 characters
// of letters, digits, spaces and a few marks; "" when it does not pass.
func nickname(raw string) string {
	raw = strings.TrimSpace(raw)
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r == ' ' || r == '_' || r == '-' || r == '.':
			b.WriteRune(r)
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= 'А' && r <= 'я', r == 'Ё', r == 'ё':
			b.WriteRune(r)
		}
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	if n := len([]rune(out)); n < 2 {
		return ""
	} else if n > 20 {
		out = string([]rune(out)[:20])
	}
	return out
}
