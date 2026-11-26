import { useEffect, useState } from 'react';
import { api, errorMessage } from '../api/client';
import type { AuditEvent, RoleList } from '../api/types';
import { ErrorBox, Loading, RelTime } from '../components/common';
import { PreviewTable } from '../components/tables';
import { useMe } from '../lib/me';
import { navigate, useLocation } from '../lib/router';
import { useApi } from '../lib/useApi';

const PAGE_SIZE = 100;

export function AdminPage() {
  const me = useMe();
  const { pathname, search } = useLocation();
  const tab = search.get('tab') ?? 'audit';

  if (!me.isAdmin) {
    return (
      <div className="page">
        <h1>Admin</h1>
        <div className="callout callout-warn">You are not allowed to see this page. It is only for platform admins.</div>
      </div>
    );
  }

  return (
    <div className="page">
      <header className="page-head">
        <h1>Admin</h1>
      </header>
      <div className="tabs" role="tablist" aria-label="Admin sections">
        <button type="button" role="tab" className="tab" aria-selected={tab === 'audit'} onClick={() => navigate(pathname, { replace: true })}>
          Audit log
        </button>
        <button type="button" role="tab" className="tab" aria-selected={tab === 'previews'} onClick={() => navigate(`${pathname}?tab=previews`, { replace: true })}>
          Previews
        </button>
        <button type="button" role="tab" className="tab" aria-selected={tab === 'roles'} onClick={() => navigate(`${pathname}?tab=roles`, { replace: true })}>
          Roles
        </button>
      </div>
      <div className="section">{tab === 'previews' ? <AdminPreviews /> : tab === 'roles' ? <AdminRoles /> : <AuditLog />}</div>
    </div>
  );
}

function AdminPreviews() {
  const res = useApi('admin-previews', api.adminPreviews, 30000);
  if (!res.data) return res.error ? <ErrorBox error={res.error} /> : <Loading />;
  const orphaned = res.data.filter((p) => p.orphaned).length;
  return (
    <>
      {orphaned > 0 && (
        <div className="callout callout-warn">
          {orphaned} orphaned preview{orphaned === 1 ? '' : 's'}: the target is no longer in the config. Delete them once you are sure they are not needed.
        </div>
      )}
      <PreviewTable previews={res.data} onChanged={res.reload} admin />
    </>
  );
}

// The partial import format of Keycloak (Realm settings > Action > Partial import).
// The admin console ignores ifResourceExists and uses its own dropdown, so the UI text says to pick Skip there.
export function keycloakImport(list: RoleList): string {
  return JSON.stringify({ ifResourceExists: 'SKIP', roles: { client: { [list.clientId]: list.roles } } }, null, 2);
}

function AdminRoles() {
  const res = useApi('admin-roles', api.roles);
  if (!res.data) return res.error ? <ErrorBox error={res.error} /> : <Loading />;
  const href = `data:application/json;charset=utf-8,${encodeURIComponent(keycloakImport(res.data))}`;
  return (
    <>
      <p className="muted">
        The client roles of <code>{res.data.clientId}</code> that the configuration uses. simple-cd does not create them.
        Import the file in Keycloak under Realm settings, Action, Partial import, tick Client roles and choose Skip for
        existing resources; existing roles and their assignments stay as they are.
        Who gets which role you decide in Keycloak.
      </p>
      <div className="toolbar">
        <a className="btn" href={href} download={`${res.data.clientId}-roles.json`}>Download Keycloak import</a>
        {res.data.realmSettingsUrl && (
          <a className="btn" href={res.data.realmSettingsUrl} target="_blank" rel="noopener noreferrer">
            Open realm settings in Keycloak
          </a>
        )}
      </div>
      <div className="table-wrap">
        <table className="table">
          <thead>
            <tr>
              <th>Role</th>
              <th>Description</th>
            </tr>
          </thead>
          <tbody>
            {res.data.roles.map((r) => (
              <tr key={r.name}>
                <td><code>{r.name}</code></td>
                <td>{r.description}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </>
  );
}

function AuditLog() {
  const [events, setEvents] = useState<AuditEvent[]>([]);
  const [done, setDone] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = async (before?: number) => {
    setLoading(true);
    setError(null);
    try {
      const page = await api.audit(before);
      setEvents((prev) => (before === undefined ? page : [...prev, ...page]));
      setDone(page.length < PAGE_SIZE);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void load();
  }, []);

  return (
    <>
      {events.length === 0 && !loading && !error && <p className="muted">No audit events.</p>}
      {events.length > 0 && (
        <div className="table-wrap">
          <table className="table">
            <thead>
              <tr>
                <th>Time</th>
                <th>Actor</th>
                <th>Action</th>
                <th>Object</th>
                <th>Details</th>
              </tr>
            </thead>
            <tbody>
              {events.map((e) => (
                <tr key={e.id}>
                  <td><RelTime at={e.at} /></td>
                  <td>{e.actor}</td>
                  <td><code>{e.action}</code></td>
                  <td className="wrap">{e.object}</td>
                  <td>
                    {e.details != null && (
                      <details>
                        <summary>Show</summary>
                        <pre className="json">{JSON.stringify(e.details, null, 2)}</pre>
                      </details>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {error && <p className="error-text" role="alert">{error}</p>}
      {loading && <Loading />}
      {!loading && !done && events.length > 0 && (
        <button type="button" className="btn load-more" onClick={() => load(events[events.length - 1].id)}>
          Load more
        </button>
      )}
    </>
  );
}
