import { describe, expect, it } from 'vitest';
import { FrameJudge } from './fx';

describe('FrameJudge', () => {
  it('never judges during the warm-up, however slow', () => {
    const j = new FrameJudge(30, 4000, 3000);
    for (let i = 0; i < 60; i++) expect(j.feed(60)).toBe(false); // 3.6 s of 16 fps
  });

  it('judges a phone slow after sustained slow frames, once', () => {
    const j = new FrameJudge(30, 4000, 3000);
    let flips = 0;
    for (let i = 0; i < 400; i++) if (j.feed(45)) flips++;
    expect(flips).toBe(1);
  });

  it('forgives a hiccup and a hidden tab', () => {
    const j = new FrameJudge(30, 4000, 3000);
    for (let i = 0; i < 600; i++) expect(j.feed(16)).toBe(false);
    expect(j.feed(900)).toBe(false); // the tab was hidden
    for (let i = 0; i < 20; i++) expect(j.feed(80)).toBe(false); // a GC pause, under the hold time
    for (let i = 0; i < 600; i++) expect(j.feed(16)).toBe(false);
  });
});
