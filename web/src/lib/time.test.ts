import { describe, expect, it } from 'vitest';
import { formatDuration, formatRelative, formatRemaining } from './time';

const now = Date.parse('2026-11-20T12:00:00Z');
const at = (ms: number) => new Date(now + ms).toISOString();
const MIN = 60_000;
const HOUR = 60 * MIN;

describe('formatRelative', () => {
  it('formats past and future', () => {
    expect(formatRelative(at(42 * MIN), now)).toBe('in 42 min.');
    expect(formatRelative(at(-3 * HOUR), now)).toBe('3 hr. ago');
    expect(formatRelative(at(-10_000), now)).toBe('now');
    expect(formatRelative(at(-24 * HOUR), now)).toBe('yesterday');
    expect(formatRelative(at(-5 * 24 * HOUR), now)).toBe('5 days ago');
  });
});

describe('formatDuration', () => {
  it('uses the two largest units', () => {
    expect(formatDuration(45_000)).toBe('45 s');
    expect(formatDuration(5 * MIN + 3000)).toBe('5 min 3 s');
    expect(formatDuration(2 * HOUR + 5 * MIN)).toBe('2 h 5 min');
    expect(formatDuration(2 * HOUR)).toBe('2 h');
    expect(formatDuration(-5)).toBe('0 s');
  });
});

describe('formatRemaining', () => {
  it('rounds up to whole minutes', () => {
    expect(formatRemaining(at(42 * MIN), now)).toBe('42 min left');
    expect(formatRemaining(at(41 * MIN + 1000), now)).toBe('42 min left');
    expect(formatRemaining(at(HOUR + 5 * MIN), now)).toBe('1 h 5 min left');
    expect(formatRemaining(at(10_000), now)).toBe('1 min left');
  });

  it('reports expiry', () => {
    expect(formatRemaining(at(0), now)).toBe('expired');
    expect(formatRemaining(at(-MIN), now)).toBe('expired');
  });
});
