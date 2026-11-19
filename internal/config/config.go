// Package config loads and validates the target configuration: the YAML file
// that lists projects and deployment targets. It describes what a target is,
// never what state it is in (that lives in Postgres).
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// Target types.
const (
	TypeHelm    = "helm"
	TypeCompose = "compose"
	TypeAnsible = "ansible"
)

// Artifact sources, derived by the server from GitHub.
const (
	SourcePR      = "pr"
	SourceBranch  = "branch"
	SourceRelease = "release"
)

// Duration accepts Go durations plus a "d" suffix for days ("90d").
type Duration time.Duration

func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	v, err := ParseDuration(n.Value)
	if err != nil {
		return fmt.Errorf("line %d: %w", n.Line, err)
	}
	*d = Duration(v)
	return nil
}

func (d Duration) D() time.Duration { return time.Duration(d) }

// ParseDuration parses "90d", "1h", "30m" and so on.
func ParseDuration(s string) (time.Duration, error) {
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q", s)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q", s)
	}
	return v, nil
}

type Config struct {
	Defaults Defaults           `yaml:"defaults"`
	Projects map[string]Project `yaml:"projects"`
	Targets  []Target           `yaml:"targets"`
}

type Defaults struct {
	LockDuration        Duration `yaml:"lockDuration"`
	JobTimeout          Duration `yaml:"jobTimeout"`
	PreviewTTL          Duration `yaml:"previewTTL"`
	LogRetention        Duration `yaml:"logRetention"`
	AuditRetention      Duration `yaml:"auditRetention"`
	ArtifactRetention   Duration `yaml:"artifactRetention"`
	KeepBranchArtifacts int      `yaml:"keepBranchArtifacts"`
	Registries          []string `yaml:"registries"`
}

type Project struct {
	Registries []string `yaml:"registries"`
}

type Target struct {
	ID           string      `yaml:"id"`
	Project      string      `yaml:"project"`
	Type         string      `yaml:"type"`
	Role         string      `yaml:"role"`
	Lockable     bool        `yaml:"lockable"`
	LockDuration *Duration   `yaml:"lockDuration"`
	JobTimeout   *Duration   `yaml:"jobTimeout"`
	AutoDeploy   *AutoDeploy `yaml:"autoDeploy"`

	// helm
	Cluster             string    `yaml:"cluster"`
	Namespace           string    `yaml:"namespace"`
	Release             string    `yaml:"release"`
	Values              ValuesRef `yaml:"values"`
	RegistryCredentials string    `yaml:"registryCredentials"`
	Preview             bool      `yaml:"preview"`
	PreviewTTL          *Duration `yaml:"previewTTL"`

	// compose
	Host string `yaml:"host"`
	SSH  string `yaml:"ssh"`
	Dir  string `yaml:"dir"`

	// ansible
	Ansible *AnsibleTarget `yaml:"ansible"`
}

type AutoDeploy struct {
	From     []string `yaml:"from"`
	Branches []string `yaml:"branches"`
}

type AnsibleTarget struct {
	Source      string `yaml:"source"` // "repo" or a git URL
	Ref         string `yaml:"ref"`
	Playbook    string `yaml:"playbook"`
	Inventory   string `yaml:"inventory"`
	Credentials string `yaml:"credentials"`
}

// ValuesRef is either a Vault reference ("vault:secret/...") or inline values.
type ValuesRef struct {
	Vault  string
	Inline map[string]any
}

func (v *ValuesRef) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		v.Vault = n.Value
		return nil
	}
	return n.Decode(&v.Inline)
}

