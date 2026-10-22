import { useEffect, useState } from 'react';
import { useGame } from '../state/GameContext';
import { errorText } from '../i18n';
import { haptic } from '../telegram/useTelegram';

/** Hard black strip for errors and short notices; auto-hides. */
export function Toast() {
  const { lastError, clearError, notice, clearNotice } = useGame();
  const [visible, setVisible] = useState(false);
  useEffect(() => {
    if (!lastError && !notice) return;
    setVisible(true);
    if (lastError) haptic('error');
    const t = window.setTimeout(() => {
      setVisible(false);
      if (lastError) clearError();
      if (notice) clearNotice();
    }, lastError ? 2600 : 3600);
    return () => window.clearTimeout(t);
  }, [lastError, clearError, notice, clearNotice]);
  if ((!lastError && !notice) || !visible) return null;
  return (
    <div className="toast" role="alert" onClick={() => setVisible(false)}>
      {lastError ? errorText(lastError.code, lastError.message) : notice}
    </div>
  );
}
