// Per-device preferences (localStorage); URL parameters win for quick testing.
const FX_KEY = 'durag.fx';

export function getFxSetting(): boolean {
  const params = new URLSearchParams(location.search);
  if (params.get('fx') === '0') return false;
  if (params.get('fx') === '1') return true;
  try {
    const stored = localStorage.getItem(FX_KEY);
    if (stored === '0') return false;
    if (stored === '1') return true;
  } catch {
    // storage unavailable
  }
  // very weak devices skip the full-screen shader passes by default
  return (navigator.hardwareConcurrency ?? 4) >= 3;
}

export function setFxSetting(on: boolean): void {
  try {
    localStorage.setItem(FX_KEY, on ? '1' : '0');
  } catch {
    // ignore
  }
}
