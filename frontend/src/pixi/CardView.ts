// A playing card drawn entirely with vector primitives: white face, 3px ink
// border, a hard black shadow offset by 6px (no blur), huge corner index in
// a wide grotesque and a crisp SVG suit. Special cards get their own art.
import { Container, Graphics, Text, type Filter } from 'pixi.js';
import type { Card } from '../types/protocol';
import { cardColor, isJoker, isSuper, rankLabel } from '../util/cards';
import { drawSuit } from './suits';

const INK = 0x111111;
const PAPER = 0xffffff;
const RED = 0xff2a1a;
const ACID = 0xe6ff00;
const SHADOW_DX = 6;
const SHADOW_DY = 7;
const FONT = 'Unbounded, "Arial Black", Impact, sans-serif';

export class CardView {
  readonly root = new Container();
  readonly card: Card;
  readonly w: number;
  readonly h: number;

  private shadow = new Graphics();
  private face = new Container();
  private back = new Container();
  private highlight = new Graphics();
  private lifted = false;
  private _faceUp = true;

  constructor(card: Card, w: number, h: number, faceUp = true, hologram?: Filter) {
    this.card = card;
    this.w = w;
    this.h = h;
    this.buildShadow();
    this.buildFace(hologram);
    this.buildBack();
    this.buildHighlight();
    this.root.addChild(this.shadow, this.back, this.face, this.highlight);
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

  destroy(): void {
    this.root.destroy({ children: true });
  }

  private buildShadow(): void {
    const { w, h } = this;
    this.shadow.rect(-w / 2, -h / 2, w, h).fill({ color: INK });
    this.shadow.position.set(SHADOW_DX, SHADOW_DY);
  }

  private buildHighlight(): void {
    const { w, h } = this;
    this.highlight.rect(-w / 2 - 4, -h / 2 - 4, w + 8, h + 8).stroke({ width: 4, color: ACID });
    this.highlight.visible = false;
  }

  private buildFace(hologram?: Filter): void {
    const { w, h, card } = this;
    const color = cardColor(card) === 'red' ? RED : INK;
    const plate = new Graphics().rect(-w / 2, -h / 2, w, h).fill({ color: PAPER }).stroke({ width: 3, color: INK, alignment: 1 });
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
    const plate = new Graphics().rect(-w / 2, -h / 2, w, h).fill({ color: INK });
    const inner = new Graphics().rect(-w / 2 + 6, -h / 2 + 6, w - 12, h - 12).fill({ color: 0xf2f0ea });
    const hatch = new Graphics();
    for (let i = -h; i < h + w; i += 7) {
      hatch.moveTo(-w / 2 + 6, i).lineTo(w / 2 - 6, i - (w - 12) * 0.9);
    }
    hatch.stroke({ width: 1.5, color: INK });
    const hatchMask = new Graphics().rect(-w / 2 + 6, -h / 2 + 6, w - 12, h - 12).fill({ color: 0xffffff });
    hatch.mask = hatchMask;
    const word = new Text({
      text: 'DURAG',
      style: { fontFamily: FONT, fontWeight: '900', fontSize: Math.round(w * 0.2), fill: 0xf2f0ea, letterSpacing: 1 },
    });
    word.anchor.set(0.5, 0.5);
    const wordBg = new Graphics().rect(-word.width / 2 - 6, -word.height / 2 - 2, word.width + 12, word.height + 4).fill({ color: INK });
    const wordBox = new Container();
    wordBox.addChild(wordBg, word);
    wordBox.rotation = -Math.PI / 2 + 0.08;
    this.back.addChild(plate, inner, hatch, hatchMask, wordBox);
  }
}
