import { useCallback, useEffect, useState } from 'react';
import { Button } from './Button';
import { useGame } from '../state/GameContext';
import { useBackButton } from '../telegram/useTelegram';
import { t } from '../i18n';

interface Overview {
  server: { started: string; uptime_seconds: number; go: string; local_rooms: number; local_clients: number };
  counters: Record<string, number>;
  llm: {
    configured: string[];
    discovered: string[];
    paused: boolean;
    pause_reason?: string;
    calls: number;
    ok: number;
    failed: number;
    last_error?: string;
    cooling?: string[];
    last_model?: string;
    last_ok?: string;
  } | null;
  bots: Record<string, boolean>;
  rooms: {
    id: string;
    status: string;
    bout: number;
    humans: number;
    updated_at: number;
    version: number;
    players: { id: string; name: string; bot: boolean; connected: boolean; hand: number; stump: number; xp: number; out: boolean }[];
  }[];
  users: { id: string; name: string; games: number; wins: number; losses: number; xp: number; last_seen: number }[];
  events: { at: string; kind: string; message: string; fields?: Record<string, string> }[];
  logs: { at: string; level: string; message: string; attrs?: string }[];
}

type Tab = 'overview' | 'rooms' | 'users' | 'events' | 'logs';

function apiUrl(path: string): string {
  const configured = import.meta.env.VITE_WS_URL as string | undefined;
  if (!configured) return path;
  try {
    const u = new URL(configured);
    u.protocol = u.protocol === 'wss:' ? 'https:' : 'http:';
    u.pathname = path.split('?')[0] ?? path;
    u.search = path.includes('?') ? path.slice(path.indexOf('?')) : '';
    return u.toString();
  } catch {
    return path;
  }
}

const fmtTime = (iso: string | number) => {
  const d = typeof iso === 'number' ? new Date(iso) : new Date(iso);
  return Number.isNaN(d.getTime()) ? '' : d.toLocaleTimeString();
};
const fmtUptime = (s: number) => `${Math.floor(s / 3600)}h ${Math.floor((s % 3600) / 60)}m`;

