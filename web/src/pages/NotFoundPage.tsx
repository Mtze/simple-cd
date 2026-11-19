import { Link } from '../lib/router';

export function NotFoundPage() {
  return (
    <div className="page empty-page">
      <p className="big-code num">404</p>
      <h1>Page not found</h1>
      <p className="muted">This page does not exist.</p>
      <p>
        <Link href="/">Back to projects</Link>
      </p>
    </div>
  );
}
