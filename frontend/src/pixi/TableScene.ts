// The game table: an authoritative-state renderer. Every STATE_UPDATE is
// turned into a SceneModel and `sync()` moves cards to where the state says
// they are. Drag-and-drop sends intents optimistically; an ERROR from the
// server snaps the card back into the hand with a stop-motion rubber band.
import { Application, Container, Graphics, Text, type FederatedPointerEvent } from 'pixi.js';
import { CardView } from './CardView';
import { StopMotionTweener, easings } from './tween';
import {
  computeMetrics,
  deckSlot,
  discardSlot,
  exitSlot,
  handSlots,
  hitSlot,
  nextAttackSlot,
  stumpSlot,
  tableSlots,
  trumpSlot,
  type Metrics,
  type Slot,
} from './layout';
import { drawSuit } from './suits';
import { canBeat } from '../util/rules';
import { isSuper } from '../util/cards';
import type { Card, Suit } from '../types/protocol';
import type { SceneModel } from './model';

export interface SceneHandlers {
  onPlay(cardId: string, targetId?: string): void;
  onTransfer(cardId: string): void;
  /** the viewer tapped their stump during the stump step */
  onTakeStump?(): void;
  /** the WebGL context was lost (iOS memory pressure): the host remounts the scene */
  onContextLost?(): void;
}

export interface SceneLabels {
  trump: string;
  stump?: string;
}

export interface SceneOptions {
  fps?: number;
  topInset?: number;
  labels?: SceneLabels;
}

interface Placement {
  card: Card;
  layer: Container;
  slot: Slot;
  faceUp: boolean;
  z: number;
  kind: 'hand' | 'attack' | 'defense';
}

interface DragState {
  id: string;
  view: CardView;
  dx: number;
  dy: number;
  startX: number;
  startY: number;
  moved: boolean;
}

interface Pending {
  slot: Slot;
  until: number;
}

const FONT = 'Unbounded, "Arial Black", Impact, sans-serif';
// A card in flight returns to the hand ONLY on ERROR (or when the state shows
// it elsewhere); this is just a last-resort guard against a lost connection.
const PENDING_TTL = 15000;

// No shader passes at all: WebKit on iPhones rendered filtered cards black.
// Everything on the table is plain vector geometry, text and gradients.
const GLOW_STEP_MS = 1000 / 12; // stop-motion cadence of the glow pulse
const REVEAL_HOLD_MS = 1600; // the drawn trump card rests in the centre this long
const REVEAL_GUARD_MS = 4000; // hard stop for the reveal animation (the hand card must never stay hidden)
const INK = 0x0d0d0d;
const PAPER = 0xece9e1;
const ACID = 0xe6ff00;
const RED = 0xcf1fff;

export class TableScene {
  readonly app = new Application();

  private readonly handlers: SceneHandlers;
  private topInset: number;
  private labels: SceneLabels;
  private readonly tweener: StopMotionTweener;

  private world = new Container();
  private layers = { piles: new Container(), table: new Container(), hand: new Container(), drag: new Container() };
  private views = new Map<string, CardView>();
  private pending = new Map<string, Pending>();
  private model: SceneModel | null = null;
  private metrics: Metrics = computeMetrics(390, 844);
  private drag: DragState | null = null;
  private glowAcc = 0;
  private time = 0;
  private ready = false;
  private destroyed = false;
  private lastOutcome: '' | 'bito' | 'took' = '';
  private timers = new Set<number>();

  // piles
  private deckPile = new Container();
  private deckLabel!: Text;
  private hiddenTrump: CardView | null = null;
  private trumpPlate = new Container();
  private trumpPlateSuit: Suit | '' = '';
  private discardPile = new Container();
  private discardLabel!: Text;
  private stumpPile = new Container();
  private stumpLabel!: Text;
  private stumpShown = -1;
  private dissolved = new Set<string>();

  // trump reveal animation
  private prevRevealed: boolean | null = null;
  private revealTemp: CardView | null = null;
  private revealHidden: string | null = null;

  constructor(handlers: SceneHandlers, opts: SceneOptions = {}) {
    this.handlers = handlers;
    this.topInset = opts.topInset ?? 0;
    this.labels = opts.labels ?? { trump: 'TRUMP' };
    this.tweener = new StopMotionTweener(opts.fps ?? 12);
  }

