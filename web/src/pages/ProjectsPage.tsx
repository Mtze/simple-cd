import { api } from '../api/client';
import type { ProjectSummary, Target } from '../api/types';
import { ErrorBox, Loading, Remaining, shortSha, StatusBadge } from '../components/common';
import { Link } from '../lib/router';
import { useApi } from '../lib/useApi';

export function ProjectsPage() {
  const { data, error } = useApi('projects', api.projects, 30000);

  return (
    <div className="page">
      <header className="page-head">
        <h1>Projects</h1>
      </header>
      {!data ? (
        error ? <ErrorBox error={error} /> : <Loading />
      ) : data.length === 0 ? (
        <p className="muted">No projects are configured yet.</p>
      ) : (
        <div className="project-grid">
          {data.map((p) => <ProjectCard key={p.name} project={p} />)}
        </div>
      )}
    </div>
  );
}

function ProjectCard({ project }: { project: ProjectSummary }) {
  return (
    <article className="card project-card">
      <header className="project-card-head">
        <h2>
          <Link href={`/projects/${project.name}`}>{project.name}</Link>
        </h2>
        {project.role && <span className="badge badge-neutral">{project.role}</span>}
      </header>
      {project.targets.length === 0 ? (
        <p className="muted">No targets.</p>
      ) : (
        <ul className="target-rows">
          {project.targets.map((t) => <TargetRow key={t.id} target={t} livePreviews={project.livePreviews} />)}
        </ul>
      )}
    </article>
  );
}

function TargetRow({ target: t, livePreviews }: { target: Target; livePreviews: number }) {
  return (
    <li className="target-row">
      <Link className="target-row-name" href={`/targets/${encodeURIComponent(t.id)}`}>{t.id}</Link>
      <StatusBadge status={t.status} />
      <span className="target-row-detail muted">
        {t.status === 'locked' && t.lock ? (
          <>{t.lock.holder}, <Remaining until={t.lock.until} /></>
        ) : t.preview ? (
          `${livePreviews} live preview${livePreviews === 1 ? '' : 's'}`
        ) : t.current ? (
          <>
            <code className="sha">{shortSha(t.current.sha)}</code> {t.current.source === 'pr' && t.current.pr ? `#${t.current.pr}` : t.current.refName}
          </>
        ) : (
          'nothing deployed'
        )}
      </span>
    </li>
  );
}
