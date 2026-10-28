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
  private tv: { src: AudioBufferSourceNode; lfo: OscillatorNode; slow: OscillatorNode } | null = null;
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

  /** a breath across the table */
  whoosh(): void {
    const ctx = this.ready();
    if (!ctx) return;
    this.burst(ctx, 0.12, 700, 0.5, 0.35);
  }

  /** a thumb rubbing a card: a short squeak */
  squeak(): void {
    const ctx = this.ready();
    if (!ctx) return;
    const t = ctx.currentTime;
    const osc = ctx.createOscillator();
    osc.type = 'triangle';
    osc.frequency.setValueAtTime(1400, t);
    osc.frequency.exponentialRampToValueAtTime(2600, t + 0.08);
    osc.frequency.exponentialRampToValueAtTime(1500, t + 0.18);
    const g = ctx.createGain();
    g.gain.setValueAtTime(0.0001, t);
    g.gain.exponentialRampToValueAtTime(0.08, t + 0.02);
    g.gain.exponentialRampToValueAtTime(0.0001, t + 0.2);
    osc.connect(g).connect(this.master!);
    osc.start(t);
    osc.stop(t + 0.22);
  }

  /** liquid into a glass: a rising gurgle */
  pour(): void {
    const ctx = this.ready();
    if (!ctx) return;
    const t = ctx.currentTime;
    for (let i = 0; i < 6; i++) {
      const osc = ctx.createOscillator();
      osc.type = 'sine';
      const f = 300 + i * 90 + Math.random() * 60;
      osc.frequency.setValueAtTime(f, t + i * 0.09);
      osc.frequency.exponentialRampToValueAtTime(f * 1.6, t + i * 0.09 + 0.08);
      const g = ctx.createGain();
      g.gain.setValueAtTime(0.0001, t + i * 0.09);
      g.gain.exponentialRampToValueAtTime(0.05, t + i * 0.09 + 0.02);
      g.gain.exponentialRampToValueAtTime(0.0001, t + i * 0.09 + 0.09);
      osc.connect(g).connect(this.master!);
      osc.start(t + i * 0.09);
      osc.stop(t + i * 0.09 + 0.1);
    }
  }

  /** a glass knocked back: two gulps and a sigh */
  gulp(): void {
    const ctx = this.ready();
    if (!ctx) return;
    const t = ctx.currentTime;
    [0, 0.22].forEach((d) => {
      const osc = ctx.createOscillator();
      osc.type = 'sine';
      osc.frequency.setValueAtTime(180, t + d);
      osc.frequency.exponentialRampToValueAtTime(90, t + d + 0.12);
      const g = ctx.createGain();
      g.gain.setValueAtTime(0.0001, t + d);
      g.gain.exponentialRampToValueAtTime(0.25, t + d + 0.02);
      g.gain.exponentialRampToValueAtTime(0.0001, t + d + 0.14);
      osc.connect(g).connect(this.master!);
      osc.start(t + d);
      osc.stop(t + d + 0.16);
    });
    this.burst(ctx, 0.25, 500, 0.4, 0.12, 0.5);
  }

  /** a pile going up: crackle and a whoosh */
  fire(): void {
    const ctx = this.ready();
    if (!ctx) return;
    this.burst(ctx, 0.5, 400, 0.4, 0.25);
    for (let i = 0; i < 14; i++) this.burst(ctx, 0.015, 1500 + Math.random() * 3500, 3, 0.18, Math.random() * 1.1);
  }

  /** the toilet: a rush of water going down, then the cistern */
  /** a toilet flush: the handle clunks, the water roars down with a vortex wobble, gurgles out, the cistern hisses full */
  flush(): void {
    const ctx = this.ready();
    if (!ctx) return;
    const t = ctx.currentTime;
    const out = this.master!;
    // the handle and the flap
    this.burst(ctx, 0.04, 160, 1.2, 0.45);
    this.burst(ctx, 0.03, 90, 1.5, 0.3, 0.08);
    // the rush: broadband noise, dark at first, opening up as the water tears through, then closing
    const rush = ctx.createBufferSource();
    rush.buffer = this.noiseBuffer(ctx);
    rush.loop = true;
    const lp = ctx.createBiquadFilter();
    lp.type = 'lowpass';
    lp.Q.value = 0.9;
    lp.frequency.setValueAtTime(500, t);
    lp.frequency.exponentialRampToValueAtTime(3600, t + 0.6);
    lp.frequency.setValueAtTime(3600, t + 1.4);
    lp.frequency.exponentialRampToValueAtTime(700, t + 2.8);
    const hp = ctx.createBiquadFilter();
    hp.type = 'highpass';
    hp.frequency.value = 120;
    const rg = ctx.createGain();
    rg.gain.setValueAtTime(0.0001, t + 0.1);
    rg.gain.exponentialRampToValueAtTime(0.7, t + 0.55);
    rg.gain.setValueAtTime(0.7, t + 1.5);
    rg.gain.exponentialRampToValueAtTime(0.25, t + 2.4);
    rg.gain.exponentialRampToValueAtTime(0.0001, t + 3.1);
    rush.connect(hp).connect(lp).connect(rg).connect(out);
    rush.start(t + 0.1);
    rush.stop(t + 3.2);
    // the vortex: a mid band whose level wobbles, slower as the bowl empties
    const swirl = ctx.createBufferSource();
    swirl.buffer = this.noiseBuffer(ctx);
    swirl.loop = true;
    const bp = ctx.createBiquadFilter();
    bp.type = 'bandpass';
    bp.Q.value = 2.5;
    bp.frequency.setValueAtTime(1100, t);
    bp.frequency.exponentialRampToValueAtTime(380, t + 2.8);
    const sg = ctx.createGain();
    sg.gain.setValueAtTime(0.0001, t + 0.3);
    sg.gain.exponentialRampToValueAtTime(0.35, t + 0.9);
    sg.gain.exponentialRampToValueAtTime(0.0001, t + 2.9);
    const wob = ctx.createOscillator();
    wob.type = 'sine';
    wob.frequency.setValueAtTime(7, t);
    wob.frequency.exponentialRampToValueAtTime(2.5, t + 2.8);
    const wg = ctx.createGain();
    wg.gain.value = 0.2;
    wob.connect(wg).connect(sg.gain);
    swirl.connect(bp).connect(sg).connect(out);
    wob.start(t);
    wob.stop(t + 3);
    swirl.start(t + 0.3);
    swirl.stop(t + 3);
    // the gurgle: bubbles of falling pitch as the last of it goes down
    for (let i = 0; i < 9; i++) {
      const at = t + 1.7 + i * 0.16 + Math.random() * 0.08;
      const osc = ctx.createOscillator();
      osc.type = 'triangle';
      const f0 = 260 - i * 14 + Math.random() * 60;
      osc.frequency.setValueAtTime(f0, at);
      osc.frequency.exponentialRampToValueAtTime(f0 * 0.45, at + 0.14);
      const og = ctx.createGain();
      og.gain.setValueAtTime(0.0001, at);
      og.gain.exponentialRampToValueAtTime(0.16 - i * 0.012, at + 0.02);
      og.gain.exponentialRampToValueAtTime(0.0001, at + 0.15);
      osc.connect(og).connect(out);
      osc.start(at);
      osc.stop(at + 0.16);
    }
    // the cistern fills: a thin hiss that rises a little and fades
    const fill = ctx.createBufferSource();
    fill.buffer = this.noiseBuffer(ctx);
    fill.loop = true;
    const fb = ctx.createBiquadFilter();
    fb.type = 'bandpass';
    fb.Q.value = 4;
    fb.frequency.setValueAtTime(2200, t + 2.6);
    fb.frequency.exponentialRampToValueAtTime(3400, t + 5);
    const fg = ctx.createGain();
    fg.gain.setValueAtTime(0.0001, t + 2.6);
    fg.gain.exponentialRampToValueAtTime(0.09, t + 3.2);
    fg.gain.setValueAtTime(0.09, t + 4.2);
    fg.gain.exponentialRampToValueAtTime(0.0001, t + 5.2);
    fill.connect(fb).connect(fg).connect(out);
    fill.start(t + 2.6);
    fill.stop(t + 5.3);
  }

  /** glass on the floor */
  crash(): void {
    const ctx = this.ready();
    if (!ctx) return;
    this.burst(ctx, 0.05, 3200, 0.8, 0.5);
    for (let i = 0; i < 7; i++) this.burst(ctx, 0.03, 2000 + Math.random() * 3000, 2, 0.25, 0.04 + Math.random() * 0.35);
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
    const hour = new Date().getHours();
    if (hour >= 23 || hour < 6) this.startTV(ctx);
  }

  /**
   * Late at night a television mumbles behind the wall: band-passed noise
   * in the voice range, chopped by a fast tremolo (syllables) under a slow
   * one (sentences), too quiet to make out a word.
   */
  private startTV(ctx: Ctx): void {
    if (this.tv) return;
    const src = ctx.createBufferSource();
    src.buffer = this.noiseBuffer(ctx);
    src.loop = true;
    const bp = ctx.createBiquadFilter();
    bp.type = 'bandpass';
    bp.frequency.value = 520;
    bp.Q.value = 2.5;
    const syll = ctx.createGain();
    syll.gain.value = 0.5;
    const lfo = ctx.createOscillator();
    lfo.frequency.value = 4.3;
    const lfoDepth = ctx.createGain();
    lfoDepth.gain.value = 0.5;
    lfo.connect(lfoDepth).connect(syll.gain);
    const phrase = ctx.createGain();
    phrase.gain.value = 0.5;
    const slow = ctx.createOscillator();
    slow.frequency.value = 0.23;
    const slowDepth = ctx.createGain();
    slowDepth.gain.value = 0.5;
    slow.connect(slowDepth).connect(phrase.gain);
    const g = ctx.createGain();
    g.gain.value = 0.035;
    src.connect(bp).connect(syll).connect(phrase).connect(g).connect(this.master!);
    src.start();
    lfo.start();
    slow.start();
    this.tv = { src, lfo, slow };
  }

  private stopRoom(): void {
    if (this.tv) {
      try {
        this.tv.src.stop();
        this.tv.lfo.stop();
        this.tv.slow.stop();
      } catch {
        // already stopped
      }
      this.tv = null;
    }
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
