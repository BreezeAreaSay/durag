// A playing card drawn entirely with vector primitives: white face, 3px ink
// border, a hard black shadow offset by 6px (no blur), huge corner index in
// a wide grotesque and a crisp SVG suit. Special cards get their own art.
import { Container, Graphics, Text, type Filter } from 'pixi.js';
import type { Card } from '../types/protocol';
import { cardColor, isJoker, isSuper, rankLabel } from '../util/cards';
import { drawSuit } from './suits';

const INK = 0x0d0d0d;
const PAPER = 0xece9e1;
const RED = 0xcf1fff; // "red" suits are toxic magenta in the v10 palette
const ACID = 0xe6ff00;
const SHADOW_DX = 6;
const SHADOW_DY = 7;
const FONT = 'Unbounded, "Arial Black", Impact, sans-serif';

function hash(seed: string, i: number): number {
  let h = 2166136261 ^ i;
  for (let k = 0; k < seed.length; k++) h = Math.imul(h ^ seed.charCodeAt(k), 16777619);
  h ^= h >>> 13;
  h = Math.imul(h, 1274126177);
  return ((h >>> 0) % 1000) / 1000;
}

/**
 * A rectangle whose edges wobble by a pixel or two, like a badly printed
 * flyer. Deterministic per seed so a card always has the same contour.
 */
export function jaggedRect(w: number, h: number, seed: string, grow = 0, step = 7, jitter = 1.6): number[] {
  const pts: number[] = [];
  const x0 = -w / 2 - grow;
  const y0 = -h / 2 - grow;
  const x1 = w / 2 + grow;
  const y1 = h / 2 + grow;
  let i = 0;
  const push = (x: number, y: number, nx: number, ny: number) => {
    const j = (hash(seed, i++) - 0.5) * 2 * jitter;
    pts.push(x + nx * j, y + ny * j);
  };
  for (let x = x0; x < x1; x += step) push(x, y0, 0, 1);
  for (let y = y0; y < y1; y += step) push(x1, y, 1, 0);
  for (let x = x1; x > x0; x -= step) push(x, y1, 0, 1);
  for (let y = y1; y > y0; y -= step) push(x0, y, 1, 0);
  return pts;
}

export class CardView {
  readonly root = new Container();
  readonly card: Card;
  readonly w: number;
  readonly h: number;

  private glow = new Graphics();
  private glowOn = false;
  private shadow = new Graphics();
  private face = new Container();
  private back = new Container();
  private highlight = new Graphics();
  private lifted = false;
  private _faceUp = true;
  private dissolved = false;

  constructor(card: Card, w: number, h: number, faceUp = true, hologram?: Filter) {
    this.card = card;
    this.w = w;
    this.h = h;
    this.buildGlow();
    this.buildShadow();
    this.buildFace(hologram);
    this.buildBack();
    this.buildHighlight();
    this.root.addChild(this.glow, this.shadow, this.back, this.face, this.highlight);
    this.setFaceUp(faceUp);
    this.root.label = `card:${card.id}`;
  }

  get faceUp(): boolean {
    return this._faceUp;
  }

  setFaceUp(v: boolean): void {
    this._faceUp = v;
    this.face.visible = v;
    this.back.visible = !v;
  }

  setHighlight(on: boolean): void {
    this.highlight.visible = on;
  }

  setPending(on: boolean): void {
    this.root.alpha = on ? 0.82 : 1;
  }

  /** Attaches or removes the pointer-driven hologram (Super card only). */
  setHologram(filter: Filter | null): void {
    if (!isSuper(this.card)) return;
    this.face.filters = filter ? [filter] : null;
  }

  setLifted(on: boolean): void {
    if (this.lifted === on) return;
    this.lifted = on;
    this.shadow.position.set(on ? SHADOW_DX * 2 : SHADOW_DX, on ? SHADOW_DY * 2.2 : SHADOW_DY);
  }

  /** A soft acid halo: the trump card you are beating with. */
  setGlow(on: boolean): void {
    if (this.glowOn === on) return;
    this.glowOn = on;
    this.glow.visible = on;
    this.glow.alpha = 1;
  }

