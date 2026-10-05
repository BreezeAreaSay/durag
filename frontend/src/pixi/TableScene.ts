// The game table: an authoritative-state renderer. Every STATE_UPDATE is
// turned into a SceneModel and `sync()` moves cards to where the state says
// they are. Drag-and-drop sends intents optimistically; an ERROR from the
// server snaps the card back into the hand with a stop-motion rubber band.
import { Application, Container, Graphics, Text, type FederatedPointerEvent, type Filter } from 'pixi.js';
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
  tableSlots,
  trumpSlot,
  type Metrics,
  type Slot,
} from './layout';
import { createChromaticFilter, createHalftoneFilter, createHologramFilter, filterCompiles, type HologramFilter } from './shaders';
import { drawSuit } from './suits';
import { canBeat } from '../util/rules';
import { isSuper } from '../util/cards';
import type { Card, Suit } from '../types/protocol';
import type { SceneModel } from './model';

export interface SceneHandlers {
  onPlay(cardId: string, targetId?: string): void;
  onTransfer(cardId: string): void;
}

export interface SceneLabels {
  trump: string;
}

export interface SceneOptions {
  fx?: boolean;
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
  startTime: number;
  moved: boolean;
}

interface Pending {
  slot: Slot;
  until: number;
}

const FONT = 'Unbounded, "Arial Black", Impact, sans-serif';
const PENDING_TTL = 2500;
const INK = 0x111111;
const ACID = 0xe6ff00;
const RED = 0xff2a1a;

export class TableScene {
  readonly app = new Application();

  private readonly handlers: SceneHandlers;
  private fx: boolean;
  private fxActive = false;
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
  private hologram: HologramFilter | null = null;
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

  // trump reveal animation
  private prevRevealed: boolean | null = null;
  private revealTemp: CardView | null = null;
  private revealHidden: string | null = null;

