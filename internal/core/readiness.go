package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Mtze/simple-cd/internal/config"
	"github.com/Mtze/simple-cd/internal/executor"
	"github.com/Mtze/simple-cd/internal/repoconfig"
	"github.com/Mtze/simple-cd/internal/store"
	"go.yaml.in/yaml/v3"
)

// Target statuses, in order of precedence.
const (
	StatusMaintenance = "maintenance"
	StatusDeploying   = "deploying"
	StatusLocked      = "locked"
	StatusAvailable   = "available"
)

// TargetStatus picks the single status shown for a target. Preview targets
// are never "deploying" as a whole: each PR has its own slot.
func TargetStatus(t config.Target, st store.TargetState, active []*store.Deployment, now time.Time) string {
	switch {
	case st.Maintenance:
		return StatusMaintenance
	case !t.Preview && len(active) > 0:
		return StatusDeploying
	case t.Lockable && st.Locked(now):
		return StatusLocked
	}
	return StatusAvailable
}

// Slot returns the deploy slot of an artifact on a target.
func Slot(t config.Target, a *store.Artifact) string {
	if t.Preview && a != nil && a.PR != nil {
		return strconv.Itoa(*a.PR)
	}
	return ""
}

var nonName = regexp.MustCompile(`[^a-z0-9-]+`)

// PreviewNamespace builds "pr-<repo>-<pr>", shortened with a hash so it
// stays unique and within the 63 character limit.
func PreviewNamespace(repo string, pr int) string {
	_, name, _ := strings.Cut(strings.ToLower(repo), "/")
	name = strings.Trim(nonName.ReplaceAllString(name, "-"), "-")
	suffix := "-" + strconv.Itoa(pr)
	ns := "pr-" + name + suffix
	if len(ns) <= 63 {
		return ns
	}
	sum := sha256.Sum256([]byte(strings.ToLower(repo)))
	hash := hex.EncodeToString(sum[:])[:6]
	room := 63 - len("pr-") - len(suffix) - len(hash) - 1
	return "pr-" + strings.TrimRight(name[:room], "-") + "-" + hash + suffix
}

// PreviewLabels mark namespaces we created, so teardown never deletes foreign ones.
func PreviewLabels(t config.Target, pr int) map[string]string {
	return map[string]string{"cd/managed": "true", "cd/target": t.ID, "cd/pr": strconv.Itoa(pr)}
}

func namespaceFor(t config.Target, a *store.Artifact) string {
	if t.Preview && a.PR != nil {
		return PreviewNamespace(a.Repo, *a.PR)
	}
	return t.Namespace
}

func placeholdersFor(t config.Target, a *store.Artifact) map[string]string {
	pr := ""
	if t.Preview && a.PR != nil {
		pr = strconv.Itoa(*a.PR)
	}
	return repoconfig.Placeholders(&a.Manifest, a.SHA, pr, t.ID, namespaceFor(t, a))
}

// Readiness lists why an artifact cannot go to a target. Empty means deployable.
// It uses only stored data, so it is cheap to compute for every target.
func Readiness(cfg *config.Config, t config.Target, a *store.Artifact) []string {
	reasons := append([]string{}, a.Reasons...)
	add := func(format string, args ...any) { reasons = append(reasons, fmt.Sprintf(format, args...)) }

	if !strings.EqualFold(a.Repo, t.Project) {
		add("artifact belongs to %s, target to %s", a.Repo, t.Project)
	}
	prefixes := cfg.Registries(t.Project)
	for _, img := range a.Manifest.Images {
		r, err := repoconfig.ParseImageRef(img.Ref)
		if err == nil && !repoconfig.AllowedBy(r.Repository, prefixes) {
			add("image %s is outside the allowed registries", r.Repository)
		}
	}
	rc := a.RepoConfig
	if t.Preview && (a.Source != config.SourcePR || a.PR == nil) {
		add("preview targets only accept pull request artifacts")
	}
	switch t.Type {
	case config.TypeHelm:
		if rc == nil || rc.Helm == nil {
			add("%s has no helm section", repoconfig.FileName)
			break
		}
		if !repoconfig.AllowedBy(repoconfig.ChartRef(rc.Helm.Chart), prefixes) {
			add("chart %s is outside the allowed registries", rc.Helm.Chart)
		}
		if a.Manifest.Helm == nil {
			add("deploy.json has no helm version")
		}
		if p := rc.Helm.ValuesFile(t.ID); p != "" && a.Files[p] == nil {
			add("values file %s does not exist at this commit", p)
		}
		if _, missing := repoconfig.RenderSet(rc.Helm.SetFor(t.ID), placeholdersFor(t, a)); len(missing) > 0 {
			add("placeholders without a value for this target: %s", strings.Join(missing, ", "))
		}
	case config.TypeCompose:
		if rc == nil || rc.Compose == nil {
			add("%s has no compose section", repoconfig.FileName)
			break
		}
		content := a.Files[rc.Compose.Path]
		if content == nil {
			add("compose file %s does not exist at this commit", rc.Compose.Path)
			break
		}
		reasons = append(reasons, repoconfig.CheckCompose([]byte(*content), a.Manifest.Images)...)
	case config.TypeAnsible:
		if t.Ansible != nil && t.Ansible.Source == "repo" {
			if rc == nil || rc.Ansible == nil {
				add("%s has no ansible section", repoconfig.FileName)
			} else if a.Files[rc.Ansible.Playbook] == nil {
				add("playbook %s does not exist at this commit", rc.Ansible.Playbook)
			}
		}
	}
	return reasons
}

