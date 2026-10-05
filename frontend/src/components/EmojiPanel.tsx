import { ALLOWED_EMOJI } from '../util/emoji';

interface Props {
  onPick(emoji: string): void;
  onClose(): void;
}

/** Brutalist reaction picker anchored above the player's own sticker. */
export function EmojiPanel({ onPick, onClose }: Props) {
  return (
    <div className="emoji-backdrop" onClick={onClose} role="dialog" aria-label="reactions">
      <div className="emoji-panel" onClick={(e) => e.stopPropagation()}>
        {ALLOWED_EMOJI.map((emoji) => (
          <button key={emoji} type="button" className="emoji-panel__btn" onClick={() => onPick(emoji)}>
            {emoji}
          </button>
        ))}
      </div>
    </div>
  );
}
