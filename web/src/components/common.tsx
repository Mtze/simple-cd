import type { ReactNode } from 'react';
import { ApiError } from '../api/client';
import type { DeploymentStatus, Source, TargetStatus } from '../api/types';
import { formatAbsolute, formatRelative, formatRemaining, useNow } from '../lib/time';
import { BranchIcon, CheckIcon, CrossIcon, LockIcon, PullRequestIcon, TagIcon, WarningIcon, WrenchIcon } from './icons';

type AnyStatus = TargetStatus | DeploymentStatus | 'active' | 'deleting' | 'deleted' | 'orphaned';

const LABELS: Record<AnyStatus, string> = {
  maintenance: 'Maintenance',
  deploying: 'Deploying',
  locked: 'Locked',
  available: 'Available',
  pending: 'Pending',
  running: 'Running',
  succeeded: 'Succeeded',
  failed: 'Failed',
  timed_out: 'Timed out',
  active: 'Active',
  deleting: 'Deleting',
  deleted: 'Deleted',
  orphaned: 'Orphaned',
};

const ICONS: Partial<Record<AnyStatus, ReactNode>> = {
  maintenance: <WrenchIcon />,
  locked: <LockIcon />,
  succeeded: <CheckIcon />,
  failed: <CrossIcon />,
  timed_out: <WarningIcon />,
  orphaned: <WarningIcon />,
};

export function StatusBadge({ status }: { status: AnyStatus }) {
  const live = status === 'deploying' || status === 'running' || status === 'pending' || status === 'deleting';
  return (
    <span className={`badge status-${status}`}>
      {ICONS[status] ?? <span className={live ? 'dot pulse' : 'dot'} aria-hidden="true" />}
      {LABELS[status] ?? status}
    </span>
  );
}

export function RelTime({ at }: { at: string }) {
  const now = useNow();
  return (
    <time className="num" dateTime={at} title={formatAbsolute(at)}>
      {formatRelative(at, now)}
    </time>
  );
}

export function Remaining({ until }: { until: string }) {
  const now = useNow();
  return (
    <time className="num" dateTime={until} title={`until ${formatAbsolute(until)}`}>
      {formatRemaining(until, now)}
    </time>
  );
}

export const shortSha = (sha?: string) => (sha ?? '').slice(0, 7);

export function SourceIcon({ source }: { source?: Source }) {
  if (source === 'pr') return <PullRequestIcon aria-label="Pull request" role="img" aria-hidden={undefined} />;
  if (source === 'release') return <TagIcon aria-label="Release" role="img" aria-hidden={undefined} />;
  if (source === 'branch') return <BranchIcon aria-label="Branch" role="img" aria-hidden={undefined} />;
  return null;
}

type RefData = { source?: Source; pr?: number; prTitle?: string; prUrl?: string; refName?: string; sha?: string };

// link=false where the whole ref already sits inside a link.
export function ArtifactRef({ a, link = true }: { a: RefData; link?: boolean }) {
  const isPR = a.source === 'pr' && a.pr;
  const label = isPR ? (a.prTitle ? `#${a.pr} ${a.prTitle}` : `#${a.pr}`) : a.refName;
  return (
    <span className="artifact-ref">
      <SourceIcon source={a.source} />
      {isPR && link && a.prUrl ? (
        <a className="ref-label" href={a.prUrl} target="_blank" rel="noopener noreferrer" title={label}>{label}</a>
      ) : (
        <span className="ref-label" title={label}>{label}</span>
      )}
      {a.sha && <code className="sha" title={a.sha}>{shortSha(a.sha)}</code>}
    </span>
  );
}

export function ErrorBox({ error }: { error: unknown }) {
  const msg =
    error instanceof ApiError && error.status === 404
      ? 'Not found.'
      : error instanceof ApiError && error.status === 403
        ? 'You are not allowed to see this.'
        : error instanceof Error
          ? error.message
          : 'Something went wrong.';
  return (
    <div className="callout callout-danger" role="alert">
      <WarningIcon /> <span>{msg}</span>
    </div>
  );
}

export function Loading() {
  return <p className="muted" aria-live="polite">Loading...</p>;
}

export function TypeBadge({ type }: { type: string }) {
  return <span className="badge badge-neutral">{type}</span>;
}
