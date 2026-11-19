// Command seed fills a development database with demo artifacts and history,
// so the UI has something to show without a GitHub App. Development only.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/Mtze/simple-cd/internal/repoconfig"
	"github.com/Mtze/simple-cd/internal/store"
)

const repo = "example/demo"

func digest(n int) string { return fmt.Sprintf("sha256:%064x", n) }

func str(s string) *string { return &s }

func main() {
	ctx := context.Background()
	st, err := store.Open(ctx, os.Getenv("SIMPLECD_DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		log.Fatal(err)
	}
	compose := "services:\n  app:\n    image: ${APP_IMAGE}\n    ports: [\"80:8080\"]\n"
	rc := &repoconfig.RepoConfig{
		Helm: &repoconfig.HelmConfig{
			Chart: "oci://ghcr.io/example/charts/demo",
			Set:   map[string]string{"image.repository": "{images.app.repository}", "image.digest": "{images.app.digest}"},
			Targets: map[string]repoconfig.HelmTarget{
				"demo-test":     {Values: "deploy/values-test.yaml"},
				"demo-previews": {Set: map[string]string{"ingress.host": "pr-{cd.pr}.demo.preview.example.org"}},
			},
		},
		Compose: &repoconfig.ComposeConfig{Path: "deploy/docker-compose.yml"},
	}
	add := func(n int, sha, source, ref string, pr *int, values string, auto []string) *store.Artifact {
		a, _, err := st.UpsertArtifact(ctx, &store.Artifact{
			Repo: repo, SHA: sha, Source: source, RefName: ref, PR: pr, RunID: int64(1000 + n),
			RunURL: "https://github.com/example/demo/actions/runs/" + fmt.Sprint(1000+n), Workflow: ".github/workflows/ci.yml",
			TriggeringActor: "octocat",
			Manifest: repoconfig.Manifest{Version: 1,
				Images:     []repoconfig.Image{{Name: "app", Ref: "ghcr.io/example/demo:" + ref + "@" + digest(n)}},
				Helm:       &repoconfig.ManifestHelm{Version: "1.4." + fmt.Sprint(n)},
				AutoDeploy: auto},
			RepoConfig: rc,
			Files:      map[string]*string{"deploy/docker-compose.yml": &compose, "deploy/values-test.yaml": str(values)},
		})
		if err != nil {
			log.Fatal(err)
		}
		return a
	}
	pr12, pr15 := 12, 15
	v1 := add(1, "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b", "release", "v1.0.0", nil, "replicas: 2\n", nil)
	main1 := add(2, "2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c", "branch", "main", nil, "replicas: 2\n", nil)
	add(3, "3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d", "branch", "main", nil, "replicas: 3\nresources:\n  cpu: 500m\n", nil)
	add(4, "4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e", "pr", "feature/login", &pr12, "replicas: 2\n", []string{"demo-previews"})
	broken := add(5, "5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f", "pr", "fix/typo", &pr15, "replicas: 2\n", nil)
	st.SetArtifactReasons(ctx, broken.ID, []string{"image missing in registry: ghcr.io/example/demo@" + digest(5)})

	history := []struct {
		a      *store.Artifact
		target string
		status string
		log    string
	}{
		{v1, "demo-prod", store.StatusSucceeded, "==> Reading SSH credentials\n==> Uploading docker-compose.yml to vm1.example.org:/opt/demo\n==> docker compose pull && up -d on vm1.example.org\n==> Done\n"},
		{main1, "demo-test", store.StatusFailed, "==> Logging in to Vault\n==> helm upgrade --install demo\nError: UPGRADE FAILED: context deadline exceeded\n"},
		{main1, "demo-test", store.StatusSucceeded, "==> Logging in to Vault\n==> helm upgrade --install demo\nRelease \"demo\" has been upgraded. Happy Helming!\n==> Done\n"},
	}
	for _, h := range history {
		d, err := st.InsertDeployment(ctx, &store.Deployment{ArtifactID: &h.a.ID, TargetID: h.target, Actor: "mia", Snapshot: "set: {}\nvalues:\n  replicas: 2\n"})
		if err != nil {
			log.Fatal(err)
		}
		st.MarkRunning(ctx, d.ID, "seed")
		st.Finish(ctx, d.ID, h.status, h.log, map[bool]string{true: "", false: "helm exited with status 1"}[h.status == store.StatusSucceeded])
	}
	st.TouchPreview(ctx, repo, 12, "demo-previews", "pr-demo-12", time.Now().Add(30*time.Hour))
	st.Audit(ctx, "seed", "dev.seed", repo, nil)
	fmt.Println("seeded", repo)
}