  async init(host: HTMLElement): Promise<void> {
    await this.app.init({
      resizeTo: host,
      backgroundAlpha: 0,
      antialias: true,
      // Native pixel density: filters and text are rendered at this resolution
      // too, which keeps the vector cards razor sharp on Retina screens.
      resolution: Math.min(window.devicePixelRatio || 1, 3),
      autoDensity: true,
      preference: 'webgl',
    });
    if (this.destroyed) {
      this.app.destroy(true, { children: true });
      return;
    }
    host.appendChild(this.app.canvas);
    this.app.canvas.style.touchAction = 'none';
    this.app.canvas.addEventListener('webglcontextlost', (event) => {
      event.preventDefault();
      this.handlers.onContextLost?.();
    });
    try {
      await Promise.race([document.fonts.load('900 32px Unbounded'), new Promise((r) => setTimeout(r, 1500))]);
    } catch {
      // fall back to the system font stack
    }
    if (this.destroyed) {
      this.app.destroy(true, { children: true });
      return;
    }

    const stage = this.app.stage;
    stage.addChild(this.world);
    this.world.addChild(this.layers.piles, this.layers.table, this.layers.hand, this.layers.drag);
    this.layers.hand.sortableChildren = true;
    this.layers.table.sortableChildren = true;
    this.layers.drag.sortableChildren = true;
    stage.eventMode = 'static';
    stage.hitArea = this.app.screen;
    stage.on('pointermove', this.onPointerMove);
    stage.on('pointerup', this.onPointerUp);
    stage.on('pointerupoutside', this.onPointerUp);
    // iOS / Telegram cancel a touch when a system gesture takes over: never leave a card stuck in the air
    stage.on('pointercancel', this.onPointerCancel);
    document.addEventListener('visibilitychange', this.onVisibility);

    this.app.renderer.on('resize', this.onResize);
    this.app.ticker.add((ticker) => {
      this.time += ticker.deltaMS;
      this.tweener.update(ticker.deltaMS);
      this.glowAcc += ticker.deltaMS;
      if (this.glowAcc >= GLOW_STEP_MS) {
        this.glowAcc = 0;
        for (const view of this.views.values()) view.setGlowPhase(this.time / 1000);
      }
      this.expirePending();
    });

    this.metrics = computeMetrics(this.app.screen.width, this.app.screen.height, this.topInset);
    this.buildPiles();
    this.ready = true;
    if (this.model) this.sync(true);
  }

  update(model: SceneModel): void {
    const first = this.model === null;
    this.model = model;
    if (model.resolving && model.resolveOutcome) this.lastOutcome = model.resolveOutcome;
    if (!first && this.prevRevealed === false && model.trumpRevealed && model.trumpCard) {
      this.playTrumpReveal(model.trumpCard, model.hand.some((c) => c.id === model.trumpCard!.id));
    }
    // A drag that outlived its card (auto-move, took, reconnect) or the phase would block every input.
    if (this.drag && (!model.interactive || !model.hand.some((c) => c.id === this.drag!.id))) this.cancelDrag();
    this.prevRevealed = model.trumpRevealed;
    if (this.ready) this.sync(false);
  }


  /** The HTML panel at the top changed its height: keep the piles below it. */
  setTopInset(px: number): void {
    if (Math.abs(px - this.topInset) < 1) return;
    this.topInset = px;
    if (!this.ready) return;
    this.metrics = computeMetrics(this.app.screen.width, this.app.screen.height, this.topInset);
    this.rebuildPiles();
    this.sync(true);
  }

  /** The connection came back: forget optimistic moves, the state is the truth. */
  clearPending(): void {
    for (const id of this.pending.keys()) this.views.get(id)?.setPending(false);
    this.pending.clear();
    if (this.ready) this.sync(false);
  }

  /** The server rejected a card: snap it back into the hand. */
  rejectCard(cardId: string): void {
    this.pending.delete(cardId);
    const view = this.views.get(cardId);
    if (!view) return;
    view.setPending(false);
    this.returnHome(view, easings.outElastic, 520);
  }

  destroy(): void {
    if (this.destroyed) return;
    this.destroyed = true;
    this.tweener.clear();
    for (const id of this.timers) window.clearTimeout(id);
    this.timers.clear();
    document.removeEventListener('visibilitychange', this.onVisibility);
    if (this.ready) {
      this.app.renderer.off('resize', this.onResize);
      this.app.destroy(true, { children: true });
    }
    this.views.clear();
  }

