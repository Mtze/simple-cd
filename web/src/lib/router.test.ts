import { describe, expect, it } from 'vitest';
import { matchPath } from './router';

describe('matchPath', () => {
  it('matches static paths', () => {
    expect(matchPath('/', '/')).toEqual({});
    expect(matchPath('/admin', '/admin')).toEqual({});
    expect(matchPath('/admin', '/admin/')).toEqual({});
    expect(matchPath('/admin', '/')).toBeNull();
  });

  it('extracts and decodes params', () => {
    expect(matchPath('/projects/:owner/:repo', '/projects/org/app')).toEqual({ owner: 'org', repo: 'app' });
    expect(matchPath('/targets/:id', '/targets/app%20test')).toEqual({ id: 'app test' });
  });

  it('rejects different segment counts and literals', () => {
    expect(matchPath('/projects/:owner/:repo', '/projects/org')).toBeNull();
    expect(matchPath('/projects/:owner/:repo', '/projects/org/app/x')).toBeNull();
    expect(matchPath('/targets/:id', '/deployments/1')).toBeNull();
  });

  it('rejects malformed escapes', () => {
    expect(matchPath('/targets/:id', '/targets/%E0%A4%A')).toBeNull();
  });
});