// helmValues returns the non-secret values: repo values file merged with inline target values.
func helmValues(t config.Target, a *store.Artifact) (map[string]any, error) {
	var layers []map[string]any
	if p := a.RepoConfig.Helm.ValuesFile(t.ID); p != "" && a.Files[p] != nil {
		v, err := repoconfig.ParseValues([]byte(*a.Files[p]))
		if err != nil {
			return nil, fmt.Errorf("values file %s: %w", p, err)
		}
		layers = append(layers, v)
	}
	if t.Values.Inline != nil {
		layers = append(layers, t.Values.Inline)
	}
	return repoconfig.Merge(layers...), nil
}

// Snapshot is what the confirmation diff compares: the compose file, or the
// non-secret Helm values plus the unrendered set mapping. Images are left out
// on purpose, so a normal image update needs no confirmation.
func Snapshot(t config.Target, a *store.Artifact) (string, error) {
	switch t.Type {
	case config.TypeCompose:
		if p := a.Files[a.RepoConfig.Compose.Path]; p != nil {
			return *p, nil
		}
	case config.TypeHelm:
		values, err := helmValues(t, a)
		if err != nil {
			return "", err
		}
		var sb strings.Builder
		enc := yaml.NewEncoder(&sb)
		enc.SetIndent(2)
		err = enc.Encode(map[string]any{"values": values, "set": a.RepoConfig.Helm.SetFor(t.ID)})
		return sb.String(), err
	}
	return "", nil
}

// BuildSpec turns a deployment into the executor's input.
func BuildSpec(cfg *config.Config, t config.Target, a *store.Artifact, d *store.Deployment, repoURL string) (executor.Spec, error) {
	spec := executor.Spec{DeploymentID: d.ID, Kind: d.Kind, Type: t.Type, TargetID: t.ID, Repo: t.Project}
	vault := func(ref string) string { return strings.TrimPrefix(ref, "vault:") }

	if d.Kind == store.KindTeardown {
		pr, err := strconv.Atoi(d.Slot)
		if err != nil || !t.Preview {
			return spec, fmt.Errorf("teardown is only possible for preview slots")
		}
		spec.Helm = &executor.HelmSpec{
			ClusterSecret: vault(t.Cluster), Namespace: PreviewNamespace(t.Project, pr),
			Release: t.Release, Preview: true, Labels: PreviewLabels(t, pr),
		}
		return spec, nil
	}
	if a == nil {
		return spec, fmt.Errorf("deployment %d has no artifact", d.ID)
	}
	spec.SHA = a.SHA
	switch t.Type {
	case config.TypeHelm:
		values, err := helmValues(t, a)
		if err != nil {
			return spec, err
		}
		set, missing := repoconfig.RenderSet(a.RepoConfig.Helm.SetFor(t.ID), placeholdersFor(t, a))
		if len(missing) > 0 {
			return spec, fmt.Errorf("placeholders without a value: %s", strings.Join(missing, ", "))
		}
		h := &executor.HelmSpec{
			ClusterSecret: vault(t.Cluster), Namespace: namespaceFor(t, a), Release: t.Release,
			Chart: a.RepoConfig.Helm.Chart, Version: a.Manifest.Helm.Version,
			Values: values, ValuesSecret: vault(t.Values.Vault), Set: set,
			RegistrySecret: vault(t.RegistryCredentials),
			Timeout:        helmTimeout(cfg.JobTimeout(t)).String(),
		}
		if t.Preview && a.PR != nil {
			h.Preview = true
			h.Labels = PreviewLabels(t, *a.PR)
		}
		spec.Helm = h
	case config.TypeCompose:
		env := map[string]string{}
		for _, img := range a.Manifest.Images {
			r, err := repoconfig.ParseImageRef(img.Ref)
			if err != nil {
				return spec, err
			}
			env[repoconfig.EnvVarName(img.Name)] = r.String()
		}
		content := a.Files[a.RepoConfig.Compose.Path]
		if content == nil {
			return spec, fmt.Errorf("compose file missing")
		}
		spec.Compose = &executor.ComposeSpec{Host: t.Host, Dir: t.Dir, SSHSecret: vault(t.SSH), ComposeFile: *content, Env: env}
	case config.TypeAnsible:
		an := t.Ansible
		images := map[string]any{}
		for _, img := range a.Manifest.Images {
			images[img.Name] = img.Ref
		}
		s := &executor.AnsibleSpec{
			RepoURL: an.Source, Ref: an.Ref, Playbook: an.Playbook, Inventory: an.Inventory,
			CredentialsSecret: vault(an.Credentials),
			ExtraVars:         map[string]any{"cd_images": images, "cd_sha": a.SHA, "cd_target": t.ID, "cd_repo": a.Repo},
		}
		if an.Source == "repo" {
			s.RepoURL, s.Ref, s.Playbook = repoURL, a.SHA, a.RepoConfig.Ansible.Playbook
		}
		spec.Ansible = s
	}
	return spec, nil
}

// helmTimeout leaves the executor time to report a helm timeout before
// Kubernetes kills the job at its deadline.
func helmTimeout(job time.Duration) time.Duration {
	if job > 4*time.Minute {
		return job - 2*time.Minute
	}
	return job * 3 / 4
}
