import { fireEvent, screen } from '@testing-library/react';
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

const openMenu = () => fireEvent.click(screen.getByRole('button', { name: 'Deploy abcdef1' }));

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
    expect(screen.queryByRole('button', { name: 'Deploy to app-test' })).toBeNull();
    openMenu();
    expect(screen.getByRole('button', { name: 'Deploy to app-test' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Deploy to app-prod' })).toBeNull();
    expect(screen.getByText('abcdef1')).toBeTruthy();
  });

  it('shows reasons and disables deploy for not deployable targets', () => {
    renderWithMe(
      <ul><ArtifactRow artifact={artifact({ readiness: { 'app-test': ['chart 1.2.3 missing'], 'app-prod': [] } })} targets={targets} onDeploy={() => {}} /></ul>,
    );
    openMenu();
    expect((screen.getByRole('button', { name: 'Deploy to app-test' }) as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getAllByText('chart 1.2.3 missing').length).toBe(2); // in the chip and in the menu
  });

  it('starts a deploy from the menu and closes it', () => {
    const onDeploy = vi.fn();
    renderWithMe(<ul><ArtifactRow artifact={artifact({ readiness: { 'app-test': [], 'app-prod': [] } })} targets={targets} onDeploy={onDeploy} /></ul>);
    openMenu();
    fireEvent.click(screen.getByRole('button', { name: 'Deploy to app-test' }));
    expect(onDeploy).toHaveBeenCalledWith(targets[0]);
    expect(screen.queryByRole('button', { name: 'Deploy to app-test' })).toBeNull();
  });

  it('shows no deploy menu without deploy permission', () => {
    renderWithMe(<ul><ArtifactRow artifact={artifact({ readiness: { 'app-prod': [] } })} targets={[targets[1]]} onDeploy={() => {}} /></ul>);
    expect(screen.queryByRole('button', { name: 'Deploy abcdef1' })).toBeNull();
    expect(screen.getByText('app-prod')).toBeTruthy();
  });

  it('marks the targets the artifact is deployed to', () => {
    renderWithMe(<ul><ArtifactRow artifact={artifact({ readiness: { 'app-test': [], 'app-prod': [] }, deployedTo: ['app-test'] })} targets={targets} onDeploy={() => {}} /></ul>);
    expect(screen.getByText(', deployed')).toBeTruthy();
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

    fireEvent.click(await screen.findByRole('button', { name: 'Deploy abcdef1' }));
    expect(screen.getByRole('button', { name: 'Deploy to app-test' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Deploy to app-previews' })).toBeNull();
    expect(screen.getByRole('heading', { name: 'Previews' })).toBeTruthy();
  });
});
