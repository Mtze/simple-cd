import { useState } from 'react';
import { api } from '../api/client';
import type { Artifact, Source, Target } from '../api/types';
import { ArtifactRef, ErrorBox, Loading, RelTime } from '../components/common';
import { DeployDialog } from '../components/DeployDialog';
import { CheckIcon, CrossIcon, ExternalIcon, WarningIcon } from '../components/icons';
import { PreviewTable } from '../components/tables';
import { TargetCard } from '../components/TargetCard';
import { navigate, useLocation } from '../lib/router';
import { useApi } from '../lib/useApi';

const SOURCES: { id: Source; label: string }[] = [
  { id: 'pr', label: 'Pull requests' },
  { id: 'branch', label: 'Branches' },
  { id: 'release', label: 'Releases' },
];

export function ProjectPage({ owner, repo }: { owner: string; repo: string }) {
  const name = `${owner}/${repo}`;
  const project = useApi(`project:${name}`, () => api.project(name), 30000);

  if (!project.data) {
    return <div className="page">{project.error ? <ErrorBox error={project.error} /> : <Loading />}</div>;
  }
  const { targets, previews, role } = project.data;
  const hasPreviewTargets = targets.some((t) => t.preview);

  return (
    <div className="page">
      <header className="page-head">
        <h1>{name}</h1>
        {role && <span className="badge badge-neutral">{role}</span>}
      </header>

      <section className="section" aria-labelledby="targets-h">
        <h2 id="targets-h">Targets</h2>
        {targets.length === 0 ? (
          <p className="muted">No targets configured.</p>
        ) : (
          <div className="target-grid">
            {targets.map((t) => <TargetCard key={t.id} target={t} onChanged={project.reload} />)}
          </div>
        )}
      </section>

      <Artifacts name={name} targets={targets} />

      {hasPreviewTargets && (
        <section className="section" aria-labelledby="previews-h">
          <h2 id="previews-h">Previews</h2>
          <PreviewTable previews={previews} onChanged={project.reload} />
        </section>
      )}
    </div>
  );
}

function Artifacts({ name, targets }: { name: string; targets: Target[] }) {
  const { pathname, search } = useLocation();
  const source = (SOURCES.find((s) => s.id === search.get('source'))?.id ?? 'pr') as Source;
  const closed = search.get('closed') === 'true';
  const list = useApi(`artifacts:${name}:${source}:${closed}`, () => api.artifacts(name, source, closed), 30000);
  const [deploying, setDeploying] = useState<{ target: Target; artifact: Artifact } | null>(null);

  const setQuery = (key: string, value: string | null) => {
    const q = new URLSearchParams(search);
    if (value === null) q.delete(key);
    else q.set(key, value);
    const qs = q.toString();
    navigate(pathname + (qs ? `?${qs}` : ''), { replace: true });
  };

  // Preview targets only make sense for pull request artifacts.
  const shown = targets.filter((t) => source === 'pr' || !t.preview);

  return (
    <section className="section" aria-labelledby="artifacts-h">
      <div className="section-head">
        <h2 id="artifacts-h">Artifacts</h2>
        <div className="toolbar">
          <div className="tabs" role="tablist" aria-label="Artifact source">
            {SOURCES.map((s) => (
              <button
                key={s.id}
                type="button"
                role="tab"
                aria-selected={s.id === source}
                className="tab"
                onClick={() => setQuery('source', s.id === 'pr' ? null : s.id)}
              >
                {s.label}
              </button>
            ))}
          </div>
          {source === 'pr' && (
            <label className="check check-inline">
              <input type="checkbox" checked={closed} onChange={(e) => setQuery('closed', e.target.checked ? 'true' : null)} />
              <span>Show closed PRs</span>
            </label>
          )}
        </div>
      </div>

      {!list.data ? (
        list.error ? <ErrorBox error={list.error} /> : <Loading />
      ) : list.data.length === 0 ? (
        <p className="muted">No artifacts.</p>
      ) : (
        <ul className="artifact-list">
          {list.data.map((a) => (
            <ArtifactRow key={a.id} artifact={a} targets={shown} onDeploy={(target) => setDeploying({ target, artifact: a })} />
          ))}
        </ul>
      )}

      {deploying && <DeployDialog target={deploying.target} artifact={deploying.artifact} onClose={() => setDeploying(null)} />}
    </section>
  );
}

export function ArtifactRow({ artifact: a, targets, onDeploy }: { artifact: Artifact; targets: Target[]; onDeploy: (t: Target) => void }) {
  return (
    <li className="card artifact">
      <div className="artifact-main">
        <div className="artifact-title">
          <ArtifactRef a={a} />
          {a.source === 'pr' && a.refName && <span className="muted artifact-branch">{a.refName}</span>}
          {a.prClosed && <span className="badge badge-neutral">closed</span>}
        </div>
        <div className="artifact-meta muted">
          <a href={a.runUrl} target="_blank" rel="noreferrer">
            {a.workflow || 'workflow run'} <ExternalIcon />
          </a>
          <span>by {a.triggeringActor}</span>
          <RelTime at={a.createdAt} />
          {a.helmVersion && <span>chart <code>{a.helmVersion}</code></span>}
        </div>
        {a.otherWorkflows && (
          <p className="status-line status-text-warn">
            <WarningIcon /> Several workflows publish deploy artifacts for this commit. The newest one is shown.
          </p>
        )}
        {a.images.length > 0 && (
          <details className="images">
            <summary>{a.images.length} image{a.images.length === 1 ? '' : 's'}</summary>
            <ul className="plain-list">
              {a.images.map((img) => (
                <li key={img.name}>
                  <strong>{img.name}</strong> <code className="wrap">{img.ref}</code>
                </li>
              ))}
            </ul>
          </details>
        )}
      </div>

      <ul className="readiness" aria-label="Targets">
        {targets.map((t) => {
          // A target missing from readiness was not evaluated for this artifact.
          const reasons = a.readiness[t.id] ?? ['Not evaluated for this target'];
          const ok = reasons.length === 0;
          const deployed = a.deployedTo.includes(t.id);
          return (
            <li key={t.id} className="readiness-row">
              {ok ? (
                <span className="ready ready-ok" title="Deployable">
                  <CheckIcon /> {t.id}
                </span>
              ) : (
                <details className="ready ready-no">
                  <summary title={reasons.join('\n')}>
                    <CrossIcon /> {t.id} <span className="sr-only">not deployable, show reasons</span>
                  </summary>
                  <ul>
                    {reasons.map((r) => <li key={r}>{r}</li>)}
                  </ul>
                </details>
              )}
              {deployed && <span className="badge badge-accent">deployed</span>}
              {t.permissions.deploy && (
                <button type="button" className="btn btn-sm" disabled={!ok} onClick={() => onDeploy(t)} aria-label={`Deploy to ${t.id}`}>
                  Deploy
                </button>
              )}
            </li>
          );
        })}
      </ul>
    </li>
  );
}
