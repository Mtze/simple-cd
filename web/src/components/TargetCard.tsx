import { useState, type FormEvent } from 'react';
import { api, errorMessage } from '../api/client';
import type { Target } from '../api/types';
import { useMe } from '../lib/me';
import { Link } from '../lib/router';
import { ArtifactRef, RelTime, Remaining, StatusBadge, TypeBadge } from './common';
import { ExternalIcon, LockIcon, WrenchIcon } from './icons';
import { Modal } from './Modal';

export function TargetCard({ target, onChanged, heading = 'h3', linkTitle = true }: {
  target: Target;
  onChanged: () => void;
  heading?: 'h1' | 'h3';
  linkTitle?: boolean;
}) {
  const H = heading;
  return (
    <article className="card target-card" aria-label={`Target ${target.id}`}>
      <div className="target-head">
        <H className="target-title">
          {linkTitle ? <Link href={`/targets/${encodeURIComponent(target.id)}`}>{target.id}</Link> : target.id}
        </H>
        <div className="badges">
          <TypeBadge type={target.type} />
          {target.preview && <span className="badge badge-neutral">preview</span>}
          <StatusBadge status={target.status} />
          {target.url && (
            <a className="btn btn-sm" href={target.url} target="_blank" rel="noopener noreferrer" aria-label={`Open ${target.id}`}>
              Open <ExternalIcon />
            </a>
          )}
        </div>
      </div>

      <TargetStatusLines target={target} />

      <dl className="kv">
        <dt>Deployed</dt>
        <dd>
          {target.preview ? (
            <span className="muted">One deployment per pull request</span>
          ) : target.current ? (
            <>
              <Link href={`/deployments/${target.current.deploymentId}`}>
                <ArtifactRef a={target.current} link={false} />
              </Link>{' '}
              <span className="muted">
                by {target.current.actor}, <RelTime at={target.current.finishedAt} />
              </span>
            </>
          ) : (
            <span className="muted">Nothing deployed yet</span>
          )}
        </dd>
        {target.active.length > 0 && (
          <>
            <dt>Active</dt>
            <dd>
              <ul className="plain-list">
                {target.active.map((d) => (
                  <li key={d.id}>
                    <Link href={`/deployments/${d.id}`}>
                      {d.kind === 'teardown' ? 'Teardown' : 'Deployment'} #{d.id}
                    </Link>{' '}
                    <StatusBadge status={d.status} />
                    {d.slot && <span className="muted"> PR #{d.slot}</span>}
                  </li>
                ))}
              </ul>
            </dd>
          </>
        )}
      </dl>

      <TargetActions target={target} onChanged={onChanged} />
    </article>
  );
}

export function TargetStatusLines({ target }: { target: Target }) {
  return (
    <>
      {target.maintenance && (
        <p className="status-line status-text-maintenance">
          <WrenchIcon />
          <span>
            {target.maintenance.reason || 'Maintenance'}{' '}
            <span className="muted">
              - {target.maintenance.by}, <RelTime at={target.maintenance.since} />
            </span>
          </span>
        </p>
      )}
      {target.lock && (
        <p className="status-line status-text-locked">
          <LockIcon />
          <span>
            Locked by {target.lock.holder}, <Remaining until={target.lock.until} />
          </span>
        </p>
      )}
    </>
  );
}

export function TargetActions({ target, onChanged }: { target: Target; onChanged: () => void }) {
  const me = useMe();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [asking, setAsking] = useState(false);

  const canRelease = !!target.lock && (target.lock.holder === me.user.username || target.permissions.override);
  const canMaintenance = target.permissions.maintenance;
  if (!canRelease && !canMaintenance) return null;

  const run = async (fn: () => Promise<unknown>) => {
    setBusy(true);
    setError(null);
    try {
      await fn();
      onChanged();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="actions">
      {canRelease && (
        <button type="button" className="btn" disabled={busy} onClick={() => run(() => api.releaseLock(target.id))}>
          Release lock
        </button>
      )}
      {canMaintenance &&
        (target.maintenance ? (
          <button type="button" className="btn" disabled={busy} onClick={() => run(() => api.setMaintenance(target.id, false, ''))}>
            End maintenance
          </button>
        ) : (
          <button type="button" className="btn" disabled={busy} onClick={() => setAsking(true)}>
            Start maintenance
          </button>
        ))}
      {error && (
        <p className="error-text" role="alert">
          {error}
        </p>
      )}
      {asking && (
        <MaintenanceDialog
          targetId={target.id}
          onClose={() => setAsking(false)}
          onDone={() => {
            setAsking(false);
            onChanged();
          }}
        />
      )}
    </div>
  );
}

function MaintenanceDialog({ targetId, onClose, onDone }: { targetId: string; onClose: () => void; onDone: () => void }) {
  const [reason, setReason] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api.setMaintenance(targetId, true, reason.trim());
      onDone();
    } catch (err) {
      setError(errorMessage(err));
      setBusy(false);
    }
  };

  return (
    <Modal title={`Start maintenance on ${targetId}`} onClose={onClose}>
      <form className="modal-body" onSubmit={submit}>
        <p className="muted">While in maintenance nobody can deploy to this target, auto-deploys included.</p>
        <label className="field">
          <span>Reason</span>
          <input value={reason} onChange={(e) => setReason(e.target.value)} required autoFocus placeholder="e.g. Docker upgrade" />
        </label>
        {error && <p className="error-text" role="alert">{error}</p>}
        <div className="modal-foot">
          <button type="button" className="btn" onClick={onClose}>Cancel</button>
          <button type="submit" className="btn btn-primary" disabled={busy || !reason.trim()}>Start maintenance</button>
        </div>
      </form>
    </Modal>
  );
}
