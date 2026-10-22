// Synthesised sound effects: no audio files, just the Web Audio API. Mobile
// browsers only let audio start after a user gesture, so unlockAudio() is
// hooked to the first tap and every effect checks the context is running.
import type { Effect } from './emoji';

let ctx: AudioContext | null = null;

type AudioWindow = Window & { webkitAudioContext?: typeof AudioContext };

/** Creates/resumes the audio context; call from a user gesture. */
export function unlockAudio(): void {
  try {
    const Ctor = window.AudioContext ?? (window as AudioWindow).webkitAudioContext;
    if (!Ctor) return;
    if (!ctx) ctx = new Ctor();
    if (ctx.state === 'suspended') void ctx.resume();
  } catch {
    ctx = null;
  }
}

function tone(ac: AudioContext, type: OscillatorType, from: number, to: number, start: number, dur: number, gain = 0.18): void {
  const osc = ac.createOscillator();
  const g = ac.createGain();
  osc.type = type;
  osc.frequency.setValueAtTime(from, start);
  osc.frequency.exponentialRampToValueAtTime(Math.max(20, to), start + dur);
  g.gain.setValueAtTime(0.0001, start);
  g.gain.exponentialRampToValueAtTime(gain, start + 0.02);
  g.gain.exponentialRampToValueAtTime(0.0001, start + dur);
  osc.connect(g).connect(ac.destination);
  osc.start(start);
  osc.stop(start + dur + 0.05);
}

function noise(ac: AudioContext, start: number, dur: number, gain = 0.3, cutoffFrom = 4000, cutoffTo = 200): void {
  const frames = Math.floor(ac.sampleRate * dur);
  const buffer = ac.createBuffer(1, frames, ac.sampleRate);
  const data = buffer.getChannelData(0);
  for (let i = 0; i < frames; i++) data[i] = Math.random() * 2 - 1;
  const src = ac.createBufferSource();
  src.buffer = buffer;
  const filter = ac.createBiquadFilter();
  filter.type = 'lowpass';
  filter.frequency.setValueAtTime(cutoffFrom, start);
  filter.frequency.exponentialRampToValueAtTime(cutoffTo, start + dur);
  const g = ac.createGain();
  g.gain.setValueAtTime(gain, start);
  g.gain.exponentialRampToValueAtTime(0.0001, start + dur);
  src.connect(filter).connect(g).connect(ac.destination);
  src.start(start);
  src.stop(start + dur);
}

/** Plays the sound of a premium reaction (silently does nothing when audio is not unlocked). */
export function playEffect(effect: Effect): void {
  if (!ctx || ctx.state !== 'running') return;
  const ac = ctx;
  const t0 = ac.currentTime + 0.01;
  switch (effect) {
    case 'cry': // two sad wah-wahs
      tone(ac, 'sine', 520, 260, t0, 0.55, 0.16);
      tone(ac, 'sine', 440, 200, t0 + 0.6, 0.7, 0.16);
      break;
    case 'finger': // a rude buzzer
      tone(ac, 'square', 140, 95, t0, 0.42, 0.12);
      noise(ac, t0 + 0.05, 0.25, 0.08, 1200, 300);
      break;
    case 'fanfare': // arpeggio and a chord
      [523, 659, 784, 1047].forEach((f, i) => tone(ac, 'triangle', f, f, t0 + i * 0.13, 0.16, 0.16));
      [523, 659, 784].forEach((f) => tone(ac, 'triangle', f, f * 1.003, t0 + 0.55, 0.7, 0.12));
      noise(ac, t0 + 0.55, 0.35, 0.06, 6000, 1000);
      break;
    case 'laugh': // staccato ha-ha-ha going down
      [0, 1, 2, 3, 4].forEach((i) => tone(ac, 'sawtooth', 330 - i * 18, 280 - i * 18, t0 + i * 0.16, 0.09, 0.08));
      break;
    case 'boom': // thump and a dull burst
      tone(ac, 'sine', 90, 30, t0, 0.6, 0.3);
      noise(ac, t0, 0.55, 0.35, 2500, 80);
      break;
  }
}
