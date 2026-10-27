// Procedural sound: nothing is downloaded, everything is synthesised with the
// Web Audio API the moment it is needed.
//
//   room    brown noise (a leaky integral of white noise,
//           b[n] = (b[n-1] + 0.02·w[n]) / 1.02) through a band-pass whose
//           centre drifts with a slow LFO: the hum of a basement.
//   snap    a card hitting the table: a 40 ms burst of noise through a
//           band-pass with an exponential envelope e^(−t/τ).
//   thud    a fist on the table: a sine falling from 70 to 35 Hz in 150 ms
//           plus a noise burst.
//   bito    two quick snaps, a beat apart.
//   took    a low descending tone: somebody picked the whole table up.
//
// Browsers only start audio after a user gesture, so the context is created
// lazily on the first call that follows one. The switch lives in
// localStorage and is off by default.

const KEY = 'durag.sound';

type Ctx = AudioContext;

class Sound {
  private ctx: Ctx | null = null;
  private master: GainNode | null = null;
  private room: { src: AudioBufferSourceNode; lfo: OscillatorNode } | null = null;
  private noise: AudioBuffer | null = null;
  private on = false;

  constructor() {
    try {
      this.on = localStorage.getItem(KEY) === '1';
    } catch {
      this.on = false;
    }
  }

  get enabled(): boolean {
    return this.on;
  }

  /** Flips the switch; must be called from a user gesture to start audio. */
  toggle(): boolean {
    this.setEnabled(!this.on);
    return this.on;
  }

  setEnabled(v: boolean): void {
    this.on = v;
    try {
      localStorage.setItem(KEY, v ? '1' : '0');
    } catch {
      // ignore
    }
    if (v) this.startRoom();
    else this.stopRoom();
  }

  /** Call from any user gesture: resumes a suspended context (iOS). */
  unlock(): void {
    if (!this.on) return;
    const ctx = this.context();
    if (ctx && ctx.state === 'suspended') void ctx.resume();
    this.startRoom();
  }

  snap(): void {
    const ctx = this.ready();
    if (!ctx) return;
    this.burst(ctx, 0.04, 1800 + Math.random() * 900, 0.9, 0.35);
  }

  thud(): void {
    const ctx = this.ready();
    if (!ctx) return;
    const t = ctx.currentTime;
    const osc = ctx.createOscillator();
    osc.type = 'sine';
    osc.frequency.setValueAtTime(70, t);
    osc.frequency.exponentialRampToValueAtTime(35, t + 0.15);
    const g = ctx.createGain();
    g.gain.setValueAtTime(0.9, t);
    g.gain.exponentialRampToValueAtTime(0.001, t + 0.35);
    osc.connect(g).connect(this.master!);
    osc.start(t);
    osc.stop(t + 0.4);
    this.burst(ctx, 0.08, 300, 0.6, 0.5);
  }

  bito(): void {
    const ctx = this.ready();
    if (!ctx) return;
    this.burst(ctx, 0.03, 2400, 1.2, 0.3);
    this.burst(ctx, 0.03, 2000, 1.2, 0.3, 0.12);
  }

  took(): void {
    const ctx = this.ready();
    if (!ctx) return;
    const t = ctx.currentTime;
    const osc = ctx.createOscillator();
    osc.type = 'triangle';
    osc.frequency.setValueAtTime(220, t);
    osc.frequency.exponentialRampToValueAtTime(110, t + 0.4);
    const g = ctx.createGain();
    g.gain.setValueAtTime(0.25, t);
    g.gain.exponentialRampToValueAtTime(0.001, t + 0.5);
    osc.connect(g).connect(this.master!);
    osc.start(t);
    osc.stop(t + 0.55);
  }

  /** a fly in the room: a wobbling buzz that lasts a while */
  buzz(seconds = 6): void {
    const ctx = this.ready();
    if (!ctx) return;
    const t = ctx.currentTime;
    const osc = ctx.createOscillator();
    osc.type = 'sawtooth';
    osc.frequency.setValueAtTime(180, t);
    const wobble = ctx.createOscillator();
    wobble.type = 'sine';
    wobble.frequency.value = 7;
    const depth = ctx.createGain();
    depth.gain.value = 25;
    wobble.connect(depth).connect(osc.frequency);
    const filter = ctx.createBiquadFilter();
    filter.type = 'bandpass';
    filter.frequency.value = 900;
    filter.Q.value = 2;
    const g = ctx.createGain();
    g.gain.setValueAtTime(0.0001, t);
    g.gain.exponentialRampToValueAtTime(0.05, t + 0.3);
    g.gain.setValueAtTime(0.05, t + seconds - 0.4);
    g.gain.exponentialRampToValueAtTime(0.0001, t + seconds);
    osc.connect(filter).connect(g).connect(this.master!);
    wobble.start(t);
    osc.start(t);
    osc.stop(t + seconds + 0.05);
    wobble.stop(t + seconds + 0.05);
  }

