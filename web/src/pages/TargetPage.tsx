import { api } from '../api/client';
import type { LockEvent } from '../api/types';
import { ErrorBox, Loading, RelTime } from '../components/common';
import { DeploymentTable } from '../components/tables';
import { TargetCard } from '../components/TargetCard';
import { Link } from '../lib/router';
import { formatAbsolute } from '../lib/time';
import { useApi } from '../lib/useApi';

const LOCK_ACTIONS: Record<LockEvent['action'], string> = {
  acquired: 'acquired the lock',
  extended: 'extended the lock',
  released: 'released the lock',
  overridden: 'took over the lock',
};

export function TargetPage({ id }: { id: string }) {
  const res = useApi(`target:${id}`, () => api.target(id), 15000);

  if (!res.data) return <div className="page">{res.error ? <ErrorBox error={res.error} /> : <Loading />}</div>;
  const { target, deployments, lockHistory } = res.data;

  return (
    <div className="page">
      <nav className="crumbs" aria-label="Breadcrumb">
        <Link href={`/projects/${target.project}`}>{target.project}</Link>
        <span aria-hidden="true">/</span>
        <span>{target.id}</span>
      </nav>

      <TargetCard target={target} onChanged={res.reload} heading="h1" linkTitle={false} />

      <section className="section" aria-labelledby="history-h">
        <h2 id="history-h">Deployments</h2>
        <DeploymentTable deployments={deployments} />
      </section>

      {(target.lockable || lockHistory.length > 0) && (
        <section className="section" aria-labelledby="locks-h">
          <h2 id="locks-h">Lock history</h2>
          {lockHistory.length === 0 ? (
            <p className="muted">The lock was never taken.</p>
          ) : (
            <ol className="timeline">
              {lockHistory.map((e) => (
                <li key={e.id}>
                  <span>
                    <strong>{e.byUser}</strong> {LOCK_ACTIONS[e.action] ?? e.action}
                    {e.action === 'overridden' && e.holder !== e.byUser && <> from {e.holder}</>}
                    {e.until && e.action !== 'released' && (
                      <span className="muted num"> until {formatAbsolute(e.until)}</span>
                    )}
                  </span>
                  <RelTime at={e.at} />
                </li>
              ))}
            </ol>
          )}
        </section>
      )}
    </div>
  );
}
