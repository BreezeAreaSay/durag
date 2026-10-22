import { ALLOWED_EMOJI, PREMIUM_EMOJI, nextUnlock } from '../util/emoji';
import { t } from '../i18n';

interface Props {
  /** the player's lifetime experience: unlocks the reactions with effects */
  xp: number;
  /** admins have every reaction whatever their XP */
  unlockAll?: boolean;
  onPick(emoji: string): void;
  onClose(): void;
}

/** Brutalist reaction picker anchored above the player's own sticker. */
export function EmojiPanel({ xp, unlockAll = false, onPick, onClose }: Props) {
  const next = unlockAll ? null : nextUnlock(xp);
  return (
    <div className="emoji-backdrop" onClick={onClose} role="dialog" aria-label="reactions">
      <div className="emoji-panel" onClick={(e) => e.stopPropagation()}>
        <div className="emoji-panel__row">
          {ALLOWED_EMOJI.map((emoji) => (
            <button key={emoji} type="button" className="emoji-panel__btn" onClick={() => onPick(emoji)}>
              {emoji}
            </button>
          ))}
        </div>
        <div className="emoji-panel__row emoji-panel__row--fx">
          {PREMIUM_EMOJI.map((p) => {
            const locked = !unlockAll && xp < p.xp;
            return (
              <button
                key={p.emoji}
                type="button"
                className={`emoji-panel__btn emoji-panel__btn--fx ${locked ? 'emoji-panel__btn--locked' : ''}`}
                title={locked ? t('emoji.locked', { n: p.xp }) : undefined}
                aria-disabled={locked}
                data-xp={p.xp}
                onClick={() => {
                  if (!locked) onPick(p.emoji);
                }}
              >
                {p.emoji}
                {locked ? <span className="emoji-panel__xp">{p.xp}</span> : null}
              </button>
            );
          })}
        </div>
        <p className="emoji-panel__hint">{unlockAll ? t('emoji.admin', { xp }) : next === null ? t('emoji.allOpen', { xp }) : t('emoji.progress', { xp, n: next })}</p>
      </div>
    </div>
  );
}
