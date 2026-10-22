import { useMemo } from 'react';
import { frameClass, frameStyle } from '../util/progress';

interface Props {
  name: string;
  avatarUrl?: string;
  size?: number;
  seed?: string;
  muted?: boolean;
  badge?: string;
  /** lifetime wins: colours the frame (every 5), effects from 50 */
  wins?: number;
  /** lets the reaction layer find this avatar on screen */
  playerId?: string;
  /** lost the last game: the fool's mark, until a win */
  scar?: boolean;
}

/** A Telegram avatar cut out like a sloppily glued black & white sticker. */
export function Sticker({ name, avatarUrl, size = 44, seed, muted, badge, wins = 0, playerId, scar }: Props) {
  const tilt = useMemo(() => {
    const s = seed ?? name;
    let h = 0;
    for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) | 0;
    return ((h % 17) - 8) * 0.9;
  }, [seed, name]);
  const initials = name
    .split(/\s+/)
    .map((w) => w[0] ?? '')
    .join('')
    .slice(0, 2)
    .toUpperCase();
  return (
    <span
      className={`sticker ${muted ? 'sticker--muted' : ''} ${frameClass(wins)}`}
      style={{ width: size, height: size, ['--tilt' as string]: `${tilt}deg`, ...frameStyle(wins) }}
      title={name}
      data-player={playerId}
    >
      {avatarUrl ? <img src={avatarUrl} alt="" referrerPolicy="no-referrer" /> : <span className="sticker__initials">{initials || '?'}</span>}
      {badge ? <span className="sticker__badge">{badge}</span> : null}
      {scar ? (
        <span className="sticker__scar" title="Дурак прошлой игры" aria-label="клеймо дурака">
          Д
        </span>
      ) : null}
    </span>
  );
}