  /** a slap: short and flat */
  slap(): void {
    const ctx = this.ready();
    if (!ctx) return;
    this.burst(ctx, 0.05, 900, 0.7, 0.6);
  }

  /** a thumb across the deck: a quick run of snaps */
  riffle(): void {
    const ctx = this.ready();
    if (!ctx) return;
    for (let i = 0; i < 9; i++) this.burst(ctx, 0.012, 2600 + Math.random() * 1200, 1.1, 0.16, i * 0.028);
  }

  // --- internals -----------------------------------------------------------

  private context(): Ctx | null {
    if (this.ctx) return this.ctx;
    const AC = window.AudioContext ?? (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
    if (!AC) return null;
    try {
      this.ctx = new AC();
    } catch {
      return null;
    }
    this.master = this.ctx.createGain();
    this.master.gain.value = 0.6;
    this.master.connect(this.ctx.destination);
    return this.ctx;
  }

  private ready(): Ctx | null {
    if (!this.on) return null;
    const ctx = this.context();
    if (!ctx || ctx.state !== 'running') return null;
    return ctx;
  }

  /** Two seconds of brown noise, looped. */
  private noiseBuffer(ctx: Ctx): AudioBuffer {
    if (this.noise) return this.noise;
    const n = ctx.sampleRate * 2;
    const buf = ctx.createBuffer(1, n, ctx.sampleRate);
    const d = buf.getChannelData(0);
    let b = 0;
    for (let i = 0; i < n; i++) {
      const w = Math.random() * 2 - 1;
      b = (b + 0.02 * w) / 1.02;
      d[i] = b * 3.5;
    }
    this.noise = buf;
    return buf;
  }

  /** A short noise burst through a band-pass with an exponential decay. */
  private burst(ctx: Ctx, tau: number, freq: number, q: number, gain: number, delay = 0): void {
    const t = ctx.currentTime + delay;
    const src = ctx.createBufferSource();
    src.buffer = this.noiseBuffer(ctx);
    src.loop = true;
    const bp = ctx.createBiquadFilter();
    bp.type = 'bandpass';
    bp.frequency.value = freq;
    bp.Q.value = q;
    const g = ctx.createGain();
    g.gain.setValueAtTime(gain, t);
    g.gain.exponentialRampToValueAtTime(0.001, t + tau * 5);
    src.connect(bp).connect(g).connect(this.master!);
    src.start(t);
    src.stop(t + tau * 5 + 0.02);
  }

  private startRoom(): void {
    const ctx = this.context();
    if (!ctx || this.room || !this.on) return;
    if (ctx.state === 'suspended') void ctx.resume();
    const src = ctx.createBufferSource();
    src.buffer = this.noiseBuffer(ctx);
    src.loop = true;
    const bp = ctx.createBiquadFilter();
    bp.type = 'bandpass';
    bp.frequency.value = 160;
    bp.Q.value = 0.7;
    // the LFO drifts the band centre ±60 Hz every ~9 s: the room breathes
    const lfo = ctx.createOscillator();
    lfo.frequency.value = 0.11;
    const depth = ctx.createGain();
    depth.gain.value = 60;
    lfo.connect(depth).connect(bp.frequency);
    const g = ctx.createGain();
    g.gain.setValueAtTime(0.0001, ctx.currentTime);
    g.gain.exponentialRampToValueAtTime(0.09, ctx.currentTime + 2);
    src.connect(bp).connect(g).connect(this.master!);
    src.start();
    lfo.start();
    this.room = { src, lfo };
  }

  private stopRoom(): void {
    if (!this.room) return;
    try {
      this.room.src.stop();
      this.room.lfo.stop();
    } catch {
      // already stopped
    }
    this.room = null;
  }
}

export const sound = new Sound();
