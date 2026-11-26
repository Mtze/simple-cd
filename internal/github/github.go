// Package github talks to GitHub as a GitHub App: it verifies webhooks,
// mints installation tokens and reads artifacts, files and pull requests. It
// also reports deployments back so they show up on pull requests.
package github

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// maxDownload caps artifact and file downloads; deploy.json and config files are tiny.
const maxDownload = 1 << 20

// ErrNotFound is returned for missing files, refs and artifacts.
var ErrNotFound = errors.New("not found on GitHub")

// VerifySignature checks the X-Hub-Signature-256 header in constant time.
func VerifySignature(secret, body []byte, header string) bool {
	sig, ok := strings.CutPrefix(header, "sha256=")
	if !ok || len(secret) == 0 {
		return false
	}
	got, err := hex.DecodeString(sig)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}

// App authenticates as a GitHub App and caches installation tokens.
type App struct {
	ID      int64
	Key     *rsa.PrivateKey
	BaseURL string // https://api.github.com or a GHES API URL
	HTTP    *http.Client
	Now     func() time.Time

	mu     sync.Mutex
	tokens map[int64]cachedToken
}

type cachedToken struct {
	token   string
	expires time.Time
}

// ParsePrivateKey reads the PEM key GitHub generates for an App.
func ParsePrivateKey(pemData []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, errors.New("github app key: no PEM block")
	}
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("github app key: %w", err)
	}
	rk, ok := k.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("github app key: not an RSA key")
	}
	return rk, nil
}

