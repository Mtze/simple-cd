package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// scp-like git URLs such as git@github.com:org/repo.git
var scpURL = regexp.MustCompile(`^[A-Za-z0-9._-]+@[A-Za-z0-9.-]+:[^-]`)

func (x *executor) ansibleDeploy(ctx context.Context, a *AnsibleSpec) error {
	sshURL := strings.HasPrefix(a.RepoURL, "ssh://") || scpURL.MatchString(a.RepoURL)
	if !sshURL && !strings.HasPrefix(a.RepoURL, "https://") {
		return fmt.Errorf("ansible repo URL %q: only https:// and ssh URLs are supported", a.RepoURL)
	}
	if a.Ref == "" || strings.HasPrefix(a.Ref, "-") {
		return fmt.Errorf("ansible ref %q is invalid", a.Ref)
	}
	for _, p := range []string{a.Playbook, a.Inventory} {
		if !insideRepo(p) {
			return fmt.Errorf("ansible path %q must be relative and stay inside the repository", p)
		}
	}

	x.logf("==> Reading Ansible credentials")
	data, err := x.vault.read(ctx, a.CredentialsSecret)
	if err != nil {
		return err
	}
	key, err := secretString(data, a.CredentialsSecret, "private_key", true)
	if err != nil {
		return err
	}
	knownHosts, err := secretString(data, a.CredentialsSecret, "known_hosts", false)
	if err != nil {
		return err
	}
	user, err := secretString(data, a.CredentialsSecret, "user", false)
	if err != nil {
		return err
	}
	if user != "" && !safeUser.MatchString(user) {
		return fmt.Errorf("vault secret %q: key \"user\" is not a valid user name", a.CredentialsSecret)
	}
	if sshURL && knownHosts == "" {
		return fmt.Errorf("cloning %q over ssh needs known_hosts in vault secret %q", a.RepoURL, a.CredentialsSecret)
	}
	keyFile, err := x.writeSecret("ssh_key", []byte(withNewline(key)))
	if err != nil {
		return err
	}
	khFile := ""
	if knownHosts != "" {
		if khFile, err = x.writeSecret("known_hosts", []byte(withNewline(knownHosts))); err != nil {
			return err
		}
	}

	checkout, err := os.MkdirTemp("", "ansible-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(checkout)

	x.env = append(x.env, "GIT_TERMINAL_PROMPT=0")
	if khFile != "" {
		x.env = append(x.env, "GIT_SSH_COMMAND=ssh -i "+shellQuote(keyFile)+
			" -o IdentitiesOnly=yes -o BatchMode=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile="+shellQuote(khFile))
	}
	x.logf("==> Cloning %s", a.RepoURL)
	if err := x.run(ctx, "", "", "git", "clone", "--quiet", "--", a.RepoURL, checkout); err != nil {
		return err
	}
	x.logf("==> Checking out %s", a.Ref)
	// A branch other than the default only exists as origin/<branch> after the clone.
	if err := x.run(ctx, checkout, "", "git", "checkout", "--quiet", "--detach", a.Ref); err != nil {
		x.logf("Trying origin/%s", a.Ref)
		if err := x.run(ctx, checkout, "", "git", "checkout", "--quiet", "--detach", "origin/"+a.Ref); err != nil {
			return fmt.Errorf("checkout %s: %w", a.Ref, err)
		}
	}

	vars, err := json.Marshal(a.ExtraVars)
	if err != nil {
		return fmt.Errorf("encode extra vars: %w", err)
	}
	varsFile, err := x.writeSecret("extra-vars.json", vars)
	if err != nil {
		return err
	}

	sshArgs := "-o IdentitiesOnly=yes"
	if khFile != "" {
		sshArgs += " -o UserKnownHostsFile=" + khFile
	}
	x.env = append(x.env, "ANSIBLE_HOST_KEY_CHECKING=True", "ANSIBLE_SSH_ARGS="+sshArgs)
	args := []string{"-i", a.Inventory, a.Playbook, "--private-key", keyFile, "-e", "@" + varsFile}
	if user != "" {
		args = append(args, "-u", user)
	}
	x.logf("==> ansible-playbook -i %s %s", a.Inventory, a.Playbook)
	return x.run(ctx, checkout, "", "ansible-playbook", args...)
}

// insideRepo accepts relative paths that stay inside the checkout.
func insideRepo(p string) bool {
	if p == "" || filepath.IsAbs(p) || strings.HasPrefix(p, "-") {
		return false
	}
	clean := path.Clean(p)
	return clean != ".." && !strings.HasPrefix(clean, "../")
}
