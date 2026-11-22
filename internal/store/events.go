package store

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"
)

// Change events tell every replica that data changed, so open UIs refresh
// at once instead of on their next poll. Postgres delivers a NOTIFY only
// after its transaction commits, to every connection that listens.
const eventsChannel = "simplecd_events"

// EventArtifacts: the artifact list of Project changed (new build, newer run, PR closed).
const EventArtifacts = "artifacts"

type Event struct {
	Kind    string `json:"kind"`
	Project string `json:"project"`
}

// notify is best effort: a lost event only delays the UI until its next poll.
func (s *Store) notify(ctx context.Context, e Event) {
	b, _ := json.Marshal(e)
	if _, err := s.db.Exec(ctx, `SELECT pg_notify($1, $2)`, eventsChannel, string(b)); err != nil {
		slog.Warn("change event not sent", "kind", e.Kind, "err", err)
	}
}

// Listen calls fn for every change event until ctx ends. It holds one
// connection of its own and reconnects after errors.
func (s *Store) Listen(ctx context.Context, fn func(Event)) {
	for ctx.Err() == nil {
		err := s.listen(ctx, fn)
		if ctx.Err() != nil {
			return
		}
		slog.Warn("change event listener stopped, reconnecting", "err", err)
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
		}
	}
}

func (s *Store) listen(ctx context.Context, fn func(Event)) error {
	pooled, err := s.Pool.Acquire(ctx)
	if err != nil {
		return err
	}
	// A listening connection must not go back to the pool.
	conn := pooled.Hijack()
	defer conn.Close(context.Background())
	if _, err := conn.Exec(ctx, "LISTEN "+eventsChannel); err != nil {
		return err
	}
	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		var e Event
		if json.Unmarshal([]byte(n.Payload), &e) == nil {
			fn(e)
		}
	}
}
