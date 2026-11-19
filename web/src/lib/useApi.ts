import { useCallback, useEffect, useState } from 'react';

type State<T> = { key: string | null; data?: T; error?: unknown; loading: boolean };

// Loads data for `key`; data from a previous key is never returned. With pollMs it refreshes quietly.
export function useApi<T>(key: string | null, load: () => Promise<T>, pollMs?: number) {
  const [state, setState] = useState<State<T>>({ key: null, loading: true });
  const [tick, setTick] = useState(0);

  useEffect(() => {
    if (key === null) return;
    let alive = true;
    setState((s) => ({ ...(s.key === key ? s : { key }), loading: true }));
    load().then(
      (data) => alive && setState({ key, data, loading: false }),
      (error: unknown) => alive && setState((s) => ({ key, data: s.key === key ? s.data : undefined, error, loading: false })),
    );
    return () => {
      alive = false;
    };
    // load is recreated each render; key identifies the request
  }, [key, tick]);

  useEffect(() => {
    if (!pollMs) return;
    const id = setInterval(() => setTick((t) => t + 1), pollMs);
    return () => clearInterval(id);
  }, [pollMs]);

  const reload = useCallback(() => setTick((t) => t + 1), []);
  const current = state.key === key;
  return {
    data: current ? state.data : undefined,
    error: current ? state.error : undefined,
    loading: !current || state.loading,
    reload,
  };
}
