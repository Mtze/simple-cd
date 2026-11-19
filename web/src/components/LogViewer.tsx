import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { ArrowDownIcon } from './icons';

// Streams /api/deployments/{id}/log. Lines are buffered and flushed once per frame,
// so a big stored log does not re-render per line.
export function LogViewer({ deploymentId, onStatus, onEnd }: {
  deploymentId: number;
  onStatus: (status: string) => void;
  onEnd: () => void;
}) {
  const lines = useRef<string[]>([]);
  const [, setVersion] = useState(0);
  const [state, setState] = useState<'streaming' | 'ended' | 'lost'>('streaming');
  const [attempt, setAttempt] = useState(0);
  const [stuck, setStuck] = useState(true);
  const pane = useRef<HTMLPreElement>(null);
  const cbs = useRef({ onStatus, onEnd });
  cbs.current = { onStatus, onEnd };

  useEffect(() => {
    lines.current = [];
    setVersion((v) => v + 1);
    setState('streaming');
    let frame = 0;
    const flush = () => {
      frame = 0;
      setVersion((v) => v + 1);
    };
    const es = new EventSource(`/api/deployments/${deploymentId}/log`);
    es.addEventListener('log', (e) => {
      lines.current.push((e as MessageEvent<string>).data);
      if (!frame) frame = requestAnimationFrame(flush);
    });
    es.addEventListener('status', (e) => cbs.current.onStatus((e as MessageEvent<string>).data));
    es.addEventListener('end', () => {
      es.close();
      setState('ended');
      cbs.current.onEnd();
    });
    // EventSource would reconnect and replay the log; stop and let the user retry instead.
    es.onerror = () => {
      es.close();
      setState((s) => (s === 'ended' ? s : 'lost'));
    };
    return () => {
      es.close();
      if (frame) cancelAnimationFrame(frame);
    };
  }, [deploymentId, attempt]);

  useLayoutEffect(() => {
    if (stuck && pane.current) pane.current.scrollTop = pane.current.scrollHeight;
  });

  const onScroll = () => {
    const el = pane.current;
    if (el) setStuck(el.scrollHeight - el.scrollTop - el.clientHeight < 24);
  };

  const jump = () => {
    setStuck(true);
    if (pane.current) pane.current.scrollTop = pane.current.scrollHeight;
  };

  return (
    <div className="log">
      <div className="log-bar">
        <span>{lines.current.length} lines</span>
        <span aria-live="polite">
          {state === 'streaming' && <span className="log-live"><span className="dot pulse" aria-hidden="true" /> Live</span>}
          {state === 'ended' && 'Complete'}
          {state === 'lost' && (
            <>
              Connection lost.{' '}
              <button type="button" className="link-btn" onClick={() => setAttempt((a) => a + 1)}>Reconnect</button>
            </>
          )}
        </span>
      </div>
      <pre ref={pane} className="log-pane" tabIndex={0} aria-label="Deployment log" onScroll={onScroll}>
        {lines.current.length ? lines.current.join('\n') : state === 'streaming' ? 'Waiting for output...' : 'No output.'}
      </pre>
      {!stuck && (
        <button type="button" className="btn btn-sm log-jump" onClick={jump}>
          <ArrowDownIcon /> Jump to bottom
        </button>
      )}
    </div>
  );
}