// Parse decodes and validates a configuration. Unknown keys are errors so
// that typos never silently change behaviour.
func Parse(data []byte) (*Config, error) {
	var c Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	c.applyDefaults()
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

func (c *Config) applyDefaults() {
	d := &c.Defaults
	set := func(p *Duration, v time.Duration) {
		if *p == 0 {
			*p = Duration(v)
		}
	}
	set(&d.LockDuration, time.Hour)
	set(&d.JobTimeout, 30*time.Minute)
	set(&d.PreviewTTL, 72*time.Hour)
	set(&d.LogRetention, 90*24*time.Hour)
	set(&d.AuditRetention, 365*24*time.Hour)
	set(&d.ArtifactRetention, 30*24*time.Hour)
	if d.KeepBranchArtifacts == 0 {
		d.KeepBranchArtifacts = 50
	}
	for i := range c.Targets {
		t := &c.Targets[i]
		if t.Type == TypeHelm && t.Release == "" {
			t.Release = repoName(t.Project)
		}
	}
}

var (
	idPattern      = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	projectPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	k8sName        = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
)

// Validate returns all problems at once, so an operator fixes them in one go.
func (c *Config) Validate() error {
	var errs []error
	add := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }

	for p := range c.Projects {
		if !projectPattern.MatchString(p) {
			add("project %q: must be owner/repo", p)
		}
	}
	seen := map[string]bool{}
	for i, t := range c.Targets {
		where := fmt.Sprintf("target %q", t.ID)
		if t.ID == "" {
			where = fmt.Sprintf("target #%d", i+1)
		}
		if !idPattern.MatchString(t.ID) {
			add("%s: id must be lowercase letters, digits and dashes (max 63)", where)
		}
		if seen[t.ID] {
			add("%s: duplicate id", where)
		}
		seen[t.ID] = true
		if !projectPattern.MatchString(t.Project) {
			add("%s: project must be owner/repo", where)
		}
		if len(c.Registries(t.Project)) == 0 {
			add("%s: no allowed registries (set defaults.registries or projects.%s.registries)", where, t.Project)
		}
		for _, d := range []*Duration{t.LockDuration, t.JobTimeout, t.PreviewTTL} {
			if d != nil && *d <= 0 {
				add("%s: durations must be positive", where)
			}
		}
		if a := t.AutoDeploy; a != nil {
			if len(a.From) == 0 {
				add("%s: autoDeploy.from must not be empty", where)
			}
			for _, f := range a.From {
				if f != SourcePR && f != SourceBranch && f != SourceRelease {
					add("%s: autoDeploy.from: unknown source %q", where, f)
				}
			}
			if len(a.Branches) > 0 && !contains(a.From, SourceBranch) {
				add("%s: autoDeploy.branches requires from: [branch]", where)
			}
		}
		if t.Preview && t.Type != TypeHelm {
			add("%s: preview is only supported for helm targets", where)
		}
		if t.Preview && t.Lockable {
			add("%s: preview targets cannot be lockable", where)
		}
		if t.PreviewTTL != nil && !t.Preview {
			add("%s: previewTTL requires preview: true", where)
		}
		vault := func(field, v string, required bool) {
			if v == "" {
				if required {
					add("%s: %s is required", where, field)
				}
				return
			}
			if !strings.HasPrefix(v, "vault:") || len(v) == len("vault:") {
				add("%s: %s must be a vault: reference", where, field)
			}
		}
		switch t.Type {
		case TypeHelm:
			vault("cluster", t.Cluster, true)
			vault("registryCredentials", t.RegistryCredentials, false)
			if t.Values.Vault != "" {
				vault("values", t.Values.Vault, true)
			}
			if t.Preview && t.Namespace != "" {
				add("%s: preview targets derive the namespace per PR, remove namespace", where)
			}
			if !t.Preview && !k8sName.MatchString(t.Namespace) {
				add("%s: namespace is required and must be a valid Kubernetes name", where)
			}
			if !k8sName.MatchString(t.Release) {
				add("%s: release must be a valid Kubernetes name", where)
			}
		case TypeCompose:
			if t.Host == "" {
				add("%s: host is required", where)
			}
			vault("ssh", t.SSH, true)
			if !strings.HasPrefix(t.Dir, "/") {
				add("%s: dir must be an absolute path", where)
			}
		case TypeAnsible:
			a := t.Ansible
			if a == nil {
				add("%s: ansible block is required", where)
				break
			}
			if a.Source == "" {
				add("%s: ansible.source is required (\"repo\" or a git URL)", where)
			}
			if a.Source != "repo" && a.Playbook == "" {
				add("%s: ansible.playbook is required for an external source", where)
			}
			if a.Inventory == "" {
				add("%s: ansible.inventory is required", where)
			}
			vault("ansible.credentials", a.Credentials, true)
		default:
			add("%s: type must be helm, compose or ansible", where)
		}
	}
	return errors.Join(errs...)
}

// Registries returns the allowed registry prefixes for a project.
func (c *Config) Registries(project string) []string {
	if p, ok := c.Projects[project]; ok && len(p.Registries) > 0 {
		return p.Registries
	}
	return c.Defaults.Registries
}

// Target returns the target with the given id.
func (c *Config) Target(id string) (Target, bool) {
	for _, t := range c.Targets {
		if t.ID == id {
			return t, true
		}
	}
	return Target{}, false
}

// ProjectNames returns every project that is listed or has a target.
func (c *Config) ProjectNames() []string {
	seen := map[string]bool{}
	var out []string
	for p := range c.Projects {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, t := range c.Targets {
		if !seen[t.Project] {
			seen[t.Project] = true
			out = append(out, t.Project)
		}
	}
	return out
}

// HasProject reports whether the repo is a known project.
func (c *Config) HasProject(repo string) bool {
	_, ok := c.Project(repo)
	return ok
}

// Project returns the project name as spelled in the config. GitHub names are
// case-insensitive, so this keeps stored data consistent with the config.
func (c *Config) Project(repo string) (string, bool) {
	for _, p := range c.ProjectNames() {
		if strings.EqualFold(p, repo) {
			return p, true
		}
	}
	return "", false
}

// TargetsOf returns the targets of a project.
func (c *Config) TargetsOf(project string) []Target {
	var out []Target
	for _, t := range c.Targets {
		if strings.EqualFold(t.Project, project) {
			out = append(out, t)
		}
	}
	return out
}

func (c *Config) LockDuration(t Target) time.Duration {
	if t.LockDuration != nil {
		return t.LockDuration.D()
	}
	return c.Defaults.LockDuration.D()
}

func (c *Config) JobTimeout(t Target) time.Duration {
	if t.JobTimeout != nil {
		return t.JobTimeout.D()
	}
	return c.Defaults.JobTimeout.D()
}

func (c *Config) PreviewTTL(t Target) time.Duration {
	if t.PreviewTTL != nil {
		return t.PreviewTTL.D()
	}
	return c.Defaults.PreviewTTL.D()
}

// AllowsAutoDeploy checks the target's auto-deploy rule against an artifact source.
func (t Target) AllowsAutoDeploy(source, branch string) bool {
	a := t.AutoDeploy
	if a == nil || !contains(a.From, source) {
		return false
	}
	if source == SourceBranch && len(a.Branches) > 0 {
		return contains(a.Branches, branch)
	}
	return true
}

func repoName(project string) string {
	_, name, _ := strings.Cut(project, "/")
	return strings.ToLower(name)
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
