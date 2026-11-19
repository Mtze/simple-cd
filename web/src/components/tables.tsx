import { useState } from 'react';
import { api, errorMessage } from '../api/client';
import type { Deployment, Preview } from '../api/types';
import { Link } from '../lib/router';
import { formatDuration } from '../lib/time';
import { ArtifactRef, RelTime, Remaining, StatusBadge } from './common';

export function actorLabel(d: Pick<Deployment, 'actor' | 'actorDetail'>) {
  return d.actorDetail ? `${d.actor} (${d.actorDetail})` : d.actor;
}

export function durationOf(d: Pick<Deployment, 'startedAt' | 'finishedAt'>, now = Date.now()): string {
  if (!d.startedAt) return '-';
  return formatDuration((d.finishedAt ? Date.parse(d.finishedAt) : now) - Date.parse(d.startedAt));
}

export function DeploymentTable({ deployments }: { deployments: Deployment[] }) {
  if (deployments.length === 0) return <p className="muted">No deployments yet.</p>;
  return (
    <div className="table-wrap">
      <table className="table">
        <thead>
          <tr>
            <th>Status</th>
            <th>Artifact</th>
            <th>Actor</th>
            <th>Started</th>
            <th>Duration</th>
            <th><span className="sr-only">Details</span></th>
          </tr>
        </thead>
        <tbody>
          {deployments.map((d) => (
            <tr key={d.id}>
              <td><StatusBadge status={d.status} /></td>
              <td>
                {d.kind === 'teardown' && <span className="badge badge-neutral">teardown</span>}{' '}
                {d.artifact ? <ArtifactRef a={d.artifact} /> : <span className="muted">-</span>}
                {d.slot && <span className="muted"> PR #{d.slot}</span>}
              </td>
              <td>{actorLabel(d)}</td>
              <td><RelTime at={d.startedAt ?? d.createdAt} /></td>
              <td className="num">{durationOf(d)}</td>
              <td><Link href={`/deployments/${d.id}`}>Log</Link></td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export function PreviewTable({ previews, onChanged, admin }: { previews: Preview[]; onChanged: () => void; admin?: boolean }) {
  const [busy, setBusy] = useState<number | null>(null);
  const [error, setError] = useState<string | null>(null);

  if (previews.length === 0) return <p className="muted">No live previews.</p>;

  const teardown = async (p: Preview) => {
    const what = p.orphaned ? 'Delete the orphaned preview' : 'Tear down the preview';
    if (!window.confirm(`${what} for PR #${p.pr} (${p.namespace})? The namespace and everything in it will be deleted.`)) return;
    setBusy(p.id);
    setError(null);
    try {
      await api.teardownPreview(p.id);
      onChanged();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(null);
    }
  };

  return (
    <>
      {error && <p className="error-text" role="alert">{error}</p>}
      <div className="table-wrap">
        <table className="table">
          <thead>
            <tr>
              {admin && <th>Project</th>}
              <th>PR</th>
              <th>Namespace</th>
              <th>Target</th>
              <th>Expires</th>
              <th>Status</th>
              <th><span className="sr-only">Actions</span></th>
            </tr>
          </thead>
          <tbody>
            {previews.map((p) => (
              <tr key={p.id} className={p.orphaned ? 'row-warn' : undefined}>
                {admin && <td>{p.repo}</td>}
                <td>#{p.pr}</td>
                <td><code>{p.namespace}</code></td>
                <td>{p.targetId}</td>
                <td><Remaining until={p.expiresAt} /></td>
                <td>
                  <span className="badges">
                    <StatusBadge status={p.status} />
                    {p.orphaned && <StatusBadge status="orphaned" />}
                  </span>
                </td>
                <td className="cell-actions">
                  {p.canTeardown && p.status === 'active' && (
                    <button type="button" className="btn btn-sm btn-danger" disabled={busy === p.id} onClick={() => teardown(p)}>
                      {p.orphaned ? 'Forget' : 'Tear down'}
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </>
  );
}
