function lineClass(l: string): string {
  if (l.startsWith('+++') || l.startsWith('---')) return 'diff-file';
  if (l.startsWith('@@')) return 'diff-hunk';
  if (l.startsWith('+')) return 'diff-add';
  if (l.startsWith('-')) return 'diff-del';
  return '';
}

export function Diff({ text }: { text: string }) {
  return (
    <pre className="diff" tabIndex={0} aria-label="Configuration diff">
      <code>
        {text.split('\n').map((l, i) => (
          <span key={i} className={lineClass(l)}>
            {l || ' '}
          </span>
        ))}
      </code>
    </pre>
  );
}
