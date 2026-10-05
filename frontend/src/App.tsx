import { useEffect, useMemo } from 'react';
import { GameProvider, useGame } from './state/GameContext';
import { Lobby } from './components/Lobby';
import { WaitingRoom } from './components/WaitingRoom';
import { GameScreen } from './components/GameScreen';
import { Toast } from './components/Toast';
import { t } from './i18n';

function startRoomFromUrl(): string | undefined {
  const tgParam = window.Telegram?.WebApp?.initDataUnsafe?.start_param;
  if (tgParam) return tgParam.toUpperCase();
  const params = new URLSearchParams(location.search);
  const fromUrl = params.get('room') ?? params.get('startapp') ?? params.get('tgWebAppStartParam');
  return fromUrl ? fromUrl.toUpperCase() : undefined;
}

function Screens() {
  const { state, roomId, identity, join, connection, replaced } = useGame();
  const startRoom = useMemo(startRoomFromUrl, []);

  // Deep links (t.me/bot/app?startapp=CODE or ?room=CODE) join automatically
  // for Telegram users; dev users first have to pick a name in the lobby.
  useEffect(() => {
    if (!startRoom || roomId || connection !== 'open') return;
    if (identity.kind === 'telegram' || identity.name) join(startRoom, true);
  }, [startRoom, roomId, connection, identity, join]);

  if (replaced) {
    return (
      <main className="screen lobby">
        <h1 className="zine-title">DURAG</h1>
        <p className="tape">{t('conn.replaced')}</p>
      </main>
    );
  }
  if (!state || !roomId) return <Lobby prefillCode={startRoom} />;
  if (state.status === 'waiting') return <WaitingRoom state={state} />;
  return <GameScreen state={state} />;
}

export default function App() {
  return (
    <GameProvider>
      <Screens />
      <Toast />
    </GameProvider>
  );
}
