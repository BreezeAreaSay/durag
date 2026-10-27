import { useEffect, useRef, useState, type PointerEvent } from 'react';
import { sound } from '../util/audio';
import { haptic } from '../telegram/useTelegram';
import { t } from '../i18n';

const BOTTLE = 100; // what a full bottle holds, in glass units it is five glasses
const GLASS = 20;

/**
 * The winner's bottle. Drag the bottle over the glass and tilt it (the
 * further right the finger, the steeper); past sixty degrees it pours and
 * the glass fills, the bottle drains. Tap the glass to drink it. Five
 * glasses to a bottle; a double tap on the empty bottle opens a new one.
 * Everything is local: a toy, nothing goes to the server.
 */
export function VodkaToy() {
  const [bottle, setBottle] = useState(BOTTLE);
  const [glass, setGlass] = useState(0);
  const [tilt, setTilt] = useState(0); // degrees, 0 = upright
  const [pos, setPos] = useState({ x: 0, y: 0 }); // the bottle's drag offset
  const [drinks, setDrinks] = useState(0);
  const [splash, setSplash] = useState(0);
  const area = useRef<HTMLDivElement>(null);
  const grab = useRef<{ x: number; y: number; ox: number; oy: number } | null>(null);
  const pouring = useRef(false);
  const lastTap = useRef(0);

  // the pour: while the bottle is tilted over the glass, a little goes every frame
  useEffect(() => {
    let raf = 0;
    const step = () => {
      const el = area.current;
      const b = el?.querySelector<HTMLElement>('.vodka__bottle');
      const g = el?.querySelector<HTMLElement>('.vodka__glass');
      if (el && b && g && tilt > 60 && bottle > 0) {
        const br = b.getBoundingClientRect();
        const gr = g.getBoundingClientRect();
        // the mouth of the bottle, roughly: the top-left corner when tilted to the right
        const mouthX = br.left + br.width * 0.15;
        const mouthY = br.top + br.height * 0.1;
        const over = mouthX > gr.left - 10 && mouthX < gr.right + 10 && mouthY < gr.top + gr.height * 0.6;
        if (over) {
          if (!pouring.current) {
            pouring.current = true;
            sound.pour();
          }
          const rate = 0.18 + (tilt - 60) / 120;
          setBottle((v) => Math.max(0, v - rate));
          setGlass((v) => Math.min(GLASS, v + rate));
          if (glass >= GLASS - 0.5 && Math.random() < 0.3) setSplash((n) => n + 1); // it runs over
        } else {
          pouring.current = false;
        }
      } else {
        pouring.current = false;
      }
      raf = requestAnimationFrame(step);
    };
    raf = requestAnimationFrame(step);
    return () => cancelAnimationFrame(raf);
  }, [tilt, bottle, glass]);

  const down = (e: PointerEvent) => {
    e.currentTarget.setPointerCapture(e.pointerId);
    grab.current = { x: e.clientX, y: e.clientY, ox: pos.x, oy: pos.y };
    const now = performance.now();
    if (bottle <= 0 && now - lastTap.current < 400) {
      setBottle(BOTTLE); // a new bottle from under the table
      sound.snap();
      haptic('medium');
    }
    lastTap.current = now;
  };
  const move = (e: PointerEvent) => {
    const g = grab.current;
    if (!g) return;
    const dx = e.clientX - g.x;
    const dy = e.clientY - g.y;
    setPos({ x: g.ox + dx, y: g.oy + dy });
    // the tilt follows the finger: the further right of where it was grabbed, the steeper
    setTilt(Math.max(0, Math.min(110, (g.ox + dx) * 0.9)));
  };
  const up = () => {
    grab.current = null;
    setTilt(0);
    setPos({ x: 0, y: 0 });
    pouring.current = false;
  };
  const drink = () => {
    if (glass < 2) return;
    setGlass(0);
    setDrinks((n) => n + 1);
    sound.gulp();
    haptic('heavy');
  };

  const empty = bottle <= 0;
  return (
    <div className="vodka" ref={area} aria-label="vodka">
      <div className="vodka__table" />
      <button type="button" className={`vodka__glass ${glass >= GLASS - 0.5 ? 'vodka__glass--full' : ''}`} onClick={drink} aria-label={t('vodka.drink')}>
        <span className="vodka__level" style={{ height: `${(glass / GLASS) * 100}%` }} />
        {splash % 2 === 1 ? <span className="vodka__drop" key={splash} /> : null}
      </button>
      <div
        className={`vodka__bottle ${empty ? 'vodka__bottle--empty' : ''} ${tilt > 60 ? 'vodka__bottle--pouring' : ''}`}
        style={{ transform: `translate(${pos.x}px, ${pos.y}px) rotate(${tilt}deg)` }}
        onPointerDown={down}
        onPointerMove={move}
        onPointerUp={up}
        onPointerCancel={up}
        role="img"
        aria-label={t('vodka.bottle')}
      >
        <span className="vodka__neck" />
        <span className="vodka__body">
          <span className="vodka__fill" style={{ height: `${bottle}%` }} />
          <span className="vodka__label">
            <b>DURAG</b>
            <small>40%</small>
          </span>
        </span>
        {tilt > 60 && bottle > 0 ? <span className="vodka__stream" /> : null}
      </div>
      <p className="vodka__hint">{empty ? t('vodka.empty') : drinks > 0 ? t('vodka.drinks', { n: drinks }) : t('vodka.hint')}</p>
    </div>
  );
}
