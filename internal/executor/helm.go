package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Mtze/simple-cd/internal/repoconfig"
	"go.yaml.in/yaml/v3"
)

func (x *executor) helmDeploy(ctx context.Context, h *HelmSpec) error {
	if h.Namespace == "" || h.Release == "" || h.Chart == "" || h.Version == "" {
		return fmt.Errorf("helm spec needs namespace, release, chart and version")
	}
	if err := x.useCluster(ctx, h.ClusterSecret); err != nil {
		return err
	}

	if h.RegistrySecret != "" {
		host, _, _ := strings.Cut(repoconfig.ChartRef(h.Chart), "/")
		x.logf("==> Logging in to registry %s", host)
		data, err := x.vault.read(ctx, h.RegistrySecret)
		if err != nil {
			return err
		}
		user, err := secretString(data, h.RegistrySecret, "username", true)
		if err != nil {
			return err
		}
		pass, err := secretString(data, h.RegistrySecret, "password", true)
		if err != nil {
			return err
		}
		if err := x.run(ctx, "", pass, "helm", "registry", "login", host, "--username", user, "--password-stdin"); err != nil {
			return err
		}
	}

	if h.Preview {
		if err := x.ensurePreviewNamespace(ctx, h); err != nil {
			return err
		}
	}

	x.logf("==> Building values")
	var secretValues map[string]any
	if h.ValuesSecret != "" {
		data, err := x.vault.read(ctx, h.ValuesSecret)
		if err != nil {
			return err
		}
		raw, err := secretString(data, h.ValuesSecret, "values", true)
		if err != nil {
			return err
		}
		if secretValues, err = repoconfig.ParseValues([]byte(raw)); err != nil {
			// The YAML error could quote secret content, so leave it out.
			return fmt.Errorf("vault secret %q: key \"values\" is not valid YAML", h.ValuesSecret)
		}
	}
	values, err := yaml.Marshal(repoconfig.Merge(h.Values, secretValues, h.Set))
	if err != nil {
		return fmt.Errorf("encode values: %w", err)
	}
	valuesFile, err := x.writeSecret("values.yaml", values)
	if err != nil {
		return err
	}

	timeout := h.Timeout
	if timeout == "" {
		timeout = "10m"
	}
	x.logf("==> helm upgrade --install %s %s --version %s --namespace %s", h.Release, h.Chart, h.Version, h.Namespace)
	return x.run(ctx, "", "", "helm", "upgrade", "--install", h.Release, h.Chart,
		"--version", h.Version, "--namespace", h.Namespace,
		"--values", valuesFile, "--wait", "--timeout", timeout)
}

func (x *executor) helmTeardown(ctx context.Context, h *HelmSpec) error {
	if !h.Preview {
		return fmt.Errorf("teardown is only supported for preview namespaces")
	}
	if h.Namespace == "" || h.Release == "" || len(h.Labels) == 0 {
		return fmt.Errorf("teardown needs namespace, release and labels")
	}
	if err := x.useCluster(ctx, h.ClusterSecret); err != nil {
		return err
	}
	x.logf("==> Checking namespace %s", h.Namespace)
	labels, exists, err := x.namespaceLabels(ctx, h.Namespace)
	if err != nil {
		return err
	}
	if !exists {
		x.logf("Namespace %s does not exist, nothing to do", h.Namespace)
		return nil
	}
	if !labelsMatch(labels, h.Labels) {
		return fmt.Errorf("namespace %s exists and is not managed by simple-cd, refusing to delete it", h.Namespace)
	}
	x.logf("==> helm uninstall %s --namespace %s", h.Release, h.Namespace)
	if err := x.run(ctx, "", "", "helm", "uninstall", h.Release, "--namespace", h.Namespace, "--ignore-not-found"); err != nil {
		return err
	}
	x.logf("==> Deleting namespace %s", h.Namespace)
	return x.run(ctx, "", "", "kubectl", "delete", "namespace", h.Namespace, "--wait=false")
}

// useCluster writes the kubeconfig and points helm and kubectl at it.
func (x *executor) useCluster(ctx context.Context, ref string) error {
	x.logf("==> Reading cluster credentials")
	data, err := x.vault.read(ctx, ref)
	if err != nil {
		return err
	}
	kubeconfig, err := secretString(data, ref, "kubeconfig", true)
	if err != nil {
		return err
	}
	p, err := x.writeSecret("kubeconfig", []byte(kubeconfig))
	if err != nil {
		return err
	}
	// Registry logins hold credentials too, so keep them in the secrets dir instead of HOME.
	x.env = append(x.env, "KUBECONFIG="+p, "HELM_REGISTRY_CONFIG="+x.secretsDir+"/registry.json")
	return nil
}

// ensurePreviewNamespace creates the namespace with our labels. An existing
// namespace must already carry them: we never adopt a foreign namespace.
func (x *executor) ensurePreviewNamespace(ctx context.Context, h *HelmSpec) error {
	if len(h.Labels) == 0 {
		return fmt.Errorf("preview deploy needs namespace labels")
	}
	x.logf("==> Ensuring preview namespace %s", h.Namespace)
	labels, exists, err := x.namespaceLabels(ctx, h.Namespace)
	if err != nil {
		return err
	}
	if exists {
		if !labelsMatch(labels, h.Labels) {
			return fmt.Errorf("namespace %s exists and is not managed by simple-cd", h.Namespace)
		}
		return nil
	}
	manifest, _ := json.Marshal(map[string]any{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata":   map[string]any{"name": h.Namespace, "labels": h.Labels},
	})
	// create, not apply: if someone else created it in the meantime we fail instead of adopting it.
	return x.run(ctx, "", string(manifest), "kubectl", "create", "-f", "-")
}

func (x *executor) namespaceLabels(ctx context.Context, ns string) (map[string]string, bool, error) {
	out, err := x.output(ctx, "kubectl", "get", "namespace", ns, "-o", "json", "--ignore-not-found")
	if err != nil {
		return nil, false, err
	}
	if len(strings.TrimSpace(string(out))) == 0 {
		return nil, false, nil
	}
	var obj struct {
		Metadata struct {
			Labels map[string]string `json:"labels"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(out, &obj); err != nil {
		return nil, false, fmt.Errorf("parse namespace %s: %w", ns, err)
	}
	return obj.Metadata.Labels, true, nil
}

func labelsMatch(have, want map[string]string) bool {
	for k, v := range want {
		if got, ok := have[k]; !ok || got != v {
			return false
		}
	}
	return true
}
