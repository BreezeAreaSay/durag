import { useState } from 'react';
import { ALLOWED_EMOJI, PHRASES, PHRASE_WINS, PREMIUM_EMOJI, WINS_EMOJI, nextUnlock, nextWinsUnlock } from '../util/emoji';
import { t } from '../i18n';

interface Props {
  /** the player's lifetime experience: unlocks the reactions with effects */
  xp: number;
  /** the player's lifetime wins: the second page and the quick phrases */
  wins?: number;
  /** admins have every reaction whatever their XP */
  unlockAll?: boolean;
  onPick(emoji: string): void;
  /** a quick phrase */
  onSay?(text: string): void;
  onClose(): void;
}

/** Brutalist reaction picker anchored above the player's own sticker: two pages and a row of phrases. */
export function EmojiPanel({ xp, wins = 0, unlockAll = false, onPick, onSay, onClose }: Props) {
  const [page, setPage] = useState<0 | 1>(0);
  const next = unlockAll ? null : nextUnlock(xp);
  const nextWins = unlockAll ? null : nextWinsUnlock(wins);
  const phrasesOpen = unlockAll || wins >= PHRASE_WINS;
  return (
    <div className="emoji-backdrop" onClick={onClose} role="dialog" aria-label="reactions">
      <div className="emoji-panel" onClick={(e) => e.stopPropagation()}>
        {page === 0 ? (
          <>
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
          </>
        ) : (
          <>
            <div className="emoji-panel__row emoji-panel__row--wins">
              {WINS_EMOJI.map((p) => {
                const locked = !unlockAll && wins < p.wins;
                return (
                  <button
                    key={p.emoji}
                    type="button"
                    className={`emoji-panel__btn emoji-panel__btn--fx ${locked ? 'emoji-panel__btn--locked' : ''}`}
                    title={locked ? t('emoji.lockedWins', { n: p.wins }) : undefined}
                    aria-disabled={locked}
                    data-wins={p.wins}
                    onClick={() => {
                      if (!locked) onPick(p.emoji);
                    }}
                  >
                    {p.emoji}
                    {locked ? <span className="emoji-panel__xp">{p.wins}★</span> : null}
                  </button>
                );
              })}
            </div>
            <p className="emoji-panel__hint">{unlockAll ? t('emoji.adminWins', { wins }) : nextWins === null ? t('emoji.winsAllOpen', { wins }) : t('emoji.winsProgress', { wins, n: nextWins })}</p>
          </>
        )}
        <div className={`emoji-panel__row emoji-panel__row--say ${phrasesOpen ? '' : 'emoji-panel__row--locked'}`} title={phrasesOpen ? undefined : t('emoji.phrasesLocked', { n: PHRASE_WINS })}>
          {PHRASES.map((text) => (
            <button
              key={text}
              type="button"
              className={`emoji-panel__say ${phrasesOpen ? '' : 'emoji-panel__say--locked'}`}
              aria-disabled={!phrasesOpen}
              onClick={() => {
                if (phrasesOpen) onSay?.(text);
              }}
            >
              {text}
            </button>
          ))}
          {phrasesOpen ? null : <span className="emoji-panel__lock">🔒 {t('emoji.phrasesLocked', { n: PHRASE_WINS })}</span>}
        </div>
        <div className="emoji-panel__pager" role="tablist">
          {[0, 1].map((i) => (
            <button key={i} type="button" role="tab" aria-selected={page === i} className={`emoji-panel__dot ${page === i ? 'emoji-panel__dot--on' : ''}`} onClick={() => setPage(i as 0 | 1)} aria-label={t('emoji.page', { n: i + 1 })}>
              {i + 1}
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}
