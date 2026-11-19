import { useEffect, useState, type ReactNode } from 'react';
import { api, errorMessage } from './api/client';
import type { Me } from './api/types';
import { LogoMark, WarningIcon, CrossIcon } from './components/icons';
import { UserMenu } from './components/UserMenu';
import { MeContext } from './lib/me';
import { Link, matchPath, useLocation, type Params } from './lib/router';
import { AdminPage } from './pages/AdminPage';
import { DeploymentPage } from './pages/DeploymentPage';
import { NotFoundPage } from './pages/NotFoundPage';
import { ProjectPage } from './pages/ProjectPage';
import { ProjectsPage } from './pages/ProjectsPage';
import { TargetPage } from './pages/TargetPage';

const ROUTES: [string, (p: Params) => ReactNode][] = [
  ['/', () => <ProjectsPage />],
  ['/projects/:owner/:repo', (p) => <ProjectPage key={`${p.owner}/${p.repo}`} owner={p.owner} repo={p.repo} />],
  ['/targets/:id', (p) => <TargetPage key={p.id} id={p.id} />],
  ['/deployments/:id', (p) => <DeploymentPage key={p.id} id={p.id} />],
  ['/admin', () => <AdminPage />],
];

function route(pathname: string): ReactNode {
  for (const [pattern, render] of ROUTES) {
    const params = matchPath(pattern, pathname);
    if (params) return render(params);
  }
  return <NotFoundPage />;
}

export function App() {
  const [me, setMe] = useState<Me | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api.me().then(setMe, (e) => setError(errorMessage(e)));
  }, []);

  if (!me) {
    return (
      <div className="boot" aria-busy={!error}>
        <LogoMark />
        {error ? <p role="alert">Could not reach the server: {error}</p> : <p className="muted">Loading...</p>}
      </div>
    );
  }

  return (
    <MeContext.Provider value={me}>
      <Shell me={me} />
    </MeContext.Provider>
  );
}

function Shell({ me }: { me: Me }) {
  const { pathname } = useLocation();
  const [bannerHidden, setBannerHidden] = useState(false);
  const section = pathname.startsWith('/admin') ? 'admin' : 'projects';

  return (
    <>
      <a className="skip-link" href="#main">Skip to content</a>
      <header className="topbar">
        <div className="topbar-inner">
          <Link href="/" className="brand">
            <LogoMark />
            <span>simple-cd</span>
          </Link>
          <nav className="nav" aria-label="Main">
            <Link href="/" aria-current={section === 'projects' ? 'page' : undefined}>Projects</Link>
            {me.isAdmin && (
              <Link href="/admin" aria-current={section === 'admin' ? 'page' : undefined}>Admin</Link>
            )}
          </nav>
          <UserMenu user={me.user} />
        </div>
      </header>
      {me.configError && !bannerHidden && (
        <div className="banner" role="alert">
          <div className="banner-inner">
            <WarningIcon />
            <div className="banner-text">
              <strong>The last target config reload failed.</strong> The previous config is still active.
              <code className="banner-msg">{me.configError}</code>
            </div>
            <button type="button" className="icon-btn" aria-label="Dismiss warning" onClick={() => setBannerHidden(true)}>
              <CrossIcon />
            </button>
          </div>
        </div>
      )}
      <main id="main" className="main">{route(pathname)}</main>
    </>
  );
}
