import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { ArtifactRef } from './common';

const pr = { source: 'pr' as const, pr: 42, prTitle: 'Fix login', prUrl: 'https://github.com/org/app/pull/42', sha: 'abcdef1234' };

describe('ArtifactRef', () => {
  it('links a pull request with its title', () => {
    render(<ArtifactRef a={pr} />);
    const link = screen.getByRole('link', { name: '#42 Fix login' }) as HTMLAnchorElement;
    expect(link.href).toBe(pr.prUrl);
  });

  it('shows the title without a link inside another link', () => {
    render(<ArtifactRef a={pr} link={false} />);
    expect(screen.queryByRole('link')).toBeNull();
    expect(screen.getByText('#42 Fix login')).toBeTruthy();
  });

  it('shows the branch for branch artifacts', () => {
    render(<ArtifactRef a={{ source: 'branch', refName: 'main', sha: 'abcdef1234' }} />);
    expect(screen.getByText('main')).toBeTruthy();
  });
});
