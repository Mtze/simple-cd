// Package repoconfig handles what a repository contributes: the static
// .cd.yaml and the per-build deploy.json. It also holds the pure parts of the
// completeness check and the Helm values merge, so server and executor share
// one implementation.
package repoconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// FileName is where the server looks for the repo config at a given SHA.
const FileName = ".cd.yaml"

// ArtifactName is the workflow artifact that carries deploy.json.
const ArtifactName = "cd-deploy"

type RepoConfig struct {
	Helm    *HelmConfig    `yaml:"helm" json:"helm,omitempty"`
	Compose *ComposeConfig `yaml:"compose" json:"compose,omitempty"`
	Ansible *AnsibleConfig `yaml:"ansible" json:"ansible,omitempty"`
}

type HelmConfig struct {
	Chart   string                `yaml:"chart" json:"chart"`
	Set     map[string]string     `yaml:"set" json:"set,omitempty"`         // for every helm target
	Targets map[string]HelmTarget `yaml:"targets" json:"targets,omitempty"` // per target id
}

// HelmTarget holds what only applies to one target.
type HelmTarget struct {
	Values string            `yaml:"values" json:"values,omitempty"` // values file in the repo
	Set    map[string]string `yaml:"set" json:"set,omitempty"`       // added to (and overriding) helm.set
}

// SetFor returns helm.set merged with the target's own set entries.
func (h *HelmConfig) SetFor(target string) map[string]string {
	out := make(map[string]string, len(h.Set))
	for k, v := range h.Set {
		out[k] = v
	}
	for k, v := range h.Targets[target].Set {
		out[k] = v
	}
	return out
}

// ValuesFile returns the target's values file path, or "".
func (h *HelmConfig) ValuesFile(target string) string { return h.Targets[target].Values }

type ComposeConfig struct {
	Path string `yaml:"path" json:"path"`
}

type AnsibleConfig struct {
	Playbook string `yaml:"playbook" json:"playbook"`
}

// ParseRepoConfig decodes .cd.yaml strictly.
func ParseRepoConfig(data []byte) (*RepoConfig, error) {
	var c RepoConfig
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("%s: %w", FileName, err)
	}
	var errs []error
	if h := c.Helm; h != nil {
		if !strings.HasPrefix(h.Chart, "oci://") {
			errs = append(errs, errors.New("helm.chart must be an oci:// reference"))
		}
		for id, t := range h.Targets {
			if t.Values != "" && !safeRepoPath(t.Values) {
				errs = append(errs, fmt.Errorf("helm.targets.%s.values: invalid path %q", id, t.Values))
			}
		}
	}
	if c.Compose != nil && !safeRepoPath(c.Compose.Path) {
		errs = append(errs, fmt.Errorf("compose.path: invalid path %q", c.Compose.Path))
	}
	if c.Ansible != nil && !safeRepoPath(c.Ansible.Playbook) {
		errs = append(errs, fmt.Errorf("ansible.playbook: invalid path %q", c.Ansible.Playbook))
	}
	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("%s: %w", FileName, err)
	}
	return &c, nil
}

// safeRepoPath accepts relative paths that stay inside the repository.
func safeRepoPath(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") {
		return false
	}
	clean := path.Clean(p)
	return clean != ".." && !strings.HasPrefix(clean, "../")
}

// Manifest is deploy.json: only build results. Provenance (repo, sha, source,
// PR) is derived from GitHub, never taken from here.
type Manifest struct {
	Version    int           `json:"version"`
	Images     []Image       `json:"images"`
	Helm       *ManifestHelm `json:"helm,omitempty"`
	AutoDeploy []string      `json:"autoDeploy,omitempty"`
}

type ManifestHelm struct {
	Version string `json:"version"`
}

type Image struct {
	Name string `json:"name"`
	Ref  string `json:"ref"`
}

var imageName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)

// ParseManifest decodes and validates deploy.json.
func ParseManifest(data []byte) (*Manifest, error) {
	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("deploy.json: %w", err)
	}
	var errs []error
	if m.Version != 1 {
		errs = append(errs, fmt.Errorf("unsupported version %d", m.Version))
	}
	if len(m.Images) == 0 && m.Helm == nil {
		errs = append(errs, errors.New("no images and no helm chart"))
	}
	seen := map[string]bool{}
	for _, img := range m.Images {
		if !imageName.MatchString(img.Name) {
			errs = append(errs, fmt.Errorf("image name %q: lowercase letters, digits, - and _", img.Name))
		}
		if seen[img.Name] {
			errs = append(errs, fmt.Errorf("duplicate image name %q", img.Name))
		}
		seen[img.Name] = true
		if _, err := ParseImageRef(img.Ref); err != nil {
			errs = append(errs, fmt.Errorf("image %q: %w", img.Name, err))
		}
	}
	if m.Helm != nil && m.Helm.Version == "" {
		errs = append(errs, errors.New("helm.version is empty"))
	}
	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("deploy.json: %w", err)
	}
	return &m, nil
}

// ImageRef is a parsed "registry/repo[:tag]@sha256:...".
type ImageRef struct {
	Repository string // registry host plus path, e.g. ghcr.io/org/app
	Tag        string
	Digest     string // sha256:...
}

func (r ImageRef) String() string { return r.Repository + "@" + r.Digest }

// Host returns the registry host.
func (r ImageRef) Host() string { h, _, _ := strings.Cut(r.Repository, "/"); return h }