  get isGlowing(): boolean {
    return this.glowOn;
  }

  /** Slow breathing of the halo (called on the stop-motion cadence). */
  setGlowPhase(seconds: number): void {
    if (!this.glowOn) return;
    this.glow.alpha = 0.7 + 0.3 * (0.5 + 0.5 * Math.sin(seconds * 2.4));
  }

  /** A card the Super card destroyed: it is gone visually but keeps its slot. */
  setDissolved(on: boolean): void {
    this.dissolved = on;
    this.root.renderable = !on;
    this.root.eventMode = on ? 'none' : this.root.eventMode;
  }

  get isDissolved(): boolean {
    return this.dissolved;
  }

  destroy(): void {
    this.root.destroy({ children: true });
  }

  private buildShadow(): void {
    const { w, h } = this;
    // on dark paper the hard offset "shadow" is a pale misregistered double print
    this.shadow.poly(jaggedRect(w, h, this.card.id + ':s')).fill({ color: PAPER, alpha: 0.22 });
    this.shadow.position.set(SHADOW_DX, SHADOW_DY);
  }

  private buildGlow(): void {
    const { w, h } = this;
    // three jagged rings of falling alpha stand in for a blur: a faint light, not a neon sign
    this.glow.poly(jaggedRect(w, h, this.card.id + ':g3', 18, 11, 1.5)).fill({ color: ACID, alpha: 0.05 });
    this.glow.poly(jaggedRect(w, h, this.card.id + ':g2', 11, 10, 1.3)).fill({ color: ACID, alpha: 0.08 });
    this.glow.poly(jaggedRect(w, h, this.card.id + ':g1', 5, 9, 1.2)).fill({ color: ACID, alpha: 0.14 });
    this.glow.visible = false;
  }

  private buildHighlight(): void {
    const { w, h } = this;
    this.highlight.poly(jaggedRect(w, h, this.card.id + ':h', 4, 9, 1.2)).stroke({ width: 4, color: ACID });
    this.highlight.visible = false;
  }

  private buildFace(hologram?: Filter): void {
    const { w, h, card } = this;
    const color = cardColor(card) === 'red' ? RED : INK;
    const plate = new Graphics().poly(jaggedRect(w, h, card.id)).fill({ color: PAPER }).stroke({ width: 3, color: INK, alignment: 0.5 });
    this.face.addChild(plate);

    if (isSuper(card)) {
      this.buildSuperFace(hologram);
      return;
    }
    if (isJoker(card)) {
      this.buildJokerFace(color);
      return;
    }

    const label = rankLabel(card);
    const index = new Text({
      text: label,
      style: {
        fontFamily: FONT,
        fontWeight: '900',
        fontSize: Math.round(w * (label.length > 1 ? 0.3 : 0.4)),
        fill: color,
        letterSpacing: -1,
      },
    });
    index.anchor.set(0, 0);
    index.position.set(-w / 2 + w * 0.08, -h / 2 + h * 0.04);
    this.face.addChild(index);

    const mini = drawSuit(new Graphics(), card.suit, w * 0.2, color);
    mini.position.set(-w / 2 + w * 0.08 + w * 0.1, -h / 2 + h * 0.04 + index.height + w * 0.1);
    this.face.addChild(mini);

    const big = drawSuit(new Graphics(), card.suit, w * 0.62, color);
    big.position.set(w * 0.12, h * 0.2);
    this.face.addChild(big);

    // bottom-right rotated index (so the card reads from the opposite side too)
    const index2 = new Text({ text: label, style: { fontFamily: FONT, fontWeight: '900', fontSize: Math.round(w * 0.2), fill: color } });
    index2.anchor.set(0, 0);
    index2.rotation = Math.PI;
    index2.position.set(w / 2 - w * 0.08, h / 2 - h * 0.04);
    this.face.addChild(index2);
  }

