import { useEffect, useState } from 'react';

const rtf = new Intl.RelativeTimeFormat('en', { style: 'short', numeric: 'auto' });

const toMs = (t: string | number | Date) => (t instanceof Date ? t.getTime() : typeof t === 'number' ? t : Date.parse(t));

// "in 42 min.", "3 hr. ago", "yesterday", "now"
export function formatRelative(t: string | number | Date, now = Date.now()): string {
  const diff = toMs(t) - now;
  const abs = Math.abs(diff);
  const sec = 1000, min = 60 * sec, hour = 60 * min, day = 24 * hour;
  if (abs < 45 * sec) return rtf.format(0, 'second');
  if (abs < 45 * min) return rtf.format(Math.round(diff / min), 'minute');
  if (abs < 22 * hour) return rtf.format(Math.round(diff / hour), 'hour');
  if (abs < 26 * day) return rtf.format(Math.round(diff / day), 'day');
  if (abs < 320 * day) return rtf.format(Math.round(diff / (30 * day)), 'month');
  return rtf.format(Math.round(diff / (365 * day)), 'year');
}

// Durations: "45 s", "5 min 3 s", "2 h 5 min", "3 d 4 h"
export function formatDuration(ms: number): string {
  const s = Math.max(0, Math.round(ms / 1000));
  const d = Math.floor(s / 86400), h = Math.floor((s % 86400) / 3600), m = Math.floor((s % 3600) / 60), r = s % 60;
  if (d > 0) return h ? `${d} d ${h} h` : `${d} d`;
  if (h > 0) return m ? `${h} h ${m} min` : `${h} h`;
  if (m > 0) return r ? `${m} min ${r} s` : `${m} min`;
  return `${r} s`;
}

// Remaining time for locks and previews, rounded up to whole minutes: "42 min left".
export function formatRemaining(until: string | number | Date, now = Date.now()): string {
  const ms = toMs(until) - now;
  if (ms <= 0) return 'expired';
  const mins = Math.ceil(ms / 60000);
  return `${formatDuration(mins * 60000)} left`;
}

export function formatAbsolute(t: string | number | Date): string {
  return new Date(toMs(t)).toLocaleString();
}

export function useNow(intervalMs = 30000): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(id);
  }, [intervalMs]);
  return now;
}
