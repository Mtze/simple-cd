import { act, renderHook, waitFor } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import type { ChangeEvent } from '../api/types';
import { useApi } from './useApi';

class FakeEventSource {
  static last: FakeEventSource | null = null;
  url: string;
  closed = false;
  private handlers: Record<string, (m: { data: string }) => void> = {};

  constructor(url: string) {
    this.url = url;
    FakeEventSource.last = this;
  }
  addEventListener(type: string, fn: (m: { data: string }) => void) {
    this.handlers[type] = fn;
  }
  close() {
    this.closed = true;
  }
  emit(e: ChangeEvent) {
    this.handlers.change?.({ data: JSON.stringify(e) });
  }
}

afterEach(() => vi.unstubAllGlobals());

it('reloads on a matching change event and closes the stream with the last user', async () => {
  vi.stubGlobal('EventSource', FakeEventSource);
  let calls = 0;
  const { result, unmount } = renderHook(() =>
    useApi('artifacts:org/app', async () => ++calls, undefined, (e) => e.project === 'org/app'),
  );
  await waitFor(() => expect(result.current.data).toBe(1));
  const es = FakeEventSource.last!;
  expect(es.url).toBe('/api/events');

  act(() => es.emit({ kind: 'artifacts', project: 'org/other' }));
  expect(calls).toBe(1);

  act(() => es.emit({ kind: 'artifacts', project: 'org/app' }));
  await waitFor(() => expect(result.current.data).toBe(2));

  unmount();
  expect(es.closed).toBe(true);
});
