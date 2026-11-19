import { useState } from 'react';
import { ApiError, api, errorMessage } from '../api/client';
import type { ArtifactBrief, Target } from '../api/types';
import { useApi } from '../lib/useApi';
import { navigate } from '../lib/router';
import { formatAbsolute } from '../lib/time';
import { ArtifactRef, ErrorBox, Loading, Remaining, StatusBadge, TypeBadge } from './common';
import { Diff } from './Diff';
import { Modal } from './Modal';

export function DeployDialog({ target, artifact, onClose }: { target: Target; artifact: ArtifactBrief; onClose: () => void }) {
  const plan = useApi(`plan:${target.id}:${artifact.id}`, () => api.plan(target.id, artifact.id));
  // Values a failed submit taught us; they win over the plan.
  const [diffOverride, setDiffOverride] = useState<string | null>(null);
  const [reasonsOverride, setReasonsOverride] = useState<string[] | null>(null);
  const [lockOverride, setLockOverride] = useState<{ holder: string; until: string } | null>(null);
  const [inMaintenance, setInMaintenance] = useState(false);
  const [reviewed, setReviewed] = useState(false);
  const [takeover, setTakeover] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const p = plan.data;

  let body;
  if (!p) {
    body = plan.error ? <ErrorBox error={plan.error} /> : <Loading />;
  } else {
    const diff = diffOverride ?? p.diff ?? '';
    const reasons = reasonsOverride ?? p.reasons ?? [];
    const needsOverride = p.needsOverride || lockOverride !== null;
    const holder = lockOverride?.holder ?? p.lockHolder ?? 'someone';
    const until = lockOverride?.until ?? p.lockUntil;
    const maintenance = p.status === 'maintenance' || inMaintenance;
    const blocked = !p.permissions.deploy || reasons.length > 0 || maintenance || (needsOverride && !p.permissions.override);
    const ready = !blocked && !busy && (!diff || reviewed) && (!needsOverride || takeover);

    const submit = async () => {
      setBusy(true);
      setError(null);
      try {
        const d = await api.deploy(target.id, {
          artifactId: artifact.id,
          confirm: diff !== '' && reviewed,
          override: needsOverride && takeover,
        });
        onClose();
        navigate(`/deployments/${d.id}`);
      } catch (e) {
        setError(describeError(e, p.permissions.override));
        if (e instanceof ApiError) {
          const b = e.body;
          if (e.code === 'confirmation_required') {
            setDiffOverride(typeof b.diff === 'string' ? b.diff : '');
            setReviewed(false);
          } else if (e.code === 'locked') {
            setLockOverride({ holder: String(b.holder ?? 'someone'), until: String(b.until ?? '') });
            setTakeover(false);
          } else if (e.code === 'maintenance') {
            setInMaintenance(true);
          } else if (e.code === 'not_deployable') {
            setReasonsOverride(Array.isArray(b.reasons) ? b.reasons.map(String) : []);
          }
        }
        setBusy(false);
      }
    };

    body = (
      <div className="modal-body">
        <dl className="kv">
          <dt>Target</dt>
          <dd>
            <strong>{target.id}</strong> <TypeBadge type={target.type} /> <StatusBadge status={maintenance ? 'maintenance' : p.status} />
            {p.slot && <span className="muted"> slot PR #{p.slot}</span>}
          </dd>
          <dt>Artifact</dt>
          <dd>
            <ArtifactRef a={artifact} />
          </dd>
        </dl>

        {!p.permissions.deploy && (
          <div className="callout callout-warn">You do not have permission to deploy to this target.</div>
        )}

        {reasons.length > 0 && (
          <div className="callout callout-danger">
            <strong>This artifact cannot be deployed to {target.id}:</strong>
            <ul>
              {reasons.map((r) => <li key={r}>{r}</li>)}
            </ul>
          </div>
        )}

        {maintenance && (
          <div className="callout callout-warn">
            {target.id} is in maintenance{target.maintenance?.reason ? `: ${target.maintenance.reason}` : ''}. Nobody can deploy until it ends.
          </div>
        )}

        {needsOverride && !maintenance &&
          (p.permissions.override ? (
            <label className="check">
              <input type="checkbox" checked={takeover} onChange={(e) => setTakeover(e.target.checked)} />
              <span>
                Take over lock from {holder}
                {until && <span className="muted"> (held for <Remaining until={until} />)</span>}
              </span>
            </label>
          ) : (
            <div className="callout callout-warn">
              {target.id} is locked by {holder}
              {until ? ` until ${formatAbsolute(until)}` : ''}. Only {holder} or a maintainer can deploy now.
            </div>
          ))}

        {diff && (
          <section className="stack-sm">
            <h3 className="section-label">Configuration change</h3>
            <Diff text={diff} />
            <label className="check">
              <input type="checkbox" checked={reviewed} onChange={(e) => setReviewed(e.target.checked)} />
              <span>I reviewed the configuration change</span>
            </label>
          </section>
        )}

        {error && (
          <div className="callout callout-danger" role="alert">
            {error}
          </div>
        )}

        <div className="modal-foot">
          <button type="button" className="btn" onClick={onClose}>
            Cancel
          </button>
          <button type="button" className="btn btn-primary" disabled={!ready} onClick={submit}>
            {busy ? 'Deploying...' : 'Deploy'}
          </button>
        </div>
      </div>
    );
  }

  return (
    <Modal title={`Deploy to ${target.id}`} onClose={onClose} wide>
      {body}
    </Modal>
  );
}

function describeError(e: unknown, canOverride: boolean): string {
  if (!(e instanceof ApiError)) return errorMessage(e);
  const b = e.body;
  switch (e.code) {
    case 'confirmation_required':
      return 'The configuration changed. Review the diff below and confirm to continue.';
    case 'locked': {
      const until = typeof b.until === 'string' && b.until ? ` until ${formatAbsolute(b.until)}` : '';
      return canOverride
        ? `The target is locked by ${String(b.holder ?? 'someone')}${until}. Take over the lock to continue.`
        : `The target is locked by ${String(b.holder ?? 'someone')}${until}.`;
    }
    case 'maintenance':
      return 'The target is in maintenance. Nobody can deploy until it ends.';
    case 'slot_busy':
      return 'Another deployment is running for this slot. Wait until it finishes, then try again.';
    case 'not_deployable':
      return 'The artifact is not deployable to this target.';
    case 'forbidden':
      return 'You are not allowed to deploy to this target.';
  }
  if (e.status === 403) return 'You are not allowed to deploy to this target.';
  return e.message;
}
