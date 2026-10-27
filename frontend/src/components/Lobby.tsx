import { useEffect, useState, type FormEvent } from 'react';
import { Button } from './Button';
import { useGame } from '../state/GameContext';
import { t } from '../i18n';
import { SkinsScreen } from './SkinsScreen';
import { RulesScreen } from './RulesScreen';
import { Sticker } from './Sticker';
import { cleanNickname, foodAvatar } from '../util/avatar';

const CODE_ALPHABET = 'ABCDEFGHJKMNPQRSTUVWXYZ23456789';

export function randomRoomCode(len = 4): string {
  const bytes = new Uint8Array(len);
  crypto.getRandomValues(bytes);
  let out = '';
  for (const b of bytes) out += CODE_ALPHABET[b % CODE_ALPHABET.length];
  return out;
}

interface Props {
  prefillCode?: string;
}

function botLink(): string | null {
  const bot = import.meta.env.VITE_BOT_USERNAME as string | undefined;
  const app = import.meta.env.VITE_APP_SHORTNAME as string | undefined;
  if (bot && app) return `https://t.me/${bot}/${app}`;
  if (bot) return `https://t.me/${bot}`;
  return null;
}

export function Lobby({ prefillCode }: Props) {
  const { identity, setDevName, join, connection, config, isAdmin, setAdminOpen, theme, setTheme, fx, setFx } = useGame();
  const [name, setName] = useState(identity.name || identity.suggested || '');
  const [code, setCode] = useState(prefillCode ?? '');
  const [panel, setPanel] = useState<'' | 'skins' | 'rules'>('');
  useEffect(() => {
    if (prefillCode) setCode(prefillCode);
  }, [prefillCode]);

  const nick = cleanNickname(name);
  const needsName = !nick;
  const commitName = () => {
    if (nick && nick !== identity.name) setDevName(nick);
  };
  const [noTimer, setNoTimerState] = useState(() => {
    try {
      return localStorage.getItem('durag.noTimer') === '1';
    } catch {
      return false;
    }
  });
  const setNoTimer = (v: boolean) => {
    setNoTimerState(v);
    try {
      localStorage.setItem('durag.noTimer', v ? '1' : '0');
    } catch {
      /* private mode */
    }
  };
  const create = () => {
    commitName();
    join(randomRoomCode(), true, { noTimer });
  };
  const joinByCode = (e: FormEvent) => {
    e.preventDefault();
    if (!code.trim()) return;
    commitName();
    join(code, true, { noTimer });
  };
  const disabled = connection !== 'open' || needsName;
  const telegramOnly = identity.kind === 'dev' && config !== null && !config.dev_auth;
  const link = botLink();

  if (telegramOnly) {
    return (
      <main className="screen lobby">
        <header className="lobby__head">
          <h1 className="zine-title">
            DU
            <br />
            RAG
          </h1>
          <p className="tape">{t('tagline')}</p>
        </header>
        <p className="lobby__hello">
          <span className="sticker-text">{t('lobby.telegramOnly')}</span>
        </p>
        {link ? (
          <a className="btn btn--acid btn--big lobby__link" href={link}>
            {t('lobby.openInTelegram')}
          </a>
        ) : null}
      </main>
    );
  }

  return (
    <main className="screen lobby">
      {/* top bar: theme and effects on the left, the admin chip on the right */}
      <div className="lobby__bar">
        <button type="button" className="chip chip--ghost" onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')} aria-label={t('ui.theme')} title={t('ui.theme')}>
          {theme === 'dark' ? '☀' : '☾'}
        </button>
        <button type="button" className="chip chip--ghost" onClick={() => setFx(fx === 'auto' ? 'full' : fx === 'full' ? 'lite' : 'auto')} aria-label={t('ui.fx')}>
          {t(`ui.fx.${fx}`)}
        </button>
        {isAdmin ? (
          <button type="button" className="chip chip--acid lobby__admin" onClick={() => setAdminOpen(true)}>
            {t('admin.open')}
          </button>
        ) : null}
      </div>
      <header className="lobby__head">
        <h1 className="zine-title">
          DU
          <br />
          RAG
        </h1>
        <p className="tape">{t('tagline')}</p>
      </header>

      {/* skins and rules sit right under the title so a short screen still shows them */}
      <div className="row lobby__extras">
        <Button variant="ink" onClick={() => setPanel('skins')}>
          {t('lobby.skins')}
        </Button>
        <Button variant="paper" onClick={() => setPanel('rules')}>
          {t('lobby.rules')}
        </Button>
      </div>

      <label className="field lobby__name">
        <span className="field__label">{t('lobby.nickname')}</span>
        <span className="lobby__name-row">
          <Sticker name={nick || '?'} avatar={foodAvatar(identity.userId)} size={44} seed={identity.userId} />
          <input
            className="input"
            value={name}
            maxLength={20}
            placeholder={t('lobby.namePlaceholder')}
            onChange={(e) => setName(e.target.value)}
            onBlur={commitName}
          />
        </span>
        <span className="field__hint">{identity.kind === 'dev' ? t('lobby.devMode') : needsName ? t('lobby.nicknameHint') : t('lobby.nicknameOk')}</span>
      </label>

      <Button big onClick={create} disabled={disabled}>
        {t('lobby.create')}
      </Button>
      <label className="lobby__option">
        <input type="checkbox" checked={noTimer} onChange={(e) => setNoTimer(e.target.checked)} />
        <span>{t('lobby.noTimer')}</span>
        <small>{t('lobby.noTimerHint')}</small>
      </label>

      <form className="lobby__join" onSubmit={joinByCode}>
        <label className="field">
          <span className="field__label">{t('lobby.code')}</span>
          <input
            className="input input--code"
            value={code}
            maxLength={12}
            placeholder={t('lobby.codePlaceholder')}
            autoCapitalize="characters"
            onChange={(e) => setCode(e.target.value.toUpperCase())}
          />
        </label>
        <Button variant="paper" type="submit" disabled={disabled || !code.trim()}>
          {t('lobby.join')}
        </Button>
      </form>
      {connection !== 'open' ? <p className="muted">{t('lobby.connecting')}</p> : null}
      {panel === 'skins' ? <SkinsScreen onClose={() => setPanel('')} /> : null}
      {panel === 'rules' ? <RulesScreen onClose={() => setPanel('')} /> : null}
    </main>
  );
}
