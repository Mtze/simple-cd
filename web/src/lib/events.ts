import type { ChangeEvent } from '../api/types';

type Listener = (e: ChangeEvent) => void;

const listeners = new Set<Listener>();
let source: EventSource | null = null;

// One stream per tab: the first listener opens it, the last one closes it.
// EventSource reconnects by itself after network errors; polling covers the gap.
export function onChange(fn: Listener): () => void {
  listeners.add(fn);
  if (!source && typeof EventSource !== 'undefined') {
    source = new EventSource('/api/events');
    source.addEventListener('change', (m) => {
      let e: ChangeEvent;
      try {
        e = JSON.parse((m as MessageEvent<string>).data) as ChangeEvent;
      } catch {
        return;
      }
      listeners.forEach((l) => l(e));
    });
  }
  return () => {
    listeners.delete(fn);
    if (listeners.size === 0 && source) {
      source.close();
      source = null;
    }
  };
}
