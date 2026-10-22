import { Button } from './Button';
import { Sticker } from './Sticker';
import { useGame } from '../state/GameContext';
import { t, type Key } from '../i18n';
import { SKINS, TIER_MAX, frameWins, isUnlocked, skinProgress, tierOf, toNextTier, type SkinId } from '../util/progress';

interface Props {
  onClose(): void;
}

/** A card back drawn in CSS: the same two designs the table renders in PixiJS. */
function BackPreview({ id }: { id: SkinId }) {
  return <span className={`back back--${id}`} aria-hidden="true" />;
}

export function SkinsScreen({ onClose }: Props) {
  const { wins: realWins, skin, setSkin, identity, isAdmin } = useGame();
  const wins = isAdmin ? frameWins({ admin: true }) : realWins; // admins own everything
  const next = toNextTier(wins);
  return (
    <div className="overlay overlay--fixed" role="dialog" aria-label={t('skins.title')}>
      <div className="overlay__card overlay__card--tall">
        <h1 className="zine-title zine-title--result">{t('skins.title')}</h1>
        <p className="sticker-text">{isAdmin ? t('skins.admin', { n: realWins }) : t('skins.wins', { n: wins })}</p>

        <h2 className="skins__h">{t('skins.backs')}</h2>
        <ul className="skins">
          {SKINS.map((s) => {
            const open = isUnlocked(s, wins);
            const on = skin === s.id;
            return (
              <li key={s.id} className={`skin ${open ? '' : 'skin--locked'} ${on ? 'skin--on' : ''}`}>
                <BackPreview id={s.id} />
                <div className="skin__info">
                  <b>{t(`skins.${s.id}` as Key)}</b>
                  {open ? (
                    <Button variant={on ? 'ink' : 'acid'} onClick={() => setSkin(s.id)} disabled={on}>
                      {on ? t('skins.selected') : t('skins.select')}
                    </Button>
                  ) : (
                    <>
                      <span className="muted">{t('skins.lockedAt', { n: s.unlockAt })}</span>
                      <span className="meter" style={{ ['--p' as string]: skinProgress(wins) }}>
                        <i />
                      </span>
                      <small>{t('skins.toGo', { n: s.unlockAt - wins })}</small>
                    </>
                  )}
                </div>
              </li>
            );
          })}
        </ul>

        <h2 className="skins__h">{t('skins.frames')}</h2>
        <div className="frames">
          <Sticker name={identity.name || '?'} avatarUrl={identity.avatarUrl} size={56} wins={wins} />
          <div className="skin__info">
            <small className="muted">{t('skins.frameHint')}</small>
            <b>{tierOf(wins) >= TIER_MAX ? t('skins.maxFrame') : t('skins.nextFrame', { n: next })}</b>
          </div>
        </div>

        <div className="row row--center">
          <Button variant="paper" onClick={onClose}>
            {t('skins.close')}
          </Button>
        </div>
      </div>
    </div>
  );
}