// Path returns the repository path without the host.
func (r ImageRef) Path() string { _, p, _ := strings.Cut(r.Repository, "/"); return p }

var digestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

// ParseImageRef requires a digest: tags can move, digests cannot.
func ParseImageRef(s string) (ImageRef, error) {
	name, digest, ok := strings.Cut(s, "@")
	if !ok || !digestPattern.MatchString(digest) {
		return ImageRef{}, fmt.Errorf("%q must be pinned by a sha256 digest", s)
	}
	var tag string
	if i := strings.LastIndex(name, ":"); i > strings.LastIndex(name, "/") {
		name, tag = name[:i], name[i+1:]
	}
	// Docker's rule: the first segment is a host if it has a dot or port, or is localhost.
	host, _, hasPath := strings.Cut(name, "/")
	if !hasPath || !(strings.ContainsAny(host, ".:") || host == "localhost") {
		return ImageRef{}, fmt.Errorf("%q must include the registry host", s)
	}
	return ImageRef{Repository: name, Tag: tag, Digest: digest}, nil
}

// EnvVarName is the compose variable for an image: "server" -> "SERVER_IMAGE".
func EnvVarName(image string) string {
	return strings.ToUpper(strings.ReplaceAll(image, "-", "_")) + "_IMAGE"
}

// ChartRef turns "oci://ghcr.io/org/charts/app" plus a version into a
// registry reference for the existence check.
func ChartRef(chart string) (repository string) {
	return strings.TrimPrefix(chart, "oci://")
}

// AllowedBy reports whether a repository (host/path) is under one of the prefixes.
func AllowedBy(repository string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(repository, p) {
			return true
		}
	}
	return false
}

// Placeholders builds the substitution context for one deploy.
func Placeholders(m *Manifest, sha, pr, target, namespace string) map[string]string {
	ctx := map[string]string{
		"cd.sha": sha, "cd.pr": pr, "cd.target": target, "cd.namespace": namespace,
	}
	for _, img := range m.Images {
		r, err := ParseImageRef(img.Ref)
		if err != nil {
			continue
		}
		p := "images." + img.Name + "."
		ctx[p+"ref"] = r.String()
		ctx[p+"repository"] = r.Repository
		ctx[p+"digest"] = r.Digest
		ctx[p+"tag"] = r.Tag
	}
	return ctx
}

var placeholder = regexp.MustCompile(`\{([a-zA-Z0-9_.-]+)\}`)

// Render replaces {placeholders}. Unknown or empty placeholders are reported
// as missing, so a deploy never goes out with an empty host or image.
func Render(tmpl string, ctx map[string]string) (string, []string) {
	var missing []string
	out := placeholder.ReplaceAllStringFunc(tmpl, func(m string) string {
		key := m[1 : len(m)-1]
		v := ctx[key]
		if v == "" {
			missing = append(missing, key)
		}
		return v
	})
	return out, missing
}

// RenderSet renders all helm.set entries into a nested values map.
func RenderSet(set map[string]string, ctx map[string]string) (map[string]any, []string) {
	out := map[string]any{}
	var missing []string
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v, miss := Render(set[k], ctx)
		missing = append(missing, miss...)
		setPath(out, strings.Split(k, "."), v)
	}
	return out, missing
}

func setPath(m map[string]any, keys []string, v any) {
	for _, k := range keys[:len(keys)-1] {
		next, ok := m[k].(map[string]any)
		if !ok {
			next = map[string]any{}
			m[k] = next
		}
		m = next
	}
	m[keys[len(keys)-1]] = v
}

// Merge deep-merges layers; later layers win. Maps merge, everything else replaces.
func Merge(layers ...map[string]any) map[string]any {
	out := map[string]any{}
	for _, l := range layers {
		mergeInto(out, l)
	}
	return out
}

func mergeInto(dst, src map[string]any) {
	for k, v := range src {
		if sm, ok := asMap(v); ok {
			if dm, ok := asMap(dst[k]); ok {
				merged := map[string]any{}
				mergeInto(merged, dm)
				mergeInto(merged, sm)
				dst[k] = merged
				continue
			}
			cp := map[string]any{}
			mergeInto(cp, sm)
			dst[k] = cp
			continue
		}
		dst[k] = v
	}
}

func asMap(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case map[string]any:
		return m, true
	case map[any]any:
		out := make(map[string]any, len(m))
		for k, val := range m {
			out[fmt.Sprint(k)] = val
		}
		return out, true
	}
	return nil, false
}

// ParseValues decodes a values YAML document into a map.
func ParseValues(data []byte) (map[string]any, error) {
	out := map[string]any{}
	if err := yaml.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CheckCompose returns reasons why a compose file cannot be deployed:
// build sections and images not taken from the CD variables.
func CheckCompose(data []byte, images []Image) []string {
	var doc struct {
		Services map[string]map[string]any `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return []string{"compose file is not valid YAML: " + err.Error()}
	}
	var reasons []string
	names := make([]string, 0, len(doc.Services))
	for n := range doc.Services {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if _, ok := doc.Services[n]["build"]; ok {
			reasons = append(reasons, fmt.Sprintf("service %q uses build:, the VM must not build images", n))
		}
	}
	text := string(data)
	for _, img := range images {
		v := EnvVarName(img.Name)
		if !strings.Contains(text, "${"+v+"}") && !strings.Contains(text, "${"+v+":") {
			reasons = append(reasons, fmt.Sprintf("compose file does not use ${%s} for image %q", v, img.Name))
		}
	}
	return reasons
}