  private buildJokerFace(color: number): void {
    const { w, h, card } = this;
    const word = new Text({
      text: 'JOKER',
      style: { fontFamily: FONT, fontWeight: '900', fontSize: Math.round(w * 0.22), fill: color, letterSpacing: 1 },
    });
    word.anchor.set(0.5, 0.5);
    word.rotation = -Math.PI / 2;
    word.position.set(-w / 2 + w * 0.2, 0);
    this.face.addChild(word);

    const star = new Graphics().star(0, 0, 5, w * 0.3, w * 0.13).fill({ color }).stroke({ width: 3, color: INK });
    star.position.set(w * 0.12, -h * 0.08);
    star.rotation = 0.3;
    this.face.addChild(star);

    const tag = new Text({
      text: card.id === 'RJ' ? 'RED' : 'BLK',
      style: { fontFamily: FONT, fontWeight: '800', fontSize: Math.round(w * 0.16), fill: PAPER },
    });
    const tagBg = new Graphics().rect(0, 0, tag.width + 10, tag.height + 4).fill({ color: INK });
    tag.position.set(5, 2);
    const badge = new Container();
    badge.addChild(tagBg, tag);
    badge.rotation = -0.12;
    badge.position.set(-w * 0.1, h * 0.22);
    this.face.addChild(badge);
  }

  private buildSuperFace(hologram?: Filter): void {
    const { w, h } = this;
    const stripes = new Graphics();
    for (let i = -h; i < h; i += 9) {
      stripes.moveTo(-w / 2, i).lineTo(w / 2, i + w * 0.6);
    }
    stripes.stroke({ width: 2, color: 0xdddddd });
    const mask = new Graphics().rect(-w / 2 + 2, -h / 2 + 2, w - 4, h - 4).fill({ color: 0xffffff });
    stripes.mask = mask;
    this.face.addChild(stripes, mask);

    const s = new Text({
      text: 'S',
      style: { fontFamily: FONT, fontWeight: '900', fontSize: Math.round(w * 0.78), fill: INK, letterSpacing: -4 },
    });
    s.anchor.set(0.5, 0.5);
    s.position.set(0, -h * 0.05);
    this.face.addChild(s);

    const word = new Text({
      text: 'SUPER',
      style: { fontFamily: FONT, fontWeight: '900', fontSize: Math.round(w * 0.17), fill: INK, letterSpacing: 2 },
    });
    word.anchor.set(0.5, 0.5);
    word.position.set(0, h * 0.34);
    const bg = new Graphics().rect(-word.width / 2 - 6, -word.height / 2 - 2, word.width + 12, word.height + 4).fill({ color: ACID }).stroke({ width: 2, color: INK });
    bg.position.set(0, h * 0.34);
    bg.rotation = word.rotation = -0.06;
    this.face.addChild(bg, word);

    if (hologram) this.face.filters = [hologram];
  }

  private buildBack(): void {
    const { w, h } = this;
    const plate = new Graphics().poly(jaggedRect(w, h, this.card.id + ':b')).fill({ color: INK }).stroke({ width: 2, color: PAPER, alpha: 0.9 });
    const inner = new Graphics().rect(-w / 2 + 6, -h / 2 + 6, w - 12, h - 12).fill({ color: PAPER });
    const hatch = new Graphics();
    for (let i = -h; i < h + w; i += 7) {
      hatch.moveTo(-w / 2 + 6, i).lineTo(w / 2 - 6, i - (w - 12) * 0.9);
    }
    hatch.stroke({ width: 1.5, color: INK });
    const hatchMask = new Graphics().rect(-w / 2 + 6, -h / 2 + 6, w - 12, h - 12).fill({ color: 0xffffff });
    hatch.mask = hatchMask;
    const word = new Text({
      text: 'DURAG',
      style: { fontFamily: FONT, fontWeight: '900', fontSize: Math.round(w * 0.2), fill: PAPER, letterSpacing: 1 },
    });
    word.anchor.set(0.5, 0.5);
    const wordBg = new Graphics().rect(-word.width / 2 - 6, -word.height / 2 - 2, word.width + 12, word.height + 4).fill({ color: INK });
    const wordBox = new Container();
    wordBox.addChild(wordBg, word);
    wordBox.rotation = -Math.PI / 2 + 0.08;
    this.back.addChild(plate, inner, hatch, hatchMask, wordBox);
  }
}
