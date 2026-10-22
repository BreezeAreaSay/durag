// The player's eye. The table is seen from a head, not from a tripod:
//
//   sway    the head never holds still: two slow sines on each axis with
//           incommensurable periods (12–20 s), a few pixels, and a hair
//           of roll;
//   peer    now and then the eye leans in to look closer at something on
//           the table: scale 1 → 1.035 about that point over ~0.9 s, a
//           short hold, back.
//
// No blinking: it read as a glitch, not as an eye.
//
// Everything is a function of the clock, so a frame can be computed for
// any time and nothing accumulates error.

export interface Point {
  x: number;
  y: number;
}

export interface CameraFrame {
  /** offset of the world, px */
  x: number;
  y: number;
  /** roll, radians */
  rotation: number;
  /** zoom about `pivot` */
  scale: number;
  pivot: Point;
}

const PEER_IN_MS = 900;
const PEER_HOLD_MS = 650;
const PEER_OUT_MS = 900;

function easeInOut(t: number): number {
  return t < 0.5 ? 2 * t * t : 1 - Math.pow(-2 * t + 2, 2) / 2;
}

export class Camera {
  private t = 0;
  private readonly phase: number;
  /** sway amplitude in px (1 = a few pixels on a phone) */
  amplitude = 1;

  private peerAt = -Infinity;
  private peerPivot: Point = { x: 0, y: 0 };
  private peerScale = 1.035;
  private nextPeer: number;

  constructor(seed = Math.random()) {
    this.phase = seed * Math.PI * 2;
    this.nextPeer = 20000 + Camera.exp(40000);
  }

  /** an exponential wait with the given mean */
  private static exp(mean: number): number {
    return -Math.log(1 - Math.random()) * mean;
  }

  /**
   * Advance the clock. `lookAt` is asked when the eye wants to peer:
   * it returns a point on the table worth a look, or null to skip.
   */
  update(dtMs: number, lookAt: () => Point | null, busy = false): void {
    this.t += dtMs;
    if (this.t >= this.nextPeer) {
      const p = busy ? null : lookAt();
      if (p) {
        this.peerAt = this.t;
        this.peerPivot = p;
        this.peerScale = 1.025 + Math.random() * 0.02;
      }
      this.nextPeer = this.t + 25000 + Camera.exp(40000);
    }
  }

  /** stop peering now (a drag started): the zoom eases out from where it is */
  release(): void {
    const l = this.t - this.peerAt;
    if (l < 0 || l > PEER_IN_MS + PEER_HOLD_MS + PEER_OUT_MS) return;
    if (l < PEER_IN_MS + PEER_HOLD_MS) {
      // jump to the matching point of the way out, so the scale is continuous
      const k = l < PEER_IN_MS ? easeInOut(l / PEER_IN_MS) : 1;
      this.peerAt = this.t - (PEER_IN_MS + PEER_HOLD_MS + PEER_OUT_MS * (1 - k));
    }
  }

  /** how far in the eye leans, 0..1 */
  private peerWeight(): number {
    const l = this.t - this.peerAt;
    if (l < 0) return 0;
    if (l < PEER_IN_MS) return easeInOut(l / PEER_IN_MS);
    if (l < PEER_IN_MS + PEER_HOLD_MS) return 1;
    if (l < PEER_IN_MS + PEER_HOLD_MS + PEER_OUT_MS) return 1 - easeInOut((l - PEER_IN_MS - PEER_HOLD_MS) / PEER_OUT_MS);
    return 0;
  }

  frame(): CameraFrame {
    const s = this.t / 1000;
    const a = this.amplitude;
    // slow: the longest period is 20 s, the shortest 9 s
    const x = a * (2.2 * Math.sin((2 * Math.PI * s) / 17.3 + this.phase) + 1.3 * Math.sin((2 * Math.PI * s) / 9.7));
    const y = a * (2.6 * Math.sin((2 * Math.PI * s) / 13.1 + this.phase * 0.7) + 1.1 * Math.sin((2 * Math.PI * s) / 8.9));
    const rotation = 0.0035 * Math.sin((2 * Math.PI * s) / 20.3 + this.phase);
    const w = this.peerWeight();
    return { x, y, rotation, scale: 1 + (this.peerScale - 1) * w, pivot: this.peerPivot };
  }

  /** force a look now (tests) */
  peer(p: Point): void {
    this.peerAt = this.t;
    this.peerPivot = p;
  }
}
