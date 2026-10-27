// Whether the talk feed (bot lines and quick phrases) is shown: a per-phone
// choice, remembered in localStorage.
const KEY = 'durag.talk';
export const TALK_EVENT = 'durag:talk';

export function talkOn(): boolean {
  try {
    return localStorage.getItem(KEY) !== 'off';
  } catch {
    return true;
  }
}

export function setTalkOn(on: boolean): void {
  try {
    localStorage.setItem(KEY, on ? 'on' : 'off');
  } catch {
    // private mode: the choice lives for the session
  }
  window.dispatchEvent(new CustomEvent(TALK_EVENT, { detail: on }));
}
