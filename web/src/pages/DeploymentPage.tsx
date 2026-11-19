import { useState } from 'react';
import { api } from '../api/client';
import type { DeploymentStatus } from '../api/types';
import { ArtifactRef, ErrorBox, Loading, RelTime, StatusBadge } from '../components/common';
import { LogViewer } from '../components/LogViewer';
import { actorLabel, durationOf } from '../components/tables';
import { Link } from '../lib/router';
import { useNow } from '../lib/time';
import { useApi } from '../lib/useApi';

const STATUSES: DeploymentStatus[] = ['pending', 'running', 'succeeded', 'failed', 'timed_out'];

export function DeploymentPage({ id }: { id: string }) {
  const res = useApi(`deployment:${id}`, () => api.deployment(id));
  const [liveStatus, setLiveStatus] = useState<DeploymentStatus | null>(null);

  if (!res.data) return <div className="page">{res.error ? <ErrorBox error={res.error} /> : <Loading />}</div>;
  const d = res.data;
  const status = liveStatus ?? d.status;
  const finished = status === 'succeeded' || status === 'failed' || status === 'timed_out';

  return (
    <div className="page">
      <nav className="crumbs" aria-label="Breadcrumb">
        <Link href={`/projects/${d.target.project}`}>{d.target.project}</Link>
        <span aria-hidden="true">/</span>
        <Link href={`/targets/${encodeURIComponent(d.targetId)}`}>{d.targetId}</Link>
        <span aria-hidden="true">/</span>
        <span>#{d.id}</span>
      </nav>

      <header className="page-head">
        <h1>
          {d.kind === 'teardown' ? 'Teardown' : 'Deployment'} #{d.id}
        </h1>
        <StatusBadge status={status} />
      </header>

      <dl className="kv kv-wide card">
        <dt>Target</dt>
        <dd>
          <Link href={`/targets/${encodeURIComponent(d.targetId)}`}>{d.targetId}</Link>
          {d.slot && <span className="muted"> PR #{d.slot}</span>}
        </dd>
        <dt>Artifact</dt>
        <dd>{d.artifact ? <ArtifactRef a={d.artifact} /> : <span className="muted">-</span>}</dd>
        <dt>Actor</dt>
        <dd>{actorLabel(d)}</dd>
        <dt>Created</dt>
        <dd><RelTime at={d.createdAt} /></dd>
        {d.startedAt && (
          <>
            <dt>Started</dt>
            <dd><RelTime at={d.startedAt} /></dd>
            <dt>Duration</dt>
            <dd className="num">{d.finishedAt || finished ? durationOf(d) : <LiveDuration startedAt={d.startedAt} />}</dd>
          </>
        )}
        {d.finishedAt && (
          <>
            <dt>Finished</dt>
            <dd><RelTime at={d.finishedAt} /></dd>
          </>
        )}
      </dl>

      {d.error && (
        <div className="callout callout-danger" role="alert">
          <strong>Error:</strong> {d.error}
        </div>
      )}

      <section className="section" aria-labelledby="log-h">
        <h2 id="log-h">Log</h2>
        <LogViewer
          deploymentId={d.id}
          onStatus={(s) => {
            if ((STATUSES as string[]).includes(s)) setLiveStatus(s as DeploymentStatus);
          }}
          onEnd={() => {
            // Pick up finishedAt and error once the run is over.
            setLiveStatus(null);
            res.reload();
          }}
        />
      </section>
    </div>
  );
}

// Separate component so the per-second tick does not re-render the log.
function LiveDuration({ startedAt }: { startedAt: string }) {
  const now = useNow(1000);
  return <>{durationOf({ startedAt }, now)} so far</>;
}
