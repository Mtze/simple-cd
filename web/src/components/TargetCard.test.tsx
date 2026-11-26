import { screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import type { Permissions, Target } from '../api/types';
import { makeTarget, renderWithMe } from '../test/helpers';
import { TargetCard } from './TargetCard';

const perms = (p: Partial<Permissions> = {}): Permissions => ({ deploy: false, override: false, maintenance: false, ...p });
const lockedBy = (holder: string) => ({ holder, until: new Date(Date.now() + 42 * 60_000).toISOString() });

function renderCard(t: Partial<Target>) {
  return renderWithMe(<TargetCard target={makeTarget(t)} onChanged={() => {}} />);
}

const btn = (name: string) => screen.queryByRole('button', { name });

describe('TargetCard actions', () => {
  it('renders no actions for read-only users', () => {
    renderCard({ status: 'locked', lock: lockedBy('bob') });
    expect(btn('Release lock')).toBeNull();
    expect(btn('Start maintenance')).toBeNull();
    expect(btn('End maintenance')).toBeNull();
  });

  it('shows the lock holder with a countdown', () => {
    renderCard({ status: 'locked', lock: lockedBy('bob') });
    expect(screen.getByText(/Locked by bob/).textContent).toBe('Locked by bob, 42 min left');
  });

  it('lets the lock holder release their own lock', () => {
    renderCard({ status: 'locked', lock: lockedBy('alice'), permissions: perms({ deploy: true }) });
    expect(btn('Release lock')).not.toBeNull();
  });

  it("hides release for someone else's lock without override", () => {
    renderCard({ status: 'locked', lock: lockedBy('bob'), permissions: perms({ deploy: true }) });
    expect(btn('Release lock')).toBeNull();
  });

  it("allows release of someone else's lock with override", () => {
    renderCard({ status: 'locked', lock: lockedBy('bob'), permissions: perms({ deploy: true, override: true }) });
    expect(btn('Release lock')).not.toBeNull();
  });

  it('shows maintenance toggles only with the maintenance permission', () => {
    renderCard({ permissions: perms({ deploy: true, override: true }) });
    expect(btn('Start maintenance')).toBeNull();
  });

  it('offers to start or end maintenance', () => {
    const { unmount } = renderCard({ permissions: perms({ maintenance: true }) });
    expect(btn('Start maintenance')).not.toBeNull();
    unmount();

    renderCard({
      status: 'maintenance',
      maintenance: { reason: 'Docker upgrade', by: 'carol', since: new Date().toISOString() },
      permissions: perms({ maintenance: true }),
    });
    expect(btn('End maintenance')).not.toBeNull();
    expect(screen.getByText('Docker upgrade')).toBeTruthy();
  });
});

describe('TargetCard url', () => {
  it('offers to open the environment when the target has a url', () => {
    renderCard({ url: 'https://app-test.example.org' });
    const link = screen.getByRole('link', { name: 'Open app-test' }) as HTMLAnchorElement;
    expect(link.href).toBe('https://app-test.example.org/');
    expect(link.target).toBe('_blank');
  });

  it('shows no open link without a url', () => {
    renderCard({});
    expect(screen.queryByRole('link', { name: 'Open app-test' })).toBeNull();
  });
});
