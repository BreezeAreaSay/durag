// Suit glyphs as clean vector outlines (SVG path data in a 100x100 box).
import { Graphics, GraphicsPath } from 'pixi.js';
import type { Suit } from '../types/protocol';

const HEART = 'M50 90 C22 64 4 48 4 28 C4 14 14 6 26 6 C36 6 45 12 50 21 C55 12 64 6 74 6 C86 6 96 14 96 28 C96 48 78 64 50 90 Z';
const DIAMOND = 'M50 3 L94 50 L50 97 L6 50 Z';
const SPADE = 'M50 5 C32 30 8 44 8 62 C8 76 18 85 30 85 C38 85 45 81 50 75 C55 81 62 85 70 85 C82 85 92 76 92 62 C92 44 68 30 50 5 Z';
const SPADE_STEM = 'M44 74 L56 74 L63 97 L37 97 Z';
const CLUB_STEM = 'M44 58 L56 58 L62 97 L38 97 Z';

/** Draws a suit centred at (0,0) with the given size into the Graphics. */
export function drawSuit(g: Graphics, suit: Suit, size: number, color: number): Graphics {
  g.clear();
  const s = size / 100;
  g.scale.set(s);
  g.pivot.set(50, 50);
  switch (suit) {
    case 'Hearts':
      g.path(new GraphicsPath(HEART)).fill({ color });
      break;
    case 'Diamonds':
      g.path(new GraphicsPath(DIAMOND)).fill({ color });
      break;
    case 'Spades':
      g.path(new GraphicsPath(SPADE)).fill({ color });
      g.path(new GraphicsPath(SPADE_STEM)).fill({ color });
      break;
    case 'Clubs':
      g.circle(50, 28, 20).fill({ color });
      g.circle(29, 60, 20).fill({ color });
      g.circle(71, 60, 20).fill({ color });
      g.path(new GraphicsPath(CLUB_STEM)).fill({ color });
      break;
    default:
      g.star(50, 50, 5, 46, 20).fill({ color });
  }
  return g;
}
