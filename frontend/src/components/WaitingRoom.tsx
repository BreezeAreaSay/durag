import { useState } from 'react';
import { Button } from './Button';
import { Sticker } from './Sticker';
import { TalkFeed } from './TalkFeed';
import { useGame } from '../state/GameContext';
import { useBackButton } from '../telegram/useTelegram';
import { frameWins } from '../util/progress';
import { t } from '../i18n';
import type { BotLevel, GameState } from '../types/protocol';

const LEVELS: BotLevel[] = ['easy', 'normal', 'hard'];

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
  const { selfId, setReady, leave, addBot, removeBot, config, reactions } = useGame();
  const [copied, setCopied] = useState(false);
  const [level, setLevel] = useState<BotLevel>('normal');
  const me = state.players.find((p) => p.id === selfId);
  const link = shareLink(state.room_id);
  useBackButton(leave);
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
        <div className="waiting__top">
          <span className="tape tape--small">{t('waiting.title')}</span>
          <button type="button" className="chip chip--link" onClick={leave}>
            ← {t('waiting.menu')}
          </button>
        </div>
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
              <Sticker name={p.name} avatarUrl={p.avatar_url} avatar={p.avatar} seed={p.id} wins={frameWins(p)} playerId={p.id} scar={p.scar} />
              <span className="players__name">
                {p.name}
                {p.is_bot ? <span className="players__bot">{t('waiting.bot')}</span> : null}
                {p.id === state.host_id ? <small> · {t('waiting.host')}</small> : null}
                {p.is_bot ? <small> · {t(`waiting.level.${p.bot_level ?? 'normal'}`).toLowerCase()}</small> : <small> · XP {p.xp ?? 0}</small>}
              </span>
              {p.is_bot ? (
                <button type="button" className="players__remove" onClick={() => removeBot(p.id)} aria-label={t('waiting.removeBot')}>
                  ✕
                </button>
              ) : (
                <span className="players__state">{p.is_ready ? t('waiting.ready') : t('waiting.notReady')}</span>
              )}
            </li>
          ))}
        </ul>
        <TalkFeed reactions={reactions} players={state.players} />
        {config?.bots !== false && state.players.length < state.max_players ? (
          <div className="players__add">
            <Button variant="paper" onClick={() => addBot(level)}>
              {t('waiting.addBot')}
            </Button>
            <button
              type="button"
              className={`chip chip--level chip--level-${level}`}
              aria-label={t('waiting.level')}
              title={t('waiting.level')}
              onClick={() => setLevel(LEVELS[(LEVELS.indexOf(level) + 1) % LEVELS.length] ?? 'normal')}
            >
              {t(`waiting.level.${level}`)}
            </button>
          </div>
        ) : null}
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