  /** Read-only snapshot for diagnostics and end-to-end tests (`?debug=1` exposes it as window.__durag). */
  debugSnapshot(): Record<string, unknown> {
    const layerName = (c: Container | null) => (c === this.layers.hand ? 'hand' : c === this.layers.table ? 'table' : c === this.layers.drag ? 'drag' : c === this.layers.piles ? 'piles' : 'none');
    const m = this.model;
    return {
      hand: (m?.hand ?? []).map((c) => {
        const v = this.views.get(c.id);
        return v ? { id: c.id, x: Math.round(v.root.x), y: Math.round(v.root.y), visible: v.root.visible, renderable: v.root.renderable, alpha: v.root.alpha, layer: layerName(v.root.parent) } : { id: c.id, missing: true };
      }),
      table: m?.table.length ?? 0,
      pending: [...this.pending.keys()],
      drag: this.drag?.id ?? null,
      revealHidden: this.revealHidden,
      revealTemp: this.revealTemp !== null,
      trumpRevealed: m?.trumpRevealed ?? false,
      trumpSuit: m?.trumpSuit ?? '',
      glowing: [...this.views.values()].filter((v) => v.isGlowing).map((v) => v.card.id),
      version: m?.version ?? 0,
    };
  }

  // --- layout & sync -------------------------------------------------------

  private onResize = (): void => {
    this.metrics = computeMetrics(this.app.screen.width, this.app.screen.height, this.topInset);
    this.rebuildPiles();
    this.sync(true);
  };

  private sync(immediate: boolean): void {
    const m = this.model;
    if (!m || !this.ready) return;
    const metrics = this.metrics;
    const wanted = new Map<string, Placement>();

    const hs = handSlots(m.hand.length, metrics);
    m.hand.forEach((card, i) => {
      wanted.set(card.id, { card, layer: this.layers.hand, slot: hs[i]!, faceUp: true, z: i, kind: 'hand' });
    });
    const ts = tableSlots(m.table.length, metrics);
    m.table.forEach((pair, i) => {
      wanted.set(pair.attack.id, { card: pair.attack, layer: this.layers.table, slot: ts[i]!.attack, faceUp: true, z: i * 2, kind: 'attack' });
      if (pair.defense) {
        wanted.set(pair.defense.id, { card: pair.defense, layer: this.layers.table, slot: ts[i]!.defense, faceUp: true, z: i * 2 + 1, kind: 'defense' });
      }
    });

    // Cards that left the visible state fly away and are removed.
    for (const [id, view] of this.views) {
      if (wanted.has(id) || this.drag?.id === id) continue;
      if (this.pending.has(id)) continue; // still waiting for the server's verdict
      this.views.delete(id);
      if (view.isDissolved) {
        view.destroy(); // already blown to pixels by the Super card
        continue;
      }
      const wasOnTable = view.root.parent === this.layers.table;
      const outcome = m.lastOutcome || this.lastOutcome;
      const exit = wasOnTable && outcome !== 'took' ? discardSlot(metrics) : exitSlot(metrics);
      view.root.eventMode = 'none';
      view.root.visible = true;
      view.setGlow(false);
      this.reparent(view, this.layers.table);
      void this.tweener.to(view.root, { x: exit.x, y: exit.y, rotation: exit.rotation, scale: exit.scale, alpha: 0.9 }, { duration: 360, ease: easings.outQuad }).then(() => view.destroy());
    }
    if (m.table.length === 0) this.dissolved.clear();
    // The reveal animation is over (or never ran to its end): nothing may stay hidden.
    if (this.revealHidden && !this.revealTemp) this.revealHidden = null;

    // Place every visible card.
    for (const [id, p] of wanted) {
      let view = this.views.get(id);
      let spawned = false;
      if (!view) {
        view = this.createView(p.card, metrics);
        this.views.set(id, view);
        spawned = true;
        const from = p.kind === 'hand' ? deckSlot(metrics) : exitSlot(metrics);
        view.root.position.set(from.x, from.y);
        view.root.rotation = from.rotation;
        view.root.scale.set(from.scale);
        if (p.kind === 'hand' && (m.lastOutcome || this.lastOutcome) === 'took') {
          // cards the viewer just took come from the table centre, not the deck
          const c = nextAttackSlot(0, metrics);
          view.root.position.set(c.x, c.y);
        } else if (p.kind === 'hand' && m.lastEvent === 'stump') {
          // the stump was just picked up: cards come out of the viewer's stump pile
          const sSlot = stumpSlot(metrics);
          view.root.position.set(sSlot.x, sSlot.y);
          view.root.scale.set(sSlot.scale);
        }
        if (p.kind === 'attack' || p.kind === 'defense') this.flash(view);
      }
      if (this.pending.has(id)) {
        if (p.kind === 'attack' || p.kind === 'defense') {
          this.pending.delete(id);
          view.setPending(false);
        } else {
          continue; // state not yet caught up with our optimistic move
        }
      }
      if (this.drag?.id === id) continue;
      // a card the Super card blew apart came back (the defender took the table): it is whole again
      if (view.isDissolved && p.kind === 'hand') view.setDissolved(false);
      // the trump card you beat with glows softly
      view.setGlow(p.kind === 'defense' && m.trumpRevealed && p.card.suit === m.trumpSuit);
      if (view.root.parent !== p.layer) this.reparent(view, p.layer);
      view.root.zIndex = p.z;
      view.setFaceUp(p.faceUp);
      // the freshly drawn trump card is shown by the reveal animation instead
      view.root.visible = this.revealHidden !== id;
      view.root.eventMode = p.kind === 'hand' && m.interactive ? 'static' : 'none';
      view.root.cursor = p.kind === 'hand' && m.interactive ? 'grab' : 'default';
      this.moveTo(view, p.slot, immediate && !spawned);
    }

    // The Super card destroys the card it beats: coarse pixels, no card left.
    for (const pair of m.table) {
      if (pair.defense?.id === 'SC' && !this.dissolved.has(pair.attack.id)) {
        this.dissolved.add(pair.attack.id);
        const target = this.views.get(pair.attack.id);
        if (target) this.later(immediate ? 0 : 380, () => this.disintegrate(target));
      }
    }

    if (m.table.length === 0 && !m.resolving) this.lastOutcome = '';
    this.updatePiles();
  }

