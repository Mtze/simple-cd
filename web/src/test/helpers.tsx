import { render } from '@testing-library/react';
import type { ReactNode } from 'react';
import { vi } from 'vitest';
import type { Me, Target } from '../api/types';
import { MeContext } from '../lib/me';

export function json(status: number, body?: unknown): Response {
  return new Response(body === undefined ? null : JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

type Handler = (init: RequestInit | undefined, url: string) => Response;

// Routes keyed by "METHOD /path?query". Unknown requests fail the test loudly.
export function mockFetch(routes: Record<string, Handler>) {
  const fn = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    const key = `${init?.method ?? 'GET'} ${url}`;
    const h = routes[key];
    if (!h) throw new Error(`unexpected request ${key}`);
    return h(init, url);
  });
  vi.stubGlobal('fetch', fn);
  return fn;
}

export const ME: Me = {
  user: { username: 'alice', name: 'Alice Example', email: 'alice@example.org', roles: ['project:org/app:developer'] },
  isAdmin: false,
};

export function makeTarget(over: Partial<Target> = {}): Target {
  return {
    id: 'app-test',
    project: 'org/app',
    type: 'helm',
    preview: false,
    lockable: true,
    lockDurationSeconds: 3600,
    status: 'available',
    active: [],
    permissions: { deploy: false, override: false, maintenance: false },
    ...over,
  };
}

export function renderWithMe(ui: ReactNode, me: Me = ME) {
  return render(<MeContext.Provider value={me}>{ui}</MeContext.Provider>);
}
