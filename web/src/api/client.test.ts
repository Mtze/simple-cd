import { afterEach, describe, expect, it, vi } from 'vitest';
import { json, mockFetch } from '../test/helpers';
import { ApiError, api, browser } from './client';

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  window.history.replaceState(null, '', '/');
});

describe('api client', () => {
  it('redirects to login with the current path on 401', async () => {
    window.history.replaceState(null, '', '/projects/org/app?source=branch');
    const go = vi.spyOn(browser, 'go').mockImplementation(() => {});
    mockFetch({ 'GET /api/projects': () => json(401, { error: 'unauthenticated' }) });

    let settled = false;
    void api.projects().finally(() => (settled = true));
    await vi.waitFor(() => expect(go).toHaveBeenCalled());

    expect(go).toHaveBeenCalledWith('/auth/login?return=%2Fprojects%2Forg%2Fapp%3Fsource%3Dbranch');
    await Promise.resolve();
    expect(settled).toBe(false);
  });

  it('sends X-Requested-With and JSON body on mutating requests', async () => {
    const fetch = mockFetch({
      'POST /api/targets/app-test/deployments': () => json(201, { id: 7 }),
      'GET /api/me': () => json(200, {}),
    });

    await api.deploy('app-test', { artifactId: 12, confirm: true, override: false });
    const init = fetch.mock.calls[0][1]!;
    const headers = init.headers as Record<string, string>;
    expect(headers['X-Requested-With']).toBe('simple-cd');
    expect(headers['Content-Type']).toBe('application/json');
    expect(init.credentials).toBe('same-origin');
    expect(JSON.parse(String(init.body))).toEqual({ artifactId: 12, confirm: true, override: false });

    await api.me();
    const getHeaders = fetch.mock.calls[1][1]!.headers as Record<string, string>;
    expect(getHeaders['X-Requested-With']).toBeUndefined();
  });

  it('sends X-Requested-With on DELETE without a body', async () => {
    const fetch = mockFetch({ 'DELETE /api/previews/3': () => new Response(null, { status: 202 }) });
    await api.teardownPreview(3);
    const headers = fetch.mock.calls[0][1]!.headers as Record<string, string>;
    expect(headers['X-Requested-With']).toBe('simple-cd');
    expect(headers['Content-Type']).toBeUndefined();
  });

  it('parses error bodies into ApiError with code and extra fields', async () => {
    mockFetch({
      'POST /api/targets/app-test/deployments': () =>
        json(409, { error: 'target is locked', code: 'locked', holder: 'bob', until: '2026-11-20T12:00:00Z' }),
    });

    const err = await api.deploy('app-test', { artifactId: 1, confirm: false, override: false }).catch((e) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(err.status).toBe(409);
    expect(err.code).toBe('locked');
    expect(err.message).toBe('target is locked');
    expect(err.body.holder).toBe('bob');
  });

  it('falls back to an http_<status> code for non-JSON errors', async () => {
    mockFetch({ 'GET /api/projects': () => new Response('boom', { status: 502, statusText: 'Bad Gateway' }) });
    const err = await api.projects().catch((e) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(err.code).toBe('http_502');
  });

  it('encodes project names per path segment', async () => {
    const fetch = mockFetch({ 'GET /api/projects/org/app/artifacts?source=pr&closed=true': () => json(200, []) });
    await api.artifacts('org/app', 'pr', true);
    expect(fetch).toHaveBeenCalledOnce();
  });
});