  /** Blows a card apart into coarse squares and crosses (stop-motion). */
  private disintegrate(view: CardView): void {
    if (this.destroyed || view.isDissolved) return;
    const { x, y } = view.root.position;
    const scale = view.root.scale.x;
    const w = view.w * scale;
    const h = view.h * scale;
    view.setDissolved(true);
    const colors = [INK, PAPER, PAPER, RED, ACID];
    for (let i = 0; i < 30; i++) {
      const g = new Graphics();
      const size = 4 + Math.round(Math.random() * 6);
      const color = colors[i % colors.length]!;
      if (i % 4 === 0) {
        g.rect(-size, -1.5, size * 2, 3).fill({ color });
        g.rect(-1.5, -size, 3, size * 2).fill({ color });
      } else {
        g.rect(-size / 2, -size / 2, size, size).fill({ color });
      }
      g.position.set(x + (Math.random() - 0.5) * w, y + (Math.random() - 0.5) * h);
      this.layers.drag.addChild(g);
      const angle = Math.random() * Math.PI * 2;
      const dist = 50 + Math.random() * 110;
      void this.tweener
        .to(
          g,
          { x: g.x + Math.cos(angle) * dist, y: g.y + Math.sin(angle) * dist + 30, rotation: (Math.random() - 0.5) * 4, alpha: 0, scale: 0.4 },
          { duration: 420 + Math.random() * 400, ease: easings.outQuad },
        )
        .then(() => g.destroy());
    }
  }

  /** Briefly outlines a card another player just put on the table. */
  private flash(view: CardView): void {
    view.setHighlight(true);
    this.later(900, () => view.setHighlight(false));
  }

  private later(ms: number, fn: () => void): void {
    const id = window.setTimeout(() => {
      this.timers.delete(id);
      if (!this.destroyed) fn();
    }, ms);
    this.timers.add(id);
  }

  private createView(card: Card, m: Metrics): CardView {
    const view = new CardView(card, m.cardW, m.cardH, true);
    view.root.on('pointerdown', (e: FederatedPointerEvent) => this.onCardDown(view, e));
    return view;
  }

  private moveTo(view: CardView, slot: Slot, immediate: boolean): void {
    const r = view.root;
    const same = Math.abs(r.x - slot.x) < 0.5 && Math.abs(r.y - slot.y) < 0.5 && Math.abs(r.rotation - slot.rotation) < 0.01 && Math.abs(r.scale.x - slot.scale) < 0.01;
    if (same) return;
    if (immediate) {
      this.tweener.cancel(r);
      r.position.set(slot.x, slot.y);
      r.rotation = slot.rotation;
      r.scale.set(slot.scale);
      return;
    }
    void this.tweener.to(r, { x: slot.x, y: slot.y, rotation: slot.rotation, scale: slot.scale, alpha: 1 }, { duration: 340, ease: easings.outBack });
  }

  private reparent(view: CardView, layer: Container): void {
    // All layers share the world transform, so coordinates carry over 1:1.
    const { x, y } = view.root.position;
    layer.addChild(view.root);
    view.root.position.set(x, y);
  }

