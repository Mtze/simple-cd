import { screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { json, ME, mockFetch, renderWithMe } from '../test/helpers';
import { AdminPage, keycloakImport } from './AdminPage';

const list = {
  clientId: 'simple-cd',
  realmSettingsUrl: 'https://kc.example.org/admin/main/console/#/main/realm-settings',
  roles: [
    { name: 'platform-admin', description: 'everything' },
    { name: 'project:org/app:developer', description: 'deploy org/app' },
  ],
};

afterEach(() => {
  vi.unstubAllGlobals();
  window.history.replaceState(null, '', '/');
});

describe('AdminPage roles', () => {
  it('lists the roles and offers the Keycloak import', async () => {
    window.history.replaceState(null, '', '/admin?tab=roles');
    mockFetch({ 'GET /api/admin/roles': () => json(200, list) });
    renderWithMe(<AdminPage />, { ...ME, isAdmin: true });

    expect(await screen.findByText('project:org/app:developer')).toBeTruthy();
    const link = screen.getByRole('link', { name: 'Download Keycloak import' }) as HTMLAnchorElement;
    expect(link.getAttribute('download')).toBe('simple-cd-roles.json');
    expect(decodeURIComponent(link.href.split(',')[1])).toBe(keycloakImport(list));
    const kc = screen.getByRole('link', { name: 'Open realm settings in Keycloak' }) as HTMLAnchorElement;
    expect(kc.href).toBe(list.realmSettingsUrl);
  });

  it('hides the Keycloak link for other identity providers', async () => {
    window.history.replaceState(null, '', '/admin?tab=roles');
    mockFetch({ 'GET /api/admin/roles': () => json(200, { ...list, realmSettingsUrl: '' }) });
    renderWithMe(<AdminPage />, { ...ME, isAdmin: true });

    expect(await screen.findByText('project:org/app:developer')).toBeTruthy();
    expect(screen.queryByRole('link', { name: 'Open realm settings in Keycloak' })).toBeNull();
  });

  it('builds a partial import that skips existing roles', () => {
    expect(JSON.parse(keycloakImport(list))).toEqual({
      ifResourceExists: 'SKIP',
      roles: { client: { 'simple-cd': list.roles } },
    });
  });
});
