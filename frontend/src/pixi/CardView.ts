// A playing card drawn entirely with vector primitives: straight paper face
// with a 3px ink border, a pale offset double print for a shadow (no blur),
// huge corner index in a wide grotesque and a crisp SVG suit. Special cards
// get their own art. No shader filters anywhere: WebKit on iPhones rendered
// filtered cards as black rectangles.
import { Container, FillGradient, Graphics, Text } from 'pixi.js';
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
const CORNER = 4; // card corner radius: straight, calm cards (the torn contours got in the way)

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

  constructor(card: Card, w: number, h: number, faceUp = true) {
    this.card = card;
    this.w = w;
    this.h = h;
    this.buildGlow();
    this.buildShadow();
    this.buildFace();
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
    this.shadow.roundRect(-w / 2, -h / 2, w, h, CORNER).fill({ color: PAPER, alpha: 0.22 });
    this.shadow.position.set(SHADOW_DX, SHADOW_DY);
  }

  private buildGlow(): void {
    const { w, h } = this;
    // three rings of falling alpha stand in for a blur: a faint light, not a neon sign
    this.glow.roundRect(-w / 2 - 18, -h / 2 - 18, w + 36, h + 36, CORNER + 18).fill({ color: ACID, alpha: 0.05 });
    this.glow.roundRect(-w / 2 - 11, -h / 2 - 11, w + 22, h + 22, CORNER + 11).fill({ color: ACID, alpha: 0.08 });
    this.glow.roundRect(-w / 2 - 5, -h / 2 - 5, w + 10, h + 10, CORNER + 5).fill({ color: ACID, alpha: 0.14 });
    this.glow.visible = false;
  }

  private buildHighlight(): void {
    const { w, h } = this;
    this.highlight.roundRect(-w / 2 - 4, -h / 2 - 4, w + 8, h + 8, CORNER + 4).stroke({ width: 4, color: ACID });
    this.highlight.visible = false;
  }

  private buildFace(): void {
    const { w, h, card } = this;
    const color = cardColor(card) === 'red' ? RED : INK;
    const plate = new Graphics().roundRect(-w / 2, -h / 2, w, h, CORNER).fill({ color: PAPER }).stroke({ width: 3, color: INK, alignment: 0.5 });
    this.face.addChild(plate);

    if (isSuper(card)) {
      this.buildSuperFace();
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

  /** The Super card: a static foil — a diagonal rainbow gradient under hatching, no shader. */
  private buildSuperFace(): void {
    const { w, h } = this;
    const foil = new FillGradient({
      type: 'linear',
      start: { x: 0, y: 0 },
      end: { x: 1, y: 1 },
      colorStops: [
        { offset: 0, color: 0xcf1fff },
        { offset: 0.3, color: 0xe6ff00 },
        { offset: 0.55, color: 0x19e6ff },
        { offset: 0.8, color: 0xff3fb0 },
        { offset: 1, color: 0xe6ff00 },
      ],
      textureSpace: 'local',
    });
    const sheet = new Graphics().roundRect(-w / 2 + 3, -h / 2 + 3, w - 6, h - 6, CORNER).fill({ fill: foil, alpha: 0.55 });
    this.face.addChild(sheet);

    const stripes = new Graphics();
    for (let i = -h; i < h; i += 9) {
      stripes.moveTo(-w / 2, i).lineTo(w / 2, i + w * 0.6);
    }
    stripes.stroke({ width: 2, color: PAPER, alpha: 0.7 });
    const mask = new Graphics().roundRect(-w / 2 + 3, -h / 2 + 3, w - 6, h - 6, CORNER).fill({ color: 0xffffff });
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
  }

  private buildBack(): void {
    const { w, h } = this;
    const plate = new Graphics().roundRect(-w / 2, -h / 2, w, h, CORNER).fill({ color: INK }).stroke({ width: 2, color: PAPER, alpha: 0.9 });
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
