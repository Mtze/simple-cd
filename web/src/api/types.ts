// Mirrors docs/reference/api.md. Keep in sync with the server contract.

export type Permissions = { deploy: boolean; override: boolean; maintenance: boolean };

export type TargetStatus = 'maintenance' | 'deploying' | 'locked' | 'available';
export type Source = 'pr' | 'branch' | 'release';
export type DeploymentStatus = 'pending' | 'running' | 'succeeded' | 'failed' | 'timed_out';

export type Target = {
  id: string;
  project: string;
  type: 'helm' | 'compose' | 'ansible';
  preview: boolean;
  lockable: boolean;
  lockDurationSeconds: number;
  role?: string;
  url?: string;
  autoDeploy?: { from: Source[]; branches?: string[] };
  status: TargetStatus;
  maintenance?: { reason: string; by: string; since: string };
  lock?: { holder: string; until: string };
  current?: Deployed;
  active: Deployment[];
  permissions: Permissions;
};

export type Deployed = {
  deploymentId: number;
  artifactId?: number;
  sha?: string;
  refName?: string;
  source?: Source;
  pr?: number;
  prTitle?: string;
  actor: string;
  finishedAt: string;
};

export type Deployment = {
  id: number;
  kind: 'deploy' | 'teardown';
  artifactId?: number;
  targetId: string;
  slot: string;
  actor: string;
  actorDetail?: string;
  status: DeploymentStatus;
  error?: string;
  createdAt: string;
  startedAt?: string;
  finishedAt?: string;
  artifact?: ArtifactBrief;
};

export type ArtifactBrief = {
  id: number;
  repo: string;
  sha: string;
  source: Source;
  refName: string;
  pr?: number;
  prTitle?: string;
  prUrl?: string;
};

export type Artifact = ArtifactBrief & {
  prClosed: boolean;
  runUrl: string;
  workflow: string;
  triggeringActor: string;
  otherWorkflows: boolean;
  images: { name: string; ref: string }[];
  helmVersion?: string;
  autoDeploy: string[];
  createdAt: string;
  readiness: Record<string, string[]>;
  deployedTo: string[];
};

export type Preview = {
  id: number;
  repo: string;
  pr: number;
  targetId: string;
  namespace: string;
  status: 'active' | 'deleting' | 'deleted';
  expiresAt: string;
  createdAt: string;
  updatedAt: string;
  orphaned: boolean;
  canTeardown: boolean;
  url?: string;
};

export type User = { username: string; name: string; email: string; roles: string[] };

export type Me = { user: User; isAdmin: boolean; configError?: string };

export type ProjectSummary = { name: string; role: string; targets: Target[]; livePreviews: number };
export type Project = { name: string; role: string; targets: Target[]; previews: Preview[] };

export type LockEvent = {
  id: number;
  targetId: string;
  holder: string;
  action: 'acquired' | 'extended' | 'released' | 'overridden';
  byUser: string;
  until?: string;
  at: string;
};

export type TargetDetail = { target: Target; deployments: Deployment[]; lockHistory: LockEvent[] };

export type Plan = {
  targetId: string;
  artifactId: number;
  slot: string;
  status: TargetStatus;
  reasons: string[];
  diff: string;
  needsOverride: boolean;
  lockHolder?: string;
  lockUntil?: string;
  permissions: Permissions;
};

export type DeploymentDetail = Deployment & { target: Target };

// Pushed over GET /api/events when data changed on any replica.
export type ChangeEvent = { kind: 'artifacts'; project: string };

export type Role = { name: string; description: string };
// realmSettingsUrl is empty when the issuer is not Keycloak.
export type RoleList = { clientId: string; roles: Role[]; realmSettingsUrl: string };

export type AuditEvent = { id: number; at: string; actor: string; action: string; object: string; details: unknown };
