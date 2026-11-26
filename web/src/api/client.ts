import type {
  Artifact, AuditEvent, Deployment, DeploymentDetail, Me, Plan, Preview, Project,
  ProjectSummary, RoleList, Source, TargetDetail,
} from './types';

export class ApiError extends Error {
  status: number;
  code: string;
  body: Record<string, unknown>;

  constructor(status: number, code: string, message: string, body: Record<string, unknown>) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
    this.body = body;
  }
}

// Indirection so tests can observe full-page navigations (jsdom cannot navigate).
export const browser = {
  go(url: string) {
    window.location.href = url;
  },
};

export function loginUrl(): string {
  return `/auth/login?return=${encodeURIComponent(window.location.pathname + window.location.search)}`;
}

export async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' };
  if (method !== 'GET') headers['X-Requested-With'] = 'simple-cd';
  if (body !== undefined) headers['Content-Type'] = 'application/json';

  const res = await fetch(path, {
    method,
    headers,
    credentials: 'same-origin',
    body: body === undefined ? undefined : JSON.stringify(body),
  });

  if (res.status === 401) {
    browser.go(loginUrl());
    // The page is about to leave; never settle so callers do not flash an error.
    return new Promise<T>(() => {});
  }

  const text = await res.text();
  let data: unknown;
  try {
    data = text ? JSON.parse(text) : undefined;
  } catch {
    data = undefined;
  }

  if (!res.ok) {
    const obj = data && typeof data === 'object' ? (data as Record<string, unknown>) : {};
    const code = typeof obj.code === 'string' ? obj.code : `http_${res.status}`;
    const message = typeof obj.error === 'string' ? obj.error : res.statusText || `Request failed (${res.status})`;
    throw new ApiError(res.status, code, message, obj);
  }
  return data as T;
}

const seg = encodeURIComponent;
const proj = (name: string) => name.split('/').map(seg).join('/');

export const api = {
  me: () => request<Me>('GET', '/api/me'),
  projects: () => request<ProjectSummary[]>('GET', '/api/projects'),
  project: (name: string) => request<Project>('GET', `/api/projects/${proj(name)}`),
  artifacts: (name: string, source: Source, closed: boolean) =>
    request<Artifact[]>('GET', `/api/projects/${proj(name)}/artifacts?source=${source}${closed ? '&closed=true' : ''}`),
  target: (id: string) => request<TargetDetail>('GET', `/api/targets/${seg(id)}`),
  plan: (targetId: string, artifactId: number) =>
    request<Plan>('GET', `/api/targets/${seg(targetId)}/plan?artifact=${artifactId}`),
  deploy: (targetId: string, body: { artifactId: number; confirm: boolean; override: boolean }) =>
    request<Deployment>('POST', `/api/targets/${seg(targetId)}/deployments`, body),
  releaseLock: (targetId: string) => request<void>('POST', `/api/targets/${seg(targetId)}/lock/release`),
  setMaintenance: (targetId: string, enabled: boolean, reason: string) =>
    request<void>('PUT', `/api/targets/${seg(targetId)}/maintenance`, { enabled, reason }),
  deployment: (id: string) => request<DeploymentDetail>('GET', `/api/deployments/${seg(id)}`),
  teardownPreview: (id: number) => request<void>('DELETE', `/api/previews/${id}`),
  audit: (before?: number) =>
    request<AuditEvent[]>('GET', `/api/admin/audit${before === undefined ? '' : `?before=${before}`}`),
  adminPreviews: () => request<Preview[]>('GET', '/api/admin/previews'),
  roles: () => request<RoleList>('GET', '/api/admin/roles'),
  logout: () => request<{ logoutUrl: string }>('POST', '/auth/logout'),
};

export function errorMessage(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}