func (a *App) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *App) client() *http.Client {
	if a.HTTP != nil {
		return a.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// jwt builds the short-lived app JWT (RS256), backdated for clock skew.
func (a *App) jwt() (string, error) {
	now := a.now()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, _ := json.Marshal(map[string]any{
		"iat": now.Add(-60 * time.Second).Unix(),
		"exp": now.Add(9 * time.Minute).Unix(),
		"iss": fmt.Sprint(a.ID),
	})
	signing := header + "." + base64.RawURLEncoding.EncodeToString(claims)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, a.Key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// Token returns an installation token, cached until five minutes before expiry.
func (a *App) Token(ctx context.Context, installation int64) (string, error) {
	a.mu.Lock()
	if t, ok := a.tokens[installation]; ok && a.now().Before(t.expires.Add(-5*time.Minute)) {
		a.mu.Unlock()
		return t.token, nil
	}
	a.mu.Unlock()

	jwt, err := a.jwt()
	if err != nil {
		return "", err
	}
	var out struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	path := fmt.Sprintf("/app/installations/%d/access_tokens", installation)
	if err := a.do(ctx, "Bearer "+jwt, http.MethodPost, path, nil, &out); err != nil {
		return "", fmt.Errorf("installation token: %w", err)
	}
	a.mu.Lock()
	if a.tokens == nil {
		a.tokens = map[int64]cachedToken{}
	}
	a.tokens[installation] = cachedToken{out.Token, out.ExpiresAt}
	a.mu.Unlock()
	return out.Token, nil
}

// InstallationFor looks up the installation of a repository.
func (a *App) InstallationFor(ctx context.Context, repo string) (int64, error) {
	jwt, err := a.jwt()
	if err != nil {
		return 0, err
	}
	var out struct {
		ID int64 `json:"id"`
	}
	err = a.do(ctx, "Bearer "+jwt, http.MethodGet, "/repos/"+repo+"/installation", nil, &out)
	return out.ID, err
}

// Client returns an API client for one installation.
func (a *App) Client(installation int64) *Client {
	return &Client{app: a, installation: installation}
}

func (a *App) do(ctx context.Context, auth, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimSuffix(a.BaseURL, "/")+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Authorization", auth)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := a.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, strings.TrimSpace(string(msg)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(resp.Body, maxDownload*4)).Decode(out)
}

// Client calls the API with an installation token.
type Client struct {
	app          *App
	installation int64
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	tok, err := c.app.Token(ctx, c.installation)
	if err != nil {
		return err
	}
	return c.app.do(ctx, "token "+tok, method, path, body, out)
}

// DownloadArtifactFile returns one file from the newest non-expired workflow
// artifact with the given name, or ErrNotFound.
func (c *Client) DownloadArtifactFile(ctx context.Context, repo string, runID int64, artifact, file string) ([]byte, error) {
	var list struct {
		Artifacts []struct {
			ID      int64 `json:"id"`
			Name    string
			Expired bool `json:"expired"`
		} `json:"artifacts"`
	}
	path := fmt.Sprintf("/repos/%s/actions/runs/%d/artifacts?name=%s", repo, runID, url.QueryEscape(artifact))
	if err := c.do(ctx, http.MethodGet, path, nil, &list); err != nil {
		return nil, err
	}
	var id int64
	for _, a := range list.Artifacts {
		if a.Name == artifact && !a.Expired && a.ID > id {
			id = a.ID
		}
	}
	if id == 0 {
		return nil, ErrNotFound
	}
	zipData, err := c.download(ctx, fmt.Sprintf("/repos/%s/actions/artifacts/%d/zip", repo, id))
	if err != nil {
		return nil, err
	}
	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return nil, fmt.Errorf("artifact zip: %w", err)
	}
	for _, f := range zr.File {
		if f.Name != file {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		data, err := io.ReadAll(io.LimitReader(rc, maxDownload+1))
		if err != nil {
			return nil, err
		}
		if len(data) > maxDownload {
			return nil, fmt.Errorf("%s in artifact is larger than 1 MiB", file)
		}
		return data, nil
	}
	return nil, fmt.Errorf("artifact %q has no %s: %w", artifact, file, ErrNotFound)
}

// download follows GitHub's redirect to blob storage. The token is not sent
// to the redirect target.
func (c *Client) download(ctx context.Context, path string) ([]byte, error) {
	tok, err := c.app.Token(ctx, c.installation)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(c.app.BaseURL, "/")+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "token "+tok)
	resp, err := c.app.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("download %s: %s", path, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDownload+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxDownload {
		return nil, errors.New("artifact larger than 1 MiB")
	}
	return data, nil
}

// File returns the content of a file at a ref, or ErrNotFound.
func (c *Client) File(ctx context.Context, repo, path, ref string) ([]byte, error) {
	var out struct {
		Type     string `json:"type"`
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
		Size     int    `json:"size"`
	}
	p := fmt.Sprintf("/repos/%s/contents/%s?ref=%s", repo, escapePath(path), url.QueryEscape(ref))
	if err := c.do(ctx, http.MethodGet, p, nil, &out); err != nil {
		return nil, err
	}
	if out.Type != "file" {
		return nil, ErrNotFound
	}
	if out.Size > maxDownload || out.Encoding != "base64" {
		return nil, fmt.Errorf("%s: larger than 1 MiB", path)
	}
	return base64.StdEncoding.DecodeString(strings.ReplaceAll(out.Content, "\n", ""))
}

func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}

// PullRequestsForCommit returns the numbers of open PRs that contain the commit.
func (c *Client) PullRequestsForCommit(ctx context.Context, repo, sha string) ([]int, error) {
	var prs []struct {
		Number int    `json:"number"`
		State  string `json:"state"`
	}
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/commits/%s/pulls", repo, sha), nil, &prs); err != nil {
		return nil, err
	}
	var out []int
	for _, p := range prs {
		if p.State == "open" {
			out = append(out, p.Number)
		}
	}
	return out, nil
}

// PullRequestTitle returns the title of a pull request.
func (c *Client) PullRequestTitle(ctx context.Context, repo string, number int) (string, error) {
	var pr struct {
		Title string `json:"title"`
	}
	err := c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/pulls/%d", repo, number), nil, &pr)
	return pr.Title, err
}

// TagCommit returns the commit a tag points to, or ErrNotFound when there is
// no such tag. Annotated tags are peeled to their commit.
func (c *Client) TagCommit(ctx context.Context, repo, name string) (string, error) {
	var ref struct {
		Object struct {
			SHA  string `json:"sha"`
			Type string `json:"type"`
		} `json:"object"`
	}
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/git/ref/tags/%s", repo, escapePath(name)), nil, &ref); err != nil {
		return "", err
	}
	for i := 0; ref.Object.Type == "tag" && i < 5; i++ {
		if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/git/tags/%s", repo, url.PathEscape(ref.Object.SHA)), nil, &ref); err != nil {
			return "", err
		}
	}
	if ref.Object.Type != "commit" {
		return "", fmt.Errorf("tag %s points to a %s", name, ref.Object.Type)
	}
	return ref.Object.SHA, nil
}

type DeploymentRequest struct {
	Ref         string `json:"ref"`
	Environment string `json:"environment"`
	Description string `json:"description"`
	Transient   bool   `json:"transient_environment"`
	Production  bool   `json:"production_environment"`
}

// CreateDeployment registers a deployment so it shows up on the PR.
func (c *Client) CreateDeployment(ctx context.Context, repo string, d DeploymentRequest) (int64, error) {
	body := map[string]any{
		"ref": d.Ref, "environment": d.Environment, "description": d.Description,
		"transient_environment": d.Transient, "production_environment": d.Production,
		"auto_merge": false, "required_contexts": []string{},
	}
	var out struct {
		ID int64 `json:"id"`
	}
	err := c.do(ctx, http.MethodPost, "/repos/"+repo+"/deployments", body, &out)
	return out.ID, err
}

// SetDeploymentStatus updates the state: queued, in_progress, success, failure, error or inactive.
func (c *Client) SetDeploymentStatus(ctx context.Context, repo string, id int64, state, description, logURL string) error {
	if len(description) > 140 {
		description = description[:137] + "..."
	}
	body := map[string]any{"state": state, "description": description, "log_url": logURL, "auto_inactive": state == "success"}
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/deployments/%d/statuses", repo, id), body, nil)
}