  constructor(handlers: SceneHandlers, opts: SceneOptions = {}) {
    this.handlers = handlers;
    this.fx = opts.fx ?? true;
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

    if (this.fx) this.enableFx();

    this.app.renderer.on('resize', this.onResize);
    this.app.ticker.add((ticker) => {
      this.time += ticker.deltaMS;
      this.tweener.update(ticker.deltaMS);
      this.hologram?.setTime(this.time / 1000);
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
    this.prevRevealed = model.trumpRevealed;
    if (this.ready) this.sync(false);
  }

  /** Turns the shader effects on or off at runtime (HUD toggle). */
  setFx(on: boolean): void {
    this.fx = on;
    if (!this.ready) return;
    if (on && !this.fxActive) this.enableFx();
    else if (!on && this.fxActive) this.disableFx();
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
    if (this.ready) {
      this.app.renderer.off('resize', this.onResize);
      this.app.destroy(true, { children: true });
    }
    this.views.clear();
  }

  // --- effects ---------------------------------------------------------------

  // Deliberately gentle: a faint riso screen in the mid-tones and a sub-pixel
  // RGB misregistration. Anything stronger reads as a compression artefact on
  // small screens, and the cards must stay razor sharp.
  private enableFx(): void {
    const quality = { resolution: this.app.renderer.resolution };
    const halftone = createHalftoneFilter({ dotSize: 3, strength: 0.28, ...quality });
    const chromatic = createChromaticFilter(0.4, quality);
    const hologram = createHologramFilter(0.6, quality);
    if (![halftone, chromatic, hologram].every((f) => filterCompiles(this.app.renderer, f))) return;
    this.hologram = hologram;
    this.world.filters = [halftone, chromatic];
    this.fxActive = true;
    this.applyHologram(hologram);
  }

  private disableFx(): void {
    const old = this.world.filters;
    this.world.filters = null;
    if (Array.isArray(old)) old.forEach((f) => f.destroy());
    this.applyHologram(null);
    this.hologram?.destroy();
    this.hologram = null;
    this.fxActive = false;
  }

  private applyHologram(filter: Filter | null): void {
    for (const view of this.views.values()) view.setHologram(filter);
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
      const wasOnTable = view.root.parent === this.layers.table;
      const outcome = m.lastOutcome || this.lastOutcome;
      const exit = wasOnTable && outcome !== 'took' ? discardSlot(metrics) : exitSlot(metrics);
      view.root.eventMode = 'none';
      view.root.visible = true;
      this.reparent(view, this.layers.table);
      void this.tweener.to(view.root, { x: exit.x, y: exit.y, rotation: exit.rotation, scale: exit.scale, alpha: 0.9 }, { duration: 360, ease: easings.outQuad }).then(() => view.destroy());
    }

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
      if (view.root.parent !== p.layer) this.reparent(view, p.layer);
      view.root.zIndex = p.z;
      view.setFaceUp(p.faceUp);
      // the freshly drawn trump card is shown by the reveal animation instead
      view.root.visible = this.revealHidden !== id;
      view.root.eventMode = p.kind === 'hand' && m.interactive ? 'static' : 'none';
      view.root.cursor = p.kind === 'hand' && m.interactive ? 'grab' : 'default';
      this.moveTo(view, p.slot, immediate && !spawned);
    }

    if (m.table.length === 0 && !m.resolving) this.lastOutcome = '';
    this.updatePiles();
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
    const view = new CardView(card, m.cardW, m.cardH, true, isSuper(card) && this.hologram ? this.hologram : undefined);
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
    const temp = new CardView(card, m.cardW, m.cardH, true, isSuper(card) && this.hologram ? this.hologram : undefined);
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
      this.views.get(card.id)?.root && (this.views.get(card.id)!.root.visible = false);
    }
    const centre = nextAttackSlot(0, m);
    void this.tweener.to(temp.root, { x: centre.x, y: centre.y - m.cardH * 0.12, rotation: -0.06, scale: 1.3 }, { duration: 480, ease: easings.outBack });
    this.later(1600, () => {
      const dest = mine ? this.handSlotFor(card.id) : exitSlot(this.metrics);
      temp.setLifted(false);
      void this.tweener.to(temp.root, { x: dest.x, y: dest.y, rotation: dest.rotation, scale: dest.scale }, { duration: 460, ease: easings.outQuad }).then(() => {
        if (this.destroyed) return;
        temp.destroy();
        if (this.revealTemp === temp) this.revealTemp = null;
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
      });
    });
  }

  // --- piles -----------------------------------------------------------------

  private buildPiles(): void {
    this.deckLabel = new Text({ text: '', style: { fontFamily: FONT, fontWeight: '900', fontSize: 16, fill: INK } });
    this.deckLabel.anchor.set(0.5);
    this.discardLabel = new Text({ text: '', style: { fontFamily: FONT, fontWeight: '900', fontSize: 14, fill: 0x8a8780 } });
    this.discardLabel.anchor.set(0.5);
    this.layers.piles.addChild(this.deckPile, this.discardPile, this.trumpPlate, this.deckLabel, this.discardLabel);
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
    this.updatePiles();
  }

  /** A loud acid plate with a huge suit glyph: the trump, once it is known. */
  private buildTrumpPlate(m: Metrics, slot: Slot): void {
    this.trumpPlate.removeChildren().forEach((c) => c.destroy({ children: true }));
    const w = m.cardW * 0.82;
    const h = m.cardH * 0.82;
    const shadow = new Graphics().rect(-w / 2 + 5, -h / 2 + 6, w, h).fill({ color: INK });
    const plate = new Graphics().rect(-w / 2, -h / 2, w, h).fill({ color: ACID }).stroke({ width: 3, color: INK, alignment: 1 });
    const label = new Text({ text: this.labels.trump.toUpperCase(), style: { fontFamily: FONT, fontWeight: '900', fontSize: Math.max(9, Math.round(w * 0.16)), fill: INK, letterSpacing: 1 } });
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
      startTime: performance.now(),
      moved: false,
    };
  }

  private onPointerMove = (e: FederatedPointerEvent): void => {
    const g = e.global;
    this.hologram?.setPointer(g.x / Math.max(1, this.metrics.width), g.y / Math.max(1, this.metrics.height));
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
    this.clearHighlights();
    const m = this.model;
    if (!m || !m.interactive) {
      this.returnHome(d.view);
      return;
    }
    const g = e.global;
    const quick = !d.moved && performance.now() - d.startTime < 350;
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
