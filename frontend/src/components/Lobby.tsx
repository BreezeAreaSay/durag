import { useEffect, useState, type FormEvent } from 'react';
import { Button } from './Button';
import { useGame } from '../state/GameContext';
import { t } from '../i18n';

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

export function Lobby({ prefillCode }: Props) {
  const { identity, setDevName, join, connection } = useGame();
  const [name, setName] = useState(identity.name);
  const [code, setCode] = useState(prefillCode ?? '');
  useEffect(() => {
    if (prefillCode) setCode(prefillCode);
  }, [prefillCode]);

  const needsName = identity.kind === 'dev' && !identity.name;
  const commitName = () => {
    if (identity.kind === 'dev' && name.trim() && name.trim() !== identity.name) setDevName(name);
  };
  const create = () => {
    commitName();
    join(randomRoomCode(), true);
  };
  const joinByCode = (e: FormEvent) => {
    e.preventDefault();
    if (!code.trim()) return;
    commitName();
    join(code, true);
  };
  const disabled = connection !== 'open' || (needsName && !name.trim());

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

      {identity.kind === 'dev' ? (
        <label className="field">
          <span className="field__label">{t('lobby.yourName')}</span>
          <input
            className="input"
            value={name}
            maxLength={32}
            placeholder={t('lobby.namePlaceholder')}
            onChange={(e) => setName(e.target.value)}
            onBlur={commitName}
          />
          <span className="field__hint">{t('lobby.devMode')}</span>
        </label>
      ) : (
        <p className="lobby__hello">
          <span className="sticker-text">{identity.name}</span>
        </p>
      )}

      <Button big onClick={create} disabled={disabled}>
        {t('lobby.create')}
      </Button>

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
    </main>
  );
}
