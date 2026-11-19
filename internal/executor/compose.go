package executor

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
)

var (
	safeDir  = regexp.MustCompile(`^/[A-Za-z0-9._/-]*$`)
	safeHost = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]*$`)
	safeUser = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]*$`)
	envKey   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

func (x *executor) composeDeploy(ctx context.Context, c *ComposeSpec) error {
	if !safeHost.MatchString(c.Host) {
		return fmt.Errorf("compose host %q is not a plain host name", c.Host)
	}
	if !safeDir.MatchString(c.Dir) || path.Clean(c.Dir) != c.Dir || c.Dir == "/" {
		return fmt.Errorf("compose dir %q must be a clean absolute path of letters, digits, '.', '_', '-' and '/'", c.Dir)
	}
	envFile, err := cdEnv(c.Env)
	if err != nil {
		return err
	}

	x.logf("==> Reading SSH credentials")
	data, err := x.vault.read(ctx, c.SSHSecret)
	if err != nil {
		return err
	}
	key, err := secretString(data, c.SSHSecret, "private_key", true)
	if err != nil {
		return err
	}
	// No trust on first use: without known_hosts we cannot tell the VM from an impostor.
	knownHosts, err := secretString(data, c.SSHSecret, "known_hosts", true)
	if err != nil {
		return err
	}
	user, err := secretString(data, c.SSHSecret, "user", false)
	if err != nil {
		return err
	}
	if user == "" {
		user = "deploy"
	}
	if !safeUser.MatchString(user) {
		return fmt.Errorf("vault secret %q: key \"user\" is not a valid user name", c.SSHSecret)
	}
	keyFile, err := x.writeSecret("ssh_key", []byte(withNewline(key)))
	if err != nil {
		return err
	}
	khFile, err := x.writeSecret("known_hosts", []byte(withNewline(knownHosts)))
	if err != nil {
		return err
	}

	ssh := func(stdin, remote string) error {
		return x.run(ctx, "", stdin, "ssh", "-i", keyFile, "-o", "IdentitiesOnly=yes", "-o", "BatchMode=yes",
			"-o", "StrictHostKeyChecking=yes", "-o", "UserKnownHostsFile="+khFile, user+"@"+c.Host, remote)
	}
	// Write to a temp file and rename, so a dropped connection never leaves a half written file.
	upload := func(name, content string) error {
		dir := shellQuote(c.Dir)
		tmp := dir + "/." + name + ".tmp"
		return ssh(content, "mkdir -p "+dir+" && cat > "+tmp+" && mv "+tmp+" "+dir+"/"+name)
	}

	x.logf("==> Uploading docker-compose.yml to %s:%s", c.Host, c.Dir)
	if err := upload("docker-compose.yml", c.ComposeFile); err != nil {
		return err
	}
	x.logf("==> Uploading .cd.env")
	if err := upload(".cd.env", envFile); err != nil {
		return err
	}
	x.logf("==> docker compose pull && up -d on %s", c.Host)
	// Plain POSIX sh syntax, the login shell of the deploy user runs it.
	compose := "docker compose $E --env-file .cd.env"
	return ssh("", "cd "+shellQuote(c.Dir)+" && if [ -f .env ]; then E='--env-file .env'; else E=; fi && "+
		compose+" pull && "+compose+" up -d --remove-orphans")
}

// cdEnv renders KEY=value lines, sorted so the file is stable between deploys.
func cdEnv(env map[string]string) (string, error) {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		if !envKey.MatchString(k) {
			return "", fmt.Errorf("compose env: invalid variable name %q", k)
		}
		if strings.ContainsAny(env[k], "\n\r") {
			return "", fmt.Errorf("compose env: value of %s contains a newline", k)
		}
		b.WriteString(k + "=" + env[k] + "\n")
	}
	return b.String(), nil
}

// withNewline fixes keys stored without a trailing newline, which ssh rejects.
func withNewline(s string) string {
	if strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}
