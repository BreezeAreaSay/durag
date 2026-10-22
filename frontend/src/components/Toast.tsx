import { useEffect, useRef, useState } from 'react';
import { useGame } from '../state/GameContext';
import { errorText } from '../i18n';
import { haptic } from '../telegram/useTelegram';

/** Hard black strip for errors and short notices; auto-hides. */
export function Toast() {
  const { lastError, clearError, notice, clearNotice } = useGame();
  const [visible, setVisible] = useState(false);
  // The hide timer must follow the message, not the context: a timer that
  // restarts on every state update never fires while bots move and talk.
  const latest = useRef({ lastError, clearError, notice, clearNotice });
  latest.current = { lastError, clearError, notice, clearNotice };
  const errorNonce = lastError?.nonce ?? 0;
  useEffect(() => {
    if (!errorNonce && !notice) return;
    setVisible(true);
    if (errorNonce) haptic('error');
    const t = window.setTimeout(() => {
      setVisible(false);
      const cur = latest.current;
      if (cur.lastError) cur.clearError();
      if (cur.notice) cur.clearNotice();
    }, errorNonce ? 2600 : 3600);
    return () => window.clearTimeout(t);
  }, [errorNonce, notice]);
  if ((!lastError && !notice) || !visible) return null;
  return (
    <div className="toast" role="alert" onClick={() => setVisible(false)}>
      {lastError ? errorText(lastError.code, lastError.message) : notice}
    </div>
  );
}