  private expirePending(): void {
    if (this.pending.size === 0) return;
    const now = performance.now();
    let changed = false;
    for (const [id, p] of this.pending) {
      if (p.until < now) {
        this.pending.delete(id);
        this.views.get(id)?.setPending(false);
        changed = true;
      }
    }
    if (changed) this.sync(false);
  }

  private handSlotFor(cardId: string): Slot {
    const m = this.model;
    const idx = m ? m.hand.findIndex((c) => c.id === cardId) : -1;
    if (m && idx >= 0) return handSlots(m.hand.length, this.metrics)[idx]!;
    return { x: this.metrics.width / 2, y: this.metrics.height - this.metrics.cardH / 2 - 58, rotation: 0, scale: 1 };
  }

  // --- trump reveal --------------------------------------------------------------

  /**
   * The face-down trump card at the bottom of the deck was just drawn: show it
   * to everybody in the middle of the table, then send it to its new owner.
   */
  private playTrumpReveal(card: Card, mine: boolean): void {
    if (!this.ready) return;
    const m = this.metrics;
    const temp = new CardView(card, m.cardW, m.cardH, true);
    const from = trumpSlot(m);
    temp.root.position.set(from.x, from.y);
    temp.root.rotation = from.rotation;
    temp.root.scale.set(from.scale);
    temp.root.zIndex = 3000;
    temp.setLifted(true);
    this.layers.drag.addChild(temp.root);
    this.revealTemp?.destroy();
    this.revealTemp = temp;
    if (mine) {
      this.revealHidden = card.id;
      const own = this.views.get(card.id);
      if (own) own.root.visible = false;
    }
    const centre = nextAttackSlot(0, m);
    void this.tweener.to(temp.root, { x: centre.x, y: centre.y - m.cardH * 0.12, rotation: -0.06, scale: 1.3 }, { duration: 480, ease: easings.outBack });

    // Whatever happens to the animation (paused ticker, cancelled tween, an
    // exception), the real card must become visible again: finish() is
    // idempotent and also fired by a hard timer.
    const finish = () => {
      if (this.destroyed || this.revealTemp !== temp) return;
      this.revealTemp = null;
      this.tweener.cancel(temp.root);
      temp.destroy();
      if (this.revealHidden === card.id) {
        this.revealHidden = null;
        const view = this.views.get(card.id);
        if (view) {
          const slot = this.handSlotFor(card.id);
          view.root.visible = true;
          view.root.position.set(slot.x, slot.y);
          view.root.rotation = slot.rotation;
          view.root.scale.set(slot.scale);
        }
      }
      this.updatePiles();
    };
    this.later(REVEAL_HOLD_MS, () => {
      if (this.revealTemp !== temp) return;
      try {
        const dest = mine ? this.handSlotFor(card.id) : exitSlot(this.metrics);
        temp.setLifted(false);
        void this.tweener.to(temp.root, { x: dest.x, y: dest.y, rotation: dest.rotation, scale: dest.scale }, { duration: 460, ease: easings.outQuad }).then(finish);
      } catch (err) {
        console.warn('trump reveal animation failed, showing the card at once', err);
        finish();
      }
    });
    this.later(REVEAL_GUARD_MS, finish);
  }

  // --- piles -----------------------------------------------------------------

  private buildPiles(): void {
    this.deckLabel = new Text({ text: '', style: { fontFamily: FONT, fontWeight: '900', fontSize: 16, fill: PAPER } });
    this.deckLabel.anchor.set(0.5);
    this.discardLabel = new Text({ text: '', style: { fontFamily: FONT, fontWeight: '900', fontSize: 14, fill: 0x8a877f } });
    this.discardLabel.anchor.set(0.5);
    this.stumpLabel = new Text({ text: '', style: { fontFamily: FONT, fontWeight: '900', fontSize: 13, fill: PAPER } });
    this.stumpLabel.anchor.set(1, 1); // right-aligned: the pile sits at the screen edge
    this.stumpPile.eventMode = 'none';
    this.stumpPile.cursor = 'pointer';
    this.stumpPile.on('pointertap', () => {
      if (this.model?.mustTakeStump) this.handlers.onTakeStump?.();
    });
    this.layers.piles.addChild(this.deckPile, this.discardPile, this.trumpPlate, this.stumpPile, this.deckLabel, this.discardLabel, this.stumpLabel);
    this.rebuildPiles();
  }

