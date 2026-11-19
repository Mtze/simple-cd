package repoconfig

import (
	"reflect"
	"strings"
	"testing"
)

const digest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestParseRepoConfig(t *testing.T) {
	c, err := ParseRepoConfig([]byte(`
helm:
  chart: oci://ghcr.io/org/charts/app
  set:
    server.image: "{images.server.ref}"
  targets:
    app-test:
      values: deploy/values-test.yaml
      set:
        server.image: "{images.server.repository}"
        host: test.example.org
compose:
  path: deploy/docker-compose.yml
`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Helm.ValuesFile("app-test") != "deploy/values-test.yaml" || c.Helm.ValuesFile("other") != "" || c.Compose.Path == "" {
		t.Fatalf("unexpected %+v", c)
	}
	if set := c.Helm.SetFor("app-test"); set["server.image"] != "{images.server.repository}" || set["host"] != "test.example.org" {
		t.Fatalf("SetFor = %v", set)
	}
	if set := c.Helm.SetFor("other"); len(set) != 1 || c.Helm.Set["server.image"] != "{images.server.ref}" {
		t.Fatalf("SetFor other = %v", set)
	}
	for _, bad := range []string{
		"helm: {chart: https://x}",
		"compose: {path: ../../etc/passwd}",
		"compose: {path: /etc/passwd}",
		"helm: {chart: oci://x, targets: {a: {values: ../x}}}",
		"helm: {chart: oci://x, typo: 1}",
	} {
		if _, err := ParseRepoConfig([]byte(bad)); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestParseManifest(t *testing.T) {
	m, err := ParseManifest([]byte(`{"version":1,"images":[{"name":"server","ref":"ghcr.io/org/app:1.2@` + digest + `"}],"helm":{"version":"1.2.0"},"autoDeploy":["app-previews"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if m.AutoDeploy[0] != "app-previews" || m.Helm.Version != "1.2.0" {
		t.Fatalf("unexpected %+v", m)
	}
	bad := map[string]string{
		"tag only":      `{"version":1,"images":[{"name":"a","ref":"ghcr.io/org/app:latest"}]}`,
		"no host":       `{"version":1,"images":[{"name":"a","ref":"org/app@` + digest + `"}]}`,
		"dup":           `{"version":1,"images":[{"name":"a","ref":"ghcr.io/o/a@` + digest + `"},{"name":"a","ref":"ghcr.io/o/a@` + digest + `"}]}`,
		"version":       `{"version":2,"images":[{"name":"a","ref":"ghcr.io/o/a@` + digest + `"}]}`,
		"unknown field": `{"version":1,"repo":"x/y","images":[{"name":"a","ref":"ghcr.io/o/a@` + digest + `"}]}`,
		"empty":         `{"version":1}`,
		"bad name":      `{"version":1,"images":[{"name":"A B","ref":"ghcr.io/o/a@` + digest + `"}]}`,
	}
	for name, js := range bad {
		if _, err := ParseManifest([]byte(js)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestParseImageRef(t *testing.T) {
	r, err := ParseImageRef("localhost:5000/team/app:v1@" + digest)
	if err != nil {
		t.Fatal(err)
	}
	if r.Repository != "localhost:5000/team/app" || r.Tag != "v1" || r.Host() != "localhost:5000" || r.Path() != "team/app" {
		t.Fatalf("unexpected %+v", r)
	}
	if r.String() != "localhost:5000/team/app@"+digest {
		t.Errorf("String = %s", r.String())
	}
}

func TestRenderSet(t *testing.T) {
	m := &Manifest{Images: []Image{{Name: "server", Ref: "ghcr.io/org/server:1.0@" + digest}}}
	ctx := Placeholders(m, "abc", "42", "app-previews", "pr-app-42")
	got, missing := RenderSet(map[string]string{
		"server.image.repository": "{images.server.repository}",
		"server.image.digest":     "{images.server.digest}",
		"ingress.host":            "pr-{cd.pr}.example.org",
	}, ctx)
	if len(missing) != 0 {
		t.Fatalf("missing %v", missing)
	}
	want := map[string]any{
		"server":  map[string]any{"image": map[string]any{"repository": "ghcr.io/org/server", "digest": digest}},
		"ingress": map[string]any{"host": "pr-42.example.org"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}

	ctx = Placeholders(m, "abc", "", "app-test", "app")
	_, missing = RenderSet(map[string]string{"ingress.host": "pr-{cd.pr}.x", "a": "{images.client.ref}"}, ctx)
	if strings.Join(missing, ",") != "images.client.ref,cd.pr" {
		t.Fatalf("missing = %v", missing)
	}
}

func TestMergeOrder(t *testing.T) {
	repo := map[string]any{"replicas": 3, "resources": map[string]any{"cpu": "1", "mem": "1Gi"}, "image": "x"}
	target := map[string]any{"resources": map[string]any{"cpu": "500m"}}
	set := map[string]any{"image": "y"}
	got := Merge(repo, target, set)
	want := map[string]any{"replicas": 3, "resources": map[string]any{"cpu": "500m", "mem": "1Gi"}, "image": "y"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
	if repo["resources"].(map[string]any)["cpu"] != "1" {
		t.Fatal("merge mutated input")
	}
}

func TestCheckCompose(t *testing.T) {
	images := []Image{{Name: "app"}, {Name: "web-ui"}}
	ok := `services:
  app: {image: "${APP_IMAGE}"}
  web: {image: "${WEB_UI_IMAGE:?missing}"}
`
	if r := CheckCompose([]byte(ok), images); len(r) != 0 {
		t.Fatalf("reasons %v", r)
	}
	bad := `services:
  app: {build: ., image: "ghcr.io/org/app:latest"}
`
	r := CheckCompose([]byte(bad), images)
	if len(r) != 3 {
		t.Fatalf("reasons %v", r)
	}
}

func TestEnvVarAndAllowed(t *testing.T) {
	if EnvVarName("web-ui") != "WEB_UI_IMAGE" {
		t.Error(EnvVarName("web-ui"))
	}
	if !AllowedBy("ghcr.io/org/app", []string{"docker.io/", "ghcr.io/org/"}) || AllowedBy("ghcr.io/evil/app", []string{"ghcr.io/org/"}) {
		t.Error("AllowedBy wrong")
	}
	if ChartRef("oci://ghcr.io/org/charts/app") != "ghcr.io/org/charts/app" {
		t.Error("ChartRef wrong")
	}
}
