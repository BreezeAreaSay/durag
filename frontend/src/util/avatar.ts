// The dish on a person's sticker, the same list and hash as the server
// (backend/internal/ws/avatar.go): the client can draw it before joining.
export const FOOD_AVATARS = ['🍕', '🍔', '🌭', '🥟', '🍩', '🍞', '🥨', '🧀', '🍗', '🍖', '🥓', '🍳', '🥞', '🧇', '🍜', '🍝', '🍲', '🥘', '🍱', '🍣', '🍤', '🍙', '🍚', '🥐', '🥖', '🧆', '🌮', '🌯', '🥙', '🥗', '🍅', '🥒', '🥕', '🌽', '🥔', '🍠', '🧄', '🧅', '🍄', '🥜', '🍎', '🍐', '🍊', '🍋', '🍌', '🍉', '🍇', '🍓', '🍒', '🍑', '🥭', '🍍', '🥝', '🍆', '🥑', '🍫', '🍬', '🍭', '🍰', '🧁', '🥧', '🍦', '🍪', '🥛', '☕', '🍵', '🧃', '🥤', '🍺', '🍷', '🥃', '🍾', '🫕', '🥫', '🧈', '🍯', '🥯', '🫔', '🫓', '🥪'];

export function foodAvatar(id: string): string {
  let h = 2166136261;
  const bytes = new TextEncoder().encode(id);
  for (const b of bytes) {
    h ^= b;
    h = Math.imul(h, 16777619) >>> 0;
  }
  return FOOD_AVATARS[h % FOOD_AVATARS.length]!;
}

/** 2..20 characters of letters, digits, spaces, _ - . (mirrors the server's nickname()). */
export function cleanNickname(raw: string): string {
  const kept = [...raw.trim()].filter((ch) => /[0-9A-Za-zА-яЁё _.-]/.test(ch)).join('');
  const out = kept.split(/\s+/).filter(Boolean).join(' ');
  if ([...out].length < 2) return '';
  return [...out].slice(0, 20).join('');
}
