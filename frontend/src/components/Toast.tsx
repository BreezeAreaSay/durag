import { useEffect, useState } from 'react';
import { useGame } from '../state/GameContext';
import { errorText } from '../i18n';
import { haptic } from '../telegram/useTelegram';

/** Hard black error strip; auto-hides. */
export function Toast() {
  const { lastError, clearError } = useGame();
  const [visible, setVisible] = useState(false);
  useEffect(() => {
    if (!lastError) return;
    setVisible(true);
    haptic('error');
    const t = window.setTimeout(() => {
      setVisible(false);
      clearError();
    }, 2600);
    return () => window.clearTimeout(t);
  }, [lastError, clearError]);
  if (!lastError || !visible) return null;
  return (
    <div className="toast" role="alert" onClick={() => setVisible(false)}>
      {errorText(lastError.code, lastError.message)}
    </div>
  );
}
