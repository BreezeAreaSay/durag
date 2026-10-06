// Pure layout maths for the table scene. No PixiJS imports: unit-testable.

export interface Slot {
  x: number;
  y: number;
  rotation: number;
  scale: number;
}

export interface Metrics {
  width: number;
  height: number;
  cardW: number;
  cardH: number;
  topZone: number; // reserved for the HTML HUD (opponents, status)
  handTop: number; // y where the hand zone starts
  tableTop: number;
  tableBottom: number;
}

const PILE_SCALE = 0.72;

function clamp(v: number, lo: number, hi: number): number {
  return Math.max(lo, Math.min(hi, v));
}

/**
 * @param topInset rendered height of the HTML panel at the top (opponents,
 *                 chips); the piles and the table start below it.
 */
export function computeMetrics(width: number, height: number, topInset = 0): Metrics {
  const cardW = clamp(Math.floor(Math.min(width / 6.2, height / 9.5)), 44, 112);
  const cardH = Math.round(cardW * 1.45);
  const topZone = clamp(Math.max(Math.round(height * 0.2), Math.round(topInset) + 6), 120, Math.round(height * 0.5));
  const handTop = height - cardH - 66; // leaves room for the bottom HUD strip
  return {
    width,
    height,
    cardW,
    cardH,
    topZone,
    handTop,
    tableTop: topZone + 8,
    tableBottom: handTop - 100, // the action band (status + buttons) sits in between
  };
}

/** Fanned hand along the bottom edge; cards overlap when there are many. */
export function handSlots(n: number, m: Metrics): Slot[] {
  if (n <= 0) return [];
  const maxSpacing = m.cardW * 0.74;
  const available = Math.max(0, m.width - 28 - m.cardW);
  const spacing = n > 1 ? Math.min(maxSpacing, available / (n - 1)) : 0;
  const total = spacing * (n - 1);
  const x0 = m.width / 2 - total / 2;
  const y = m.height - m.cardH / 2 - 58;
  const slots: Slot[] = [];
  for (let i = 0; i < n; i++) {
    const k = i - (n - 1) / 2;
    slots.push({ x: x0 + i * spacing, y: y + Math.abs(k) * 2.2, rotation: k * 0.03, scale: 1 });
  }
  return slots;
}

/** Attack/defence pairs on a centred grid; shrinks when the table is crowded. */
export function tableSlots(n: number, m: Metrics): { attack: Slot; defense: Slot }[] {
  if (n <= 0) return [];
  const cellW = m.cardW * 1.45;
  const cellH = m.cardH * 1.3;
  const cols = Math.max(1, Math.min(n, Math.floor((m.width - 12) / cellW)));
  const rows = Math.ceil(n / cols);
  const pileH = m.cardH * PILE_SCALE;
  const top = m.tableTop + (rows > 1 ? pileH * 0.55 : 0);
  const scale = clamp((m.tableBottom - top) / (rows * cellH), 0.45, 1);
  const sw = cellW * scale;
  const sh = cellH * scale;
  const gridW = cols * sw;
  const gridH = rows * sh;
  const x0 = m.width / 2 - gridW / 2 + sw / 2 - m.cardW * 0.12 * scale;
  const centerY = (top + m.tableBottom) / 2;
  const y0 = centerY - gridH / 2 + sh / 2 - m.cardH * 0.08 * scale;
  const lastRowCount = n - (rows - 1) * cols;
  const out: { attack: Slot; defense: Slot }[] = [];
  for (let i = 0; i < n; i++) {
    const c = i % cols;
    const r = Math.floor(i / cols);
    const centerOffset = r === rows - 1 ? ((cols - lastRowCount) * sw) / 2 : 0;
    const x = x0 + c * sw + centerOffset;
    const y = y0 + r * sh;
    const wobble = (((i * 7) % 5) - 2) * 0.02;
    out.push({
      attack: { x, y, rotation: wobble, scale },
      defense: { x: x + m.cardW * 0.3 * scale, y: y + m.cardH * 0.17 * scale, rotation: 0.2 + wobble, scale },
    });
  }
  return out;
}

/** Slot of the next attack card (where a dragged card snaps while pending). */
export function nextAttackSlot(n: number, m: Metrics): Slot {
  const slots = tableSlots(n + 1, m);
  return slots[n]?.attack ?? { x: m.width / 2, y: (m.tableTop + m.tableBottom) / 2, rotation: 0, scale: 1 };
}

export function deckSlot(m: Metrics): Slot {
  const s = PILE_SCALE;
  return { x: 16 + (m.cardW * s) / 2, y: m.tableTop + (m.cardH * s) / 2 + 4, rotation: -0.03, scale: s };
}

/** The trump lies sideways next to the deck. */
export function trumpSlot(m: Metrics): Slot {
  const d = deckSlot(m);
  const s = PILE_SCALE;
  return { x: d.x + (m.cardW * s) / 2 + (m.cardH * s) / 2 + 6, y: d.y + 6, rotation: Math.PI / 2, scale: s };
}

export function discardSlot(m: Metrics): Slot {
  const s = PILE_SCALE;
  return { x: m.width - 16 - (m.cardW * s) / 2, y: m.tableTop + (m.cardH * s) / 2 + 4, rotation: 0.35, scale: s };
}

/** The viewer's own stump: a small face-down stack at the right end of the hand. */
export function stumpSlot(m: Metrics): Slot {
  const s = 0.55;
  return { x: m.width - 14 - (m.cardW * s) / 2, y: m.height - (m.cardH * s) / 2 - 64, rotation: 0.12, scale: s };
}

/** Where cards fly when an opponent takes them / where their cards come from. */
export function exitSlot(m: Metrics): Slot {
  return { x: m.width / 2, y: -m.cardH * 0.6, rotation: 0, scale: 0.6 };
}

/** Axis-aligned hit test against a slot (rotation ignored on purpose). */
export function hitSlot(slot: Slot, m: Metrics, x: number, y: number, grow = 1.15): boolean {
  const hw = (m.cardW * slot.scale * grow) / 2;
  const hh = (m.cardH * slot.scale * grow) / 2;
  return Math.abs(x - slot.x) <= hw && Math.abs(y - slot.y) <= hh;
}
