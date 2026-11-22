package store_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Mtze/simple-cd/internal/repoconfig"
	"github.com/Mtze/simple-cd/internal/store"
	"github.com/Mtze/simple-cd/internal/store/storetest"
)

var ctx = context.Background()

func artifact(sha string, run int64, workflow string) *store.Artifact {
	pr := 7
	return &store.Artifact{
		Repo: "org/app", SHA: sha, Source: "pr", RefName: "feature", PR: &pr, RunID: run, Workflow: workflow,
		Manifest: repoconfig.Manifest{Version: 1, Images: []repoconfig.Image{{Name: "app", Ref: "ghcr.io/org/app@sha256:abc"}}},
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	s := storetest.New(t)
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestUpsertArtifactNewestRunWins(t *testing.T) {
	s := storetest.New(t)
	a, applied, err := s.UpsertArtifact(ctx, artifact("aaa", 10, "ci"))
	if err != nil || !applied {
		t.Fatal(err, applied)
	}
	if _, applied, _ := s.UpsertArtifact(ctx, artifact("aaa", 5, "ci")); applied {
		t.Fatal("older run replaced newer one")
	}
	b, applied, err := s.UpsertArtifact(ctx, artifact("aaa", 11, "release"))
	if err != nil || !applied {
		t.Fatal(err)
	}
	if b.ID != a.ID || b.RunID != 11 || !b.OtherWorkflows {
		t.Fatalf("unexpected %+v", b)
	}
	got, err := s.Artifact(ctx, a.ID)
	if err != nil || got.Manifest.Images[0].Name != "app" {
		t.Fatal(err)
	}
}

func TestBranchRunKeepsPullRequestProvenance(t *testing.T) {
	s := storetest.New(t)
	s.UpsertArtifact(ctx, artifact("aaa", 10, "ci"))
	branch := artifact("aaa", 11, "ci")
	branch.Source, branch.RefName, branch.PR = "branch", "feature", nil
	got, applied, err := s.UpsertArtifact(ctx, branch)
	if err != nil || !applied || got.Source != "pr" || got.PR == nil || *got.PR != 7 || got.RunID != 11 {
		t.Fatalf("got %+v applied=%v err=%v", got, applied, err)
	}
	release := artifact("aaa", 12, "release")
	release.Source, release.RefName, release.PR = "release", "v1.0.0", nil
	got, _, _ = s.UpsertArtifact(ctx, release)
	if got.Source != "release" || got.RefName != "v1.0.0" {
		t.Fatalf("release run did not win: %+v", got)
	}
}

func TestDeploymentSlots(t *testing.T) {
	s := storetest.New(t)
	a, _, _ := s.UpsertArtifact(ctx, artifact("aaa", 1, "ci"))
	d1, err := s.InsertDeployment(ctx, &store.Deployment{ArtifactID: &a.ID, TargetID: "prev", Slot: "1", Actor: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.InsertDeployment(ctx, &store.Deployment{ArtifactID: &a.ID, TargetID: "prev", Slot: "1", Actor: "x"}); !errors.Is(err, store.ErrSlotBusy) {
		t.Fatalf("same slot: %v", err)
	}
	if _, err := s.InsertDeployment(ctx, &store.Deployment{ArtifactID: &a.ID, TargetID: "prev", Slot: "2", Actor: "x"}); err != nil {
		t.Fatalf("other slot blocked: %v", err)
	}
	s.MarkRunning(ctx, d1.ID, "job-1")
	s.Finish(ctx, d1.ID, store.StatusSucceeded, "log", "")
	last, err := s.LastSucceeded(ctx, "prev", "1")
	if err != nil || last.ID != d1.ID || *last.Log != "log" {
		t.Fatal(err)
	}
	if _, err := s.InsertDeployment(ctx, &store.Deployment{ArtifactID: &a.ID, TargetID: "prev", Slot: "1", Actor: "x"}); err != nil {
		t.Fatalf("slot not freed: %v", err)
	}
}

func TestQueueNewestWins(t *testing.T) {
	s := storetest.New(t)
	a, _, _ := s.UpsertArtifact(ctx, artifact("aaa", 1, "ci"))
	b, _, _ := s.UpsertArtifact(ctx, artifact("bbb", 2, "ci"))
	busy, _ := s.InsertDeployment(ctx, &store.Deployment{ArtifactID: &a.ID, TargetID: "t", Slot: "1", Actor: "x"})
	s.Enqueue(ctx, store.QueueEntry{TargetID: "t", Slot: "1", ArtifactID: a.ID})
	s.Enqueue(ctx, store.QueueEntry{TargetID: "t", Slot: "1", ArtifactID: b.ID})
	if ready, _ := s.QueuedReady(ctx); len(ready) != 0 {
		t.Fatal("queue ready while slot busy")
	}
	s.Finish(ctx, busy.ID, store.StatusFailed, "", "boom")
	ready, err := s.QueuedReady(ctx)
	if err != nil || len(ready) != 1 || ready[0].ArtifactID != b.ID {
		t.Fatalf("ready = %+v, %v", ready, err)
	}
}

func TestTargetStateAndPreviews(t *testing.T) {
	s := storetest.New(t)
	err := s.WithTx(ctx, func(tx *store.Store) error {
		st, err := tx.LockTargetState(ctx, "t")
		if err != nil {
			return err
		}
		until := time.Now().Add(time.Hour)
		st.LockHolder, st.LockUntil = "alice", &until
		return tx.SaveTargetState(ctx, st)
	})
	if err != nil {
		t.Fatal(err)
	}
	states, _ := s.TargetStates(ctx)
	if !states["t"].Locked(time.Now()) || states["t"].Locked(time.Now().Add(2*time.Hour)) {
		t.Fatal("lock state wrong")
	}

	exp := time.Now().Add(time.Hour)
	if err := s.TouchPreview(ctx, "org/app", 7, "prev", "pr-app-7", exp); err != nil {
		t.Fatal(err)
	}
	if err := s.TouchPreview(ctx, "org/app", 7, "prev", "pr-app-7", exp.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	live, _ := s.LivePreviews(ctx, "org/app")
	if len(live) != 1 || !live[0].ExpiresAt.After(exp) {
		t.Fatalf("live = %+v", live)
	}
	expired, _ := s.ExpiredPreviews(ctx, exp.Add(3*time.Hour))
	if len(expired) != 1 {
		t.Fatal("expired not found")
	}
}

func TestDeleteOldArtifactsKeepsDeployed(t *testing.T) {
	s := storetest.New(t)
	old, _, _ := s.UpsertArtifact(ctx, artifact("old", 1, "ci"))
	deployed, _, _ := s.UpsertArtifact(ctx, artifact("dep", 2, "ci"))
	d, _ := s.InsertDeployment(ctx, &store.Deployment{ArtifactID: &deployed.ID, TargetID: "t", Actor: "x"})
	s.Finish(ctx, d.ID, store.StatusSucceeded, "", "")
	s.MarkPRClosed(ctx, "org/app", 7)
	n, err := s.DeleteOldArtifacts(ctx, time.Now().Add(time.Minute), 50)
	if err != nil || n != 1 {
		t.Fatalf("deleted %d, %v", n, err)
	}
	if _, err := s.Artifact(ctx, old.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("old artifact kept")
	}
	if _, err := s.Artifact(ctx, deployed.ID); err != nil {
		t.Fatal("deployed artifact deleted")
	}
}

func TestSessionsAndAudit(t *testing.T) {
	s := storetest.New(t)
	s.CreateSession(ctx, store.Session{IDHash: "h", Subject: "s", Username: "alice", Roles: []string{"platform-admin"}, ExpiresAt: time.Now().Add(time.Hour)})
	got, err := s.Session(ctx, "h")
	if err != nil || got.Roles[0] != "platform-admin" {
		t.Fatal(err)
	}
	s.Audit(ctx, "alice", "deploy", "t", map[string]any{"artifact": 1})
	ev, _ := s.AuditEvents(ctx, 0, 10)
	if len(ev) != 1 || ev[0].Actor != "alice" {
		t.Fatal("audit missing")
	}
}

func TestChangeEvents(t *testing.T) {
	s := storetest.New(t)
	lctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	got := make(chan store.Event, 100)
	go s.Listen(lctx, func(e store.Event) { got <- e })
	// The channel is shared by the whole database, so look for this test's project only.
	repo := fmt.Sprintf("org/events-%d", time.Now().UnixNano())
	wait := func(what string, send func()) {
		t.Helper()
		tick := time.NewTicker(200 * time.Millisecond)
		defer tick.Stop()
		send()
		for {
			select {
			case e := <-got:
				if e.Project == repo && e.Kind == store.EventArtifacts {
					return
				}
			case <-tick.C:
				send() // LISTEN may not have been active yet
			case <-lctx.Done():
				t.Fatalf("no event after %s", what)
			}
		}
	}
	pr, run := 3, int64(0)
	wait("upsert", func() {
		run++
		if _, _, err := s.UpsertArtifact(ctx, &store.Artifact{Repo: repo, SHA: "abc", Source: "pr", PR: &pr, RunID: run}); err != nil {
			t.Fatal(err)
		}
	})
	wait("pr closed", func() {
		if err := s.MarkPRClosed(ctx, repo, pr); err != nil {
			t.Fatal(err)
		}
	})
}
