// Package registry checks that an image digest or chart version exists in an
// OCI registry, using the distribution API and the anonymous or basic-auth
// token flow most registries (GHCR, Docker Hub, Harbor) implement.
package registry

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

var manifestTypes = strings.Join([]string{
	"application/vnd.oci.image.index.v1+json",
	"application/vnd.oci.image.manifest.v1+json",
	"application/vnd.docker.distribution.manifest.list.v2+json",
	"application/vnd.docker.distribution.manifest.v2+json",
}, ", ")

type Checker struct {
	HTTP *http.Client
	// Credentials per registry host, e.g. from a Docker config.json.
	Credentials map[string]Credential
	// PlainHTTP lists hosts reached over http, for local test registries only.
	PlainHTTP map[string]bool
}

type Credential struct{ Username, Password string }

// ErrDenied means the registry refused access. Registries like GHCR answer a
// missing repository the same way, so the two cannot be told apart.
var ErrDenied = errors.New("not found or no access")

// LoadDockerConfig reads the "auths" section of a Docker config.json.
func LoadDockerConfig(path string) (map[string]Credential, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg struct {
		Auths map[string]struct {
			Auth     string `json:"auth"`
			Username string `json:"username"`
			Password string `json:"password"`
		} `json:"auths"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("registry auth file: %w", err)
	}
	out := map[string]Credential{}
	for host, a := range cfg.Auths {
		c := Credential{a.Username, a.Password}
		if a.Auth != "" {
			raw, err := base64.StdEncoding.DecodeString(a.Auth)
			if err != nil {
				return nil, fmt.Errorf("registry auth for %s: %w", host, err)
			}
			c.Username, c.Password, _ = strings.Cut(string(raw), ":")
		}
		host = strings.TrimPrefix(strings.TrimPrefix(host, "https://"), "http://")
		out[strings.TrimSuffix(host, "/")] = c
	}
	return out, nil
}

func (c *Checker) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 20 * time.Second}
}

// Exists reports whether repository (host/path) has the reference (digest or tag).
func (c *Checker) Exists(ctx context.Context, repository, reference string) (bool, error) {
	host, path, ok := strings.Cut(repository, "/")
	if !ok {
		return false, fmt.Errorf("invalid repository %q", repository)
	}
	scheme := "https"
	if c.PlainHTTP[host] {
		scheme = "http"
	}
	u := fmt.Sprintf("%s://%s/v2/%s/manifests/%s", scheme, host, path, url.PathEscape(reference))

	resp, err := c.head(ctx, u, "")
	if err != nil {
		return false, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		token, err := c.token(ctx, host, resp.Header.Get("WWW-Authenticate"))
		if err != nil {
			return false, err
		}
		if resp, err = c.head(ctx, u, token); err != nil {
			return false, err
		}
	}
	switch {
	case resp.StatusCode == http.StatusOK:
		return true, nil
	case resp.StatusCode == http.StatusNotFound:
		return false, nil
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return false, ErrDenied
	}
	return false, fmt.Errorf("%s: unexpected %s", repository, resp.Status)
}

func (c *Checker) head(ctx context.Context, u, auth string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", manifestTypes)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, err
	}
	resp.Body.Close()
	return resp, nil
}

// token follows the Bearer challenge, or falls back to basic auth.
func (c *Checker) token(ctx context.Context, host, challenge string) (string, error) {
	cred, hasCred := c.Credentials[host]
	scheme, params := parseChallenge(challenge)
	if strings.EqualFold(scheme, "basic") {
		if !hasCred {
			return "", ErrDenied
		}
		return "Basic " + base64.StdEncoding.EncodeToString([]byte(cred.Username+":"+cred.Password)), nil
	}
	if !strings.EqualFold(scheme, "bearer") || params["realm"] == "" {
		return "", fmt.Errorf("%s: unsupported auth challenge %q", host, challenge)
	}
	q := url.Values{}
	if s := params["service"]; s != "" {
		q.Set("service", s)
	}
	if s := params["scope"]; s != "" {
		q.Set("scope", s)
	}
	realm := params["realm"]
	sep := "?"
	if strings.Contains(realm, "?") {
		sep = "&"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, realm+sep+q.Encode(), nil)
	if err != nil {
		return "", err
	}
	if hasCred {
		req.SetBasicAuth(cred.Username, cred.Password)
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", ErrDenied
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s token: %s", host, resp.Status)
	}
	var out struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&out); err != nil {
		return "", err
	}
	if out.Token == "" {
		out.Token = out.AccessToken
	}
	return "Bearer " + out.Token, nil
}

// parseChallenge parses `Bearer realm="...",service="...",scope="..."`.
func parseChallenge(h string) (string, map[string]string) {
	scheme, rest, _ := strings.Cut(strings.TrimSpace(h), " ")
	params := map[string]string{}
	for rest != "" {
		rest = strings.TrimLeft(rest, " ,")
		k, v, ok := strings.Cut(rest, "=")
		if !ok {
			break
		}
		if strings.HasPrefix(v, `"`) {
			end := strings.Index(v[1:], `"`)
			if end < 0 {
				break
			}
			params[strings.TrimSpace(k)] = v[1 : end+1]
			rest = v[end+2:]
		} else {
			val, after, _ := strings.Cut(v, ",")
			params[strings.TrimSpace(k)] = val
			rest = after
		}
	}
	return scheme, params
}
