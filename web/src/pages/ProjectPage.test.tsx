import { screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Artifact, Target } from '../api/types';
import { json, makeTarget, mockFetch, renderWithMe } from '../test/helpers';
import { ArtifactRow, ProjectPage } from './ProjectPage';

const yes = { deploy: true, override: false, maintenance: false };
const no = { deploy: false, override: false, maintenance: false };

function artifact(over: Partial<Artifact> = {}): Artifact {
  return {
    id: 1, repo: 'org/app', sha: 'abcdef1234567', source: 'branch', refName: 'main',
    prClosed: false, runUrl: 'https://github.com/org/app/actions/runs/1', workflow: 'build',
    triggeringActor: 'alice', otherWorkflows: false, images: [], autoDeploy: [],
    createdAt: new Date().toISOString(), readiness: {}, deployedTo: [],
    ...over,
  };
}

afterEach(() => {
  vi.unstubAllGlobals();
  window.history.replaceState(null, '', '/');
});

describe('ArtifactRow', () => {
  const targets: Target[] = [makeTarget({ id: 'app-test', permissions: yes }), makeTarget({ id: 'app-prod', permissions: no })];

  it('renders deploy only for permitted targets', () => {
    renderWithMe(
      <ul><ArtifactRow artifact={artifact({ readiness: { 'app-test': [], 'app-prod': [] } })} targets={targets} onDeploy={() => {}} /></ul>,
    );
    expect(screen.getByRole('button', { name: 'Deploy to app-test' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Deploy to app-prod' })).toBeNull();
    expect(screen.getByText('abcdef1')).toBeTruthy();
  });

  it('shows reasons and disables deploy for not deployable targets', () => {
    renderWithMe(
      <ul><ArtifactRow artifact={artifact({ readiness: { 'app-test': ['chart 1.2.3 missing'], 'app-prod': [] } })} targets={targets} onDeploy={() => {}} /></ul>,
    );
    expect((screen.getByRole('button', { name: 'Deploy to app-test' }) as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByText('chart 1.2.3 missing')).toBeTruthy();
  });
});

describe('ProjectPage', () => {
  it('offers preview targets only for pull request artifacts', async () => {
    window.history.replaceState(null, '', '/projects/org/app?source=branch');
    const targets = [makeTarget({ id: 'app-test', permissions: yes }), makeTarget({ id: 'app-previews', preview: true, lockable: false, permissions: yes })];
    mockFetch({
      'GET /api/projects/org/app': () => json(200, { name: 'org/app', role: 'developer', targets, previews: [] }),
      'GET /api/projects/org/app/artifacts?source=branch': () =>
        json(200, [artifact({ readiness: { 'app-test': [], 'app-previews': [] } })]),
    });
    renderWithMe(<ProjectPage owner="org" repo="app" />);

    expect(await screen.findByRole('button', { name: 'Deploy to app-test' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Deploy to app-previews' })).toBeNull();
    expect(screen.getByRole('heading', { name: 'Previews' })).toBeTruthy();
  });
});