  private rebuildPiles(): void {
    const m = this.metrics;
    this.deckPile.removeChildren().forEach((c) => c.destroy({ children: true }));
    this.discardPile.removeChildren().forEach((c) => c.destroy({ children: true }));
    this.hiddenTrump?.destroy();
    this.hiddenTrump = null;

    const d = deckSlot(m);
    for (let i = 2; i >= 0; i--) {
      const back = new CardView({ id: `deck-${i}`, suit: 'None', rank: 0 }, m.cardW, m.cardH, false);
      back.root.scale.set(d.scale);
      back.root.position.set(d.x + i * 2, d.y - i * 2);
      back.root.rotation = d.rotation + i * 0.02;
      this.deckPile.addChild(back.root);
    }
    const t = trumpSlot(m);
    const hidden = new CardView({ id: 'trump-hidden', suit: 'None', rank: 0 }, m.cardW, m.cardH, false);
    hidden.root.scale.set(t.scale);
    hidden.root.position.set(t.x, t.y);
    hidden.root.rotation = t.rotation;
    this.hiddenTrump = hidden;
    this.layers.piles.addChildAt(hidden.root, 0);

    this.buildTrumpPlate(m, t);

    const ds = discardSlot(m);
    for (let i = 2; i >= 0; i--) {
      const back = new CardView({ id: `discard-${i}`, suit: 'None', rank: 0 }, m.cardW, m.cardH, false);
      back.root.scale.set(ds.scale);
      back.root.position.set(ds.x - i * 3, ds.y + i * 2);
      back.root.rotation = ds.rotation - i * 0.12;
      this.discardPile.addChild(back.root);
    }
    this.deckLabel.position.set(d.x, d.y + (m.cardH * d.scale) / 2 + 14);
    this.discardLabel.position.set(ds.x, ds.y + (m.cardH * ds.scale) / 2 + 14);
    const st = stumpSlot(m);
    this.stumpLabel.position.set(m.width - 10, st.y - (m.cardH * st.scale) / 2 - 8);
    this.stumpShown = -1;
    this.updatePiles();
  }

  /** The viewer's stump: a rough minimalist stack of face-down cards. */
  private rebuildStumpPile(count: number): void {
    const m = this.metrics;
    this.stumpPile.removeChildren().forEach((c) => c.destroy({ children: true }));
    const st = stumpSlot(m);
    for (let i = count - 1; i >= 0; i--) {
      const back = new CardView({ id: `stump-${i}`, suit: 'None', rank: 0 }, m.cardW, m.cardH, false);
      back.root.scale.set(st.scale);
      back.root.position.set(st.x - i * 2, st.y - i * 3);
      back.root.rotation = st.rotation + (i % 2 ? -0.06 : 0.05);
      this.stumpPile.addChild(back.root);
    }
    this.stumpShown = count;
  }

  /** A loud acid plate with a huge suit glyph: the trump, once it is known. */
  private buildTrumpPlate(m: Metrics, slot: Slot): void {
    this.trumpPlate.removeChildren().forEach((c) => c.destroy({ children: true }));
    const w = m.cardW * 0.82;
    const h = m.cardH * 0.82;
    const shadow = new Graphics().rect(-w / 2 + 5, -h / 2 + 6, w, h).fill({ color: PAPER, alpha: 0.25 });
    const plate = new Graphics().rect(-w / 2, -h / 2, w, h).fill({ color: ACID }).stroke({ width: 3, color: INK, alignment: 1 });
    const label = new Text({ text: this.labels.trump.toUpperCase(), style: { fontFamily: FONT, fontWeight: '900', fontSize: Math.max(7, Math.round(w * 0.13)), fill: INK } });
    label.anchor.set(0.5, 0);
    label.position.set(0, -h / 2 + 6);
    const glyph = drawSuit(new Graphics(), 'None', w * 0.62, INK);
    glyph.label = 'glyph';
    glyph.position.set(0, h * 0.1);
    this.trumpPlate.addChild(shadow, plate, label, glyph);
    this.trumpPlate.position.set(slot.x + m.cardW * 0.12, slot.y + m.cardH * 0.04);
    this.trumpPlate.rotation = 0.08;
    this.trumpPlateSuit = '';
  }