/** Monitoring for the Telegram ids listed in ADMIN_IDS: live tables, players, errors, logs. */
export function AdminScreen() {
  const { adminQuery, setAdminOpen } = useGame();
  const close = useCallback(() => setAdminOpen(false), [setAdminOpen]);
  useBackButton(close);
  const [tab, setTab] = useState<Tab>('overview');
  const [data, setData] = useState<Overview | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    const load = async () => {
      try {
        const r = await fetch(apiUrl(`/api/admin/overview?${adminQuery()}`));
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        const json = (await r.json()) as Overview;
        if (!cancelled) {
          setData(json);
          setError(null);
        }
      } catch (e) {
        if (!cancelled) setError(String(e));
      }
    };
    void load();
    const timer = window.setInterval(() => void load(), 5000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [adminQuery]);

  const tabs: Tab[] = ['overview', 'rooms', 'users', 'events', 'logs'];
  return (
    <main className="screen admin">
      <header className="admin__head">
        <span className="tape tape--small">{t('admin.title')}</span>
        <Button variant="paper" onClick={() => setAdminOpen(false)}>
          {t('admin.close')}
        </Button>
      </header>
      <nav className="admin__tabs">
        {tabs.map((k) => (
          <button key={k} type="button" className={`chip ${tab === k ? 'chip--acid' : ''}`} onClick={() => setTab(k)}>
            {t(`admin.tab.${k}` as 'admin.tab.overview')}
          </button>
        ))}
      </nav>
      {error ? <p className="status status--warn">{error}</p> : null}
      {!data ? <p className="muted">{t('admin.loading')}</p> : null}
      {data && tab === 'overview' ? (
        <section className="admin__section">
          <table className="admin__table">
            <tbody>
              <tr>
                <th>{t('admin.uptime')}</th>
                <td>{fmtUptime(data.server.uptime_seconds)}</td>
              </tr>
              <tr>
                <th>{t('admin.localRooms')}</th>
                <td>
                  {data.server.local_rooms} / {data.server.local_clients} {t('admin.connections')}
                </td>
              </tr>
              <tr>
                <th>Go</th>
                <td>{data.server.go}</td>
              </tr>
              <tr>
                <th>{t('admin.bots')}</th>
                <td>
                  {Object.entries(data.bots)
                    .map(([k, v]) => `${k}=${v ? 'on' : 'off'}`)
                    .join(' · ')}
                </td>
              </tr>
            </tbody>
          </table>
          <h3 className="admin__h">LLM</h3>
          {data.llm ? (
            <table className="admin__table">
              <tbody>
                <tr>
                  <th>{t('admin.models')}</th>
                  <td>{[...data.llm.configured, ...data.llm.discovered].join(', ') || '—'}</td>
                </tr>
                <tr>
                  <th>{t('admin.calls')}</th>
                  <td>
                    {data.llm.calls} · ok {data.llm.ok} · fail {data.llm.failed}
                  </td>
                </tr>
                <tr>
                  <th>{t('admin.lastModel')}</th>
                  <td>
                    {data.llm.last_model || '—'} {data.llm.last_ok ? `(${fmtTime(data.llm.last_ok)})` : ''}
                  </td>
                </tr>
                <tr>
                  <th>{t('admin.paused')}</th>
                  <td>{data.llm.paused ? data.llm.pause_reason || 'yes' : 'no'}</td>
                </tr>
                <tr>
                  <th>{t('admin.cooling')}</th>
                  <td className="admin__wrap">{data.llm.cooling?.length ? data.llm.cooling.join(', ') : '—'}</td>
                </tr>
                <tr>
                  <th>{t('admin.lastError')}</th>
                  <td className="admin__wrap">{data.llm.last_error || '—'}</td>
                </tr>
              </tbody>
            </table>
          ) : (
            <p className="muted">{t('admin.noLlm')}</p>
          )}
          <h3 className="admin__h">{t('admin.counters')}</h3>
          <table className="admin__table">
            <tbody>
              {Object.entries(data.counters)
                .sort(([a], [b]) => a.localeCompare(b))
                .map(([k, v]) => (
                  <tr key={k}>
                    <th>{k}</th>
                    <td>{v}</td>
                  </tr>
                ))}
            </tbody>
          </table>
        </section>
      ) : null}
      {data && tab === 'rooms' ? (
        <section className="admin__section">
          {data.rooms.length === 0 ? <p className="muted">{t('admin.noRooms')}</p> : null}
          {data.rooms.map((r) => (
            <div key={r.id} className="admin__card">
              <div className="admin__row">
                <b>{r.id}</b> · {r.status} · {t('game.bout')} {r.bout} · v{r.version} · {fmtTime(r.updated_at)}
              </div>
              <ul className="admin__list">
                {r.players.map((p) => (
                  <li key={p.id}>
                    {p.bot ? '🤖 ' : ''}
                    {p.name} <small>({p.id})</small> · {p.hand}+{p.stump} · XP {p.xp}
                    {!p.connected ? ` · ${t('role.offline')}` : ''}
                    {p.out ? ` · ${t('role.out')}` : ''}
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </section>
      ) : null}
      {data && tab === 'users' ? (
        <section className="admin__section">
          <table className="admin__table admin__table--grid">
            <thead>
              <tr>
                <th>{t('admin.user')}</th>
                <th>XP</th>
                <th>{t('admin.games')}</th>
                <th>{t('admin.wins')}</th>
                <th>{t('admin.lastSeen')}</th>
              </tr>
            </thead>
            <tbody>
              {data.users.map((u) => (
                <tr key={u.id}>
                  <td>
                    {u.name || '?'} <small>{u.id}</small>
                  </td>
                  <td>{u.xp}</td>
                  <td>{u.games}</td>
                  <td>{u.wins}</td>
                  <td>{fmtTime(u.last_seen)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </section>
      ) : null}
      {data && tab === 'events' ? (
        <section className="admin__section">
          <ul className="admin__log">
            {[...data.events].reverse().map((e, i) => (
              <li key={i}>
                <span className="admin__time">{fmtTime(e.at)}</span> <b>{e.kind}</b> {e.message}{' '}
                <small>
                  {Object.entries(e.fields ?? {})
                    .map(([k, v]) => `${k}=${v}`)
                    .join(' ')}
                </small>
              </li>
            ))}
          </ul>
        </section>
      ) : null}
      {data && tab === 'logs' ? (
        <section className="admin__section">
          <ul className="admin__log">
            {[...data.logs].reverse().map((l, i) => (
              <li key={i} className={l.level === 'ERROR' || l.level === 'WARN' ? 'admin__log--hot' : ''}>
                <span className="admin__time">{fmtTime(l.at)}</span> <b>{l.level}</b> {l.message} <small>{l.attrs}</small>
              </li>
            ))}
          </ul>
        </section>
      ) : null}
    </main>
  );
}
