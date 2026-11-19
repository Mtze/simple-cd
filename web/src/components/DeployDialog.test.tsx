import { fireEvent, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { ArtifactBrief, Plan } from '../api/types';
import { json, makeTarget, mockFetch, renderWithMe } from '../test/helpers';
import { DeployDialog } from './DeployDialog';

const artifact: ArtifactBrief = { id: 12, repo: 'org/app', sha: 'abc1234def5678', source: 'pr', refName: 'feature/x', pr: 42 };
const target = makeTarget({ permissions: { deploy: true, override: false, maintenance: false } });

function plan(over: Partial<Plan> = {}): Plan {
  return {
    targetId: 'app-test',
    artifactId: 12,
    slot: '',
    status: 'available',
    reasons: [],
    diff: '',
    needsOverride: false,
    permissions: { deploy: true, override: false, maintenance: false },
    ...over,
  };
}

const PLAN = 'GET /api/targets/app-test/plan?artifact=12';
const DEPLOY = 'POST /api/targets/app-test/deployments';

function open() {
  renderWithMe(<DeployDialog target={target} artifact={artifact} onClose={() => {}} />);
}

const deployButton = () => screen.findByRole('button', { name: 'Deploy' }) as Promise<HTMLButtonElement>;

afterEach(() => {
  vi.unstubAllGlobals();
  window.history.replaceState(null, '', '/');
});

describe('DeployDialog', () => {
  it('lists reasons and disables deploy when not deployable', async () => {
    mockFetch({ [PLAN]: () => json(200, plan({ reasons: ['image ghcr.io/org/app missing'] })) });
    open();
    expect((await deployButton()).disabled).toBe(true);
    expect(screen.getByText('image ghcr.io/org/app missing')).toBeTruthy();
  });

  it('requires the review checkbox when the config changes', async () => {
    const fetch = mockFetch({
      [PLAN]: () => json(200, plan({ diff: '--- deployed\n+++ abc1234\n@@ -1 +1 @@\n-replicas: 1\n+replicas: 2' })),
      [DEPLOY]: () => json(201, { id: 5 }),
    });
    open();
    const deploy = await deployButton();
    expect(deploy.disabled).toBe(true);
    expect(screen.getByText('+replicas: 2').className).toBe('diff-add');
    expect(screen.getByText('-replicas: 1').className).toBe('diff-del');

    fireEvent.click(screen.getByLabelText('I reviewed the configuration change'));
    expect(deploy.disabled).toBe(false);
    fireEvent.click(deploy);

    await waitFor(() => expect(window.location.pathname).toBe('/deployments/5'));
    const body = JSON.parse(String(fetch.mock.calls[1][1]!.body));
    expect(body).toEqual({ artifactId: 12, confirm: true, override: false });
  });

  it('explains the lock and disables deploy without override permission', async () => {
    mockFetch({
      [PLAN]: () => json(200, plan({ status: 'locked', needsOverride: true, lockHolder: 'bob', lockUntil: new Date(Date.now() + 3600_000).toISOString() })),
    });
    open();
    expect((await deployButton()).disabled).toBe(true);
    expect(screen.getByText(/is locked by bob until/)).toBeTruthy();
    expect(screen.queryByLabelText(/Take over lock/)).toBeNull();
  });

  it('requires the takeover checkbox with override permission', async () => {
    const fetch = mockFetch({
      [PLAN]: () =>
        json(200, plan({
          status: 'locked', needsOverride: true, lockHolder: 'bob', lockUntil: new Date(Date.now() + 3600_000).toISOString(),
          permissions: { deploy: true, override: true, maintenance: true },
        })),
      [DEPLOY]: () => json(201, { id: 9 }),
    });
    open();
    const deploy = await deployButton();
    expect(deploy.disabled).toBe(true);

    fireEvent.click(screen.getByLabelText(/Take over lock from bob/));
    expect(deploy.disabled).toBe(false);
    fireEvent.click(deploy);

    await waitFor(() => expect(window.location.pathname).toBe('/deployments/9'));
    expect(JSON.parse(String(fetch.mock.calls[1][1]!.body))).toEqual({ artifactId: 12, confirm: false, override: true });
  });

  it('shows the returned diff on 409 confirmation_required', async () => {
    mockFetch({
      [PLAN]: () => json(200, plan()),
      [DEPLOY]: () => json(409, { error: 'confirmation required', code: 'confirmation_required', diff: '@@ -1 +1 @@\n+debug: true' }),
    });
    open();
    const deploy = await deployButton();
    expect(deploy.disabled).toBe(false);
    fireEvent.click(deploy);

    expect(await screen.findByText('+debug: true')).toBeTruthy();
    expect(screen.getByRole('alert').textContent).toMatch(/configuration changed/i);
    expect(deploy.disabled).toBe(true);
    fireEvent.click(screen.getByLabelText('I reviewed the configuration change'));
    expect(deploy.disabled).toBe(false);
  });

  it('shows a clear message on 409 slot_busy', async () => {
    mockFetch({
      [PLAN]: () => json(200, plan()),
      [DEPLOY]: () => json(409, { error: 'busy', code: 'slot_busy' }),
    });
    open();
    fireEvent.click(await deployButton());
    expect((await screen.findByRole('alert')).textContent).toMatch(/Another deployment is running/);
  });
});