  private updatePiles(): void {
    const m = this.model;
    if (!m) return;
    this.deckPile.visible = m.deckCount > 0;
    this.deckLabel.text = m.deckCount > 0 ? String(m.deckCount) : '';
    this.deckPile.children.forEach((c, i) => {
      c.visible = m.deckCount > (2 - i) * 6;
    });
    // the face-down trump lies under the deck until somebody draws it
    if (this.hiddenTrump) this.hiddenTrump.root.visible = !m.trumpRevealed;
    this.trumpPlate.visible = m.trumpRevealed && this.revealTemp === null;
    if (m.trumpRevealed && m.trumpSuit && m.trumpSuit !== this.trumpPlateSuit) {
      const glyph = this.trumpPlate.getChildByLabel('glyph') as Graphics | null;
      if (glyph) {
        const red = m.trumpSuit === 'Hearts' || m.trumpSuit === 'Diamonds';
        drawSuit(glyph, m.trumpSuit, this.metrics.cardW * 0.82 * 0.62, red ? RED : INK);
      }
      this.trumpPlateSuit = m.trumpSuit;
    }
    this.discardPile.visible = m.discardCount > 0;
    this.discardLabel.text = m.discardCount > 0 ? String(m.discardCount) : '';
    this.discardPile.children.forEach((c, i) => {
      c.visible = m.discardCount > (2 - i) * 8;
    });
    if (m.stumpCount !== this.stumpShown) this.rebuildStumpPile(Math.min(m.stumpCount, 5));
    this.stumpPile.visible = m.stumpCount > 0;
    this.stumpLabel.text = m.stumpCount > 0 ? `×${m.stumpCount}` : ''; // the word itself is in the HUD strip below
    this.stumpPile.eventMode = m.mustTakeStump ? 'static' : 'none';
    this.stumpPile.alpha = m.mustTakeStump ? 1 : 0.85;
    const st = stumpSlot(this.metrics);
    if (m.mustTakeStump) {
      if (this.stumpPile.scale.x === 1) {
        void this.tweener.to(this.stumpPile, { scale: 1.12, rotation: -0.04 }, { duration: 260, ease: easings.outBack });
      }
    } else if (this.stumpPile.scale.x !== 1) {
      this.tweener.cancel(this.stumpPile);
      this.stumpPile.scale.set(1);
      this.stumpPile.rotation = 0;
    }
    this.stumpPile.pivot.set(st.x, st.y);
    this.stumpPile.position.set(st.x, st.y);
  }

  // --- interaction -------------------------------------------------------------

  private onCardDown(view: CardView, e: FederatedPointerEvent): void {
    if (!this.model?.interactive || this.drag || this.pending.has(view.card.id)) return;
    e.stopPropagation();
    const g = e.global;
    this.tweener.cancel(view.root);
    this.reparent(view, this.layers.drag);
    view.setLifted(true);
    view.root.scale.set(1.08);
    view.root.rotation = 0;
    this.drag = {
      id: view.card.id,
      view,
      dx: view.root.x - g.x,
      dy: view.root.y - g.y,
      startX: g.x,
      startY: g.y,
      moved: false,
    };
    const m = this.model;
    if (m.role === 'defender' && m.trumpRevealed && view.card.suit === m.trumpSuit) view.setGlow(true);
  }

  /** Drops the card back into the hand without playing it (cancelled touch, hidden tab, stale drag). */
  private cancelDrag(): void {
    const d = this.drag;
    if (!d) return;
    this.drag = null;
    d.view.setLifted(false);
    d.view.setGlow(false);
    this.clearHighlights();
    this.returnHome(d.view);
  }

  private onPointerCancel = (): void => {
    this.cancelDrag();
  };

  private onVisibility = (): void => {
    if (document.visibilityState === 'hidden') this.cancelDrag();
  };

  private onPointerMove = (e: FederatedPointerEvent): void => {
    const g = e.global;
    const d = this.drag;
    if (!d) return;
    if (!d.moved && Math.hypot(g.x - d.startX, g.y - d.startY) > 7) d.moved = true;
    d.view.root.position.set(g.x + d.dx, g.y + d.dy);
    this.highlightTargets(d.view.card, g.x, g.y);
  };

  private onPointerUp = (e: FederatedPointerEvent): void => {
    const d = this.drag;
    if (!d) return;
    this.drag = null;
    d.view.setLifted(false);
    d.view.setGlow(false); // the confirmed defense gets its glow back from sync()
    this.clearHighlights();
    const m = this.model;
    if (!m || !m.interactive) {
      this.returnHome(d.view);
      return;
    }
    const g = e.global;
    // Any release without movement is a tap, however long the finger rested:
    // dropping a card back where it was would be a no-op anyway, and a slow
    // frame must not turn a tap into an ignored move.
    const quick = !d.moved;
    if (quick) {
      this.quickPlay(d.view);
      return;
    }
    const inTable = g.y < this.metrics.handTop - 8;
    const target = this.attackAt(g.x, g.y);
    if (m.role === 'defender') {
      if (target) {
        this.commit(d.view, target.slot, () => this.handlers.onPlay(d.id, target.card.id));
        return;
      }
      if (inTable && m.table.length > 0) {
        this.commit(d.view, nextAttackSlot(m.table.length, this.metrics), () => this.handlers.onTransfer(d.id));
        return;
      }
    } else if ((m.role === 'attacker' || m.role === 'thrower') && inTable) {
      this.commit(d.view, nextAttackSlot(m.table.length, this.metrics), () => this.handlers.onPlay(d.id));
      return;
    }
    this.returnHome(d.view);
  };

