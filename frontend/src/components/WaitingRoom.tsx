import { useState } from 'react';
import { Button } from './Button';
import { Sticker } from './Sticker';
import { useGame } from '../state/GameContext';
import { t } from '../i18n';
import type { GameState } from '../types/protocol';

interface Props {
  state: GameState;
}

function shareLink(room: string): string | null {
  const bot = import.meta.env.VITE_BOT_USERNAME as string | undefined;
  const app = import.meta.env.VITE_APP_SHORTNAME as string | undefined;
  if (bot && app) return `https://t.me/${bot}/${app}?startapp=${room}`;
  if (bot) return `https://t.me/${bot}?startapp=${room}`;
  return null;
}

export function WaitingRoom({ state }: Props) {
  const { selfId, setReady, leave } = useGame();
  const [copied, setCopied] = useState(false);
  const me = state.players.find((p) => p.id === selfId);
  const link = shareLink(state.room_id);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(link ?? state.room_id);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    } catch {
      // clipboard may be unavailable
    }
  };
  return (
    <main className="screen waiting">
      <header className="waiting__head">
        <span className="tape tape--small">{t('waiting.title')}</span>
        <h1 className="zine-title zine-title--code">{state.room_id}</h1>
        <p className="muted">{t('waiting.share')}</p>
        <div className="row">
          <Button variant="paper" onClick={copy}>
            {copied ? t('waiting.copied') : t('waiting.copy')}
          </Button>
          {link && window.Telegram?.WebApp?.openTelegramLink ? (
            <Button
              variant="paper"
              onClick={() => window.Telegram?.WebApp?.openTelegramLink?.(`https://t.me/share/url?url=${encodeURIComponent(link)}&text=${encodeURIComponent('DURAG ' + state.room_id)}`)}
            >
              Telegram
            </Button>
          ) : null}
        </div>
      </header>

      <section className="players">
        <h2 className="players__title">
          {t('waiting.players')} {state.players.length}/{state.max_players}
        </h2>
        <ul className="players__list">
          {state.players.map((p) => (
            <li key={p.id} className={`players__item ${p.is_ready ? 'players__item--ready' : ''}`}>
              <Sticker name={p.name} avatarUrl={p.avatar_url} seed={p.id} />
              <span className="players__name">
                {p.name}
                {p.id === state.host_id ? <small> · {t('waiting.host')}</small> : null}
              </span>
              <span className="players__state">{p.is_ready ? t('waiting.ready') : t('waiting.notReady')}</span>
            </li>
          ))}
        </ul>
        {state.players.length < 2 ? <p className="muted">{t('waiting.needPlayers')}</p> : null}
        {state.players.length >= 2 && me?.is_ready ? <p className="muted">{t('waiting.waitingOthers')}</p> : null}
      </section>

      <footer className="waiting__actions">
        <Button big variant={me?.is_ready ? 'ink' : 'acid'} onClick={() => setReady(!me?.is_ready)}>
          {me?.is_ready ? t('waiting.notReady') : t('waiting.ready')}
        </Button>
        <Button variant="paper" onClick={leave}>
          {t('waiting.leave')}
        </Button>
      </footer>
    </main>
  );
}
