// Package core holds the domain rules: who may do what, the target status,
// readiness of artifacts, locks, slots and auto-deploy.
package core

import (
	"sort"
	"strings"

	"github.com/Mtze/simple-cd/internal/config"
)

// RolePlatformAdmin may do everything in every project and read the audit log.
const RolePlatformAdmin = "platform-admin"

// Project roles.
const (
	RoleDeveloper  = "developer"
	RoleMaintainer = "maintainer"
)

// AutoDeployActor is the actor recorded for deployments started by GitHub.
const AutoDeployActor = "auto-deploy"

// SystemActor is the actor for work the server does by itself (TTL teardown).
const SystemActor = "system"

type User struct {
	Username string   `json:"username"`
	Name     string   `json:"name"`
	Email    string   `json:"email"`
	Roles    []string `json:"roles"`
}

func (u User) Has(role string) bool {
	for _, r := range u.Roles {
		if r == role {
			return true
		}
	}
	return false
}

func (u User) IsAdmin() bool { return u.Has(RolePlatformAdmin) }

// ProjectRoleName returns the Keycloak role name, e.g. project:org/app:developer.
// GitHub repo names are case-insensitive, so the role uses lowercase.
func ProjectRoleName(project, role string) string {
	return "project:" + strings.ToLower(project) + ":" + role
}

// Role is a client role in Keycloak that the configuration refers to.
type Role struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Roles lists every client role the configuration uses, so an admin can
// create them in Keycloak. simple-cd never creates roles itself: that would
// need admin rights on the whole realm.
func Roles(cfg *config.Config) []Role {
	out := []Role{
		{RolePlatformAdmin, "simple-cd: everything in every project, audit log"},
		{"viewer", "simple-cd: read access only"},
	}
	seen := map[string]bool{RolePlatformAdmin: true, "viewer": true}
	add := func(name, desc string) {
		if !seen[name] {
			seen[name] = true
			out = append(out, Role{name, desc})
		}
	}
	projects := cfg.ProjectNames()
	sort.Slice(projects, func(i, j int) bool { return strings.ToLower(projects[i]) < strings.ToLower(projects[j]) })
	for _, p := range projects {
		p = strings.ToLower(p)
		add(ProjectRoleName(p, RoleDeveloper), "simple-cd: deploy "+p)
		add(ProjectRoleName(p, RoleMaintainer), "simple-cd: deploy "+p+", take over locks, maintenance")
	}
	var targets []Role
	for _, t := range cfg.Targets {
		if t.Role != "" {
			targets = append(targets, Role{t.Role, "simple-cd: also needed to deploy to " + t.ID})
		}
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].Name < targets[j].Name })
	for _, r := range targets {
		add(r.Name, r.Description)
	}
	return out
}

// ProjectRole returns "maintainer", "developer" or "".
func (u User) ProjectRole(project string) string {
	switch {
	case u.Has(ProjectRoleName(project, RoleMaintainer)):
		return RoleMaintainer
	case u.Has(ProjectRoleName(project, RoleDeveloper)):
		return RoleDeveloper
	}
	return ""
}

// Permissions are what a user may do on one target. The UI uses them to
// hide controls; the server enforces them on every request.
type Permissions struct {
	Deploy      bool `json:"deploy"`
	Override    bool `json:"override"`
	Maintenance bool `json:"maintenance"`
}

func PermissionsFor(u User, t config.Target) Permissions {
	if u.IsAdmin() {
		return Permissions{Deploy: true, Override: true, Maintenance: true}
	}
	role := u.ProjectRole(t.Project)
	maintainer := role == RoleMaintainer
	deploy := role != "" && (t.Role == "" || u.Has(t.Role))
	return Permissions{Deploy: deploy, Override: deploy && maintainer, Maintenance: maintainer}
}