  /** Tap without dragging: pick the most plausible move for the card. */
  private quickPlay(view: CardView): void {
    const m = this.model!;
    const card = view.card;
    if (m.role === 'attacker' || m.role === 'thrower') {
      this.commit(view, nextAttackSlot(m.table.length, this.metrics), () => this.handlers.onPlay(card.id));
      return;
    }
    if (m.role === 'defender' && m.table.length > 0) {
      const trump = m.trumpRevealed ? m.trumpSuit : '';
      const slots = tableSlots(m.table.length, this.metrics);
      const idx = m.table.findIndex((p) => !p.defense && canBeat(p.attack, card, trump));
      if (idx >= 0) {
        this.commit(view, slots[idx]!.defense, () => this.handlers.onPlay(card.id, m.table[idx]!.attack.id));
        return;
      }
      const transferable = !isSuper(card) && !m.defenderTaking && m.table.every((p) => !p.defense && p.attack.rank === card.rank);
      if (transferable) {
        this.commit(view, nextAttackSlot(m.table.length, this.metrics), () => this.handlers.onTransfer(card.id));
        return;
      }
    }
    this.shake(view);
  }

  private commit(view: CardView, slot: Slot, send: () => void): void {
    this.pending.set(view.card.id, { slot, until: performance.now() + PENDING_TTL });
    this.reparent(view, this.layers.table);
    view.root.zIndex = 1000;
    view.root.eventMode = 'none';
    view.setPending(true);
    void this.tweener.to(view.root, { x: slot.x, y: slot.y, rotation: slot.rotation, scale: slot.scale }, { duration: 160, ease: easings.outQuad });
    send();
  }

  private returnHome(view: CardView, ease = easings.outBack, duration = 340): void {
    const m = this.model;
    const idx = m ? m.hand.findIndex((c) => c.id === view.card.id) : -1;
    if (!m || idx < 0) {
      this.sync(false);
      return;
    }
    const slot = handSlots(m.hand.length, this.metrics)[idx]!;
    this.reparent(view, this.layers.hand);
    view.root.zIndex = idx;
    view.root.eventMode = m.interactive ? 'static' : 'none';
    void this.tweener.to(view.root, { x: slot.x, y: slot.y, rotation: slot.rotation, scale: slot.scale, alpha: 1 }, { duration, ease });
  }

  private shake(view: CardView): void {
    const m = this.model;
    const idx = m ? m.hand.findIndex((c) => c.id === view.card.id) : -1;
    const slot = m && idx >= 0 ? handSlots(m.hand.length, this.metrics)[idx]! : null;
    this.reparent(view, this.layers.hand);
    if (!slot) return;
    view.root.position.set(slot.x, slot.y);
    view.root.scale.set(slot.scale);
    view.root.rotation = slot.rotation + 0.25;
    void this.tweener.to(view.root, { rotation: slot.rotation, scale: slot.scale }, { duration: 420, ease: easings.outElastic });
  }

  private attackAt(x: number, y: number): { card: Card; slot: Slot } | null {
    const m = this.model;
    if (!m) return null;
    const slots = tableSlots(m.table.length, this.metrics);
    for (let i = m.table.length - 1; i >= 0; i--) {
      const pair = m.table[i]!;
      if (pair.defense) continue;
      if (hitSlot(slots[i]!.attack, this.metrics, x, y, 1.3)) return { card: pair.attack, slot: slots[i]!.defense };
    }
    return null;
  }

  private highlightTargets(card: Card, x: number, y: number): void {
    const m = this.model;
    if (!m || m.role !== 'defender') return;
    const trump = m.trumpRevealed ? m.trumpSuit : '';
    const slots = tableSlots(m.table.length, this.metrics);
    m.table.forEach((pair, i) => {
      const view = this.views.get(pair.attack.id);
      if (!view) return;
      const beatable = !pair.defense && canBeat(pair.attack, card, trump);
      const hovered = hitSlot(slots[i]!.attack, this.metrics, x, y, 1.3);
      view.setHighlight(beatable && hovered);
    });
  }

  private clearHighlights(): void {
    for (const view of this.views.values()) view.setHighlight(false);
  }
}
