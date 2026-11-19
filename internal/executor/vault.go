package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type vault struct {
	addr      string
	token     string
	namespace string
	client    *http.Client
}

// vaultLogin uses VAULT_TOKEN when set (local dev), otherwise Kubernetes auth
// with the Job's service account token.
func vaultLogin(ctx context.Context, getenv func(string) string) (*vault, error) {
	v := &vault{
		addr:      strings.TrimRight(getenv("VAULT_ADDR"), "/"),
		token:     getenv("VAULT_TOKEN"),
		namespace: getenv("VAULT_NAMESPACE"),
		client:    &http.Client{Timeout: 30 * time.Second},
	}
	if v.addr == "" {
		return nil, fmt.Errorf("VAULT_ADDR is not set")
	}
	if v.token != "" {
		return v, nil
	}
	role := getenv("SIMPLECD_VAULT_ROLE")
	if role == "" {
		return nil, fmt.Errorf("neither VAULT_TOKEN nor SIMPLECD_VAULT_ROLE is set")
	}
	mount := envOr(getenv, "SIMPLECD_VAULT_AUTH_MOUNT", "kubernetes")
	jwt, err := os.ReadFile(envOr(getenv, "SIMPLECD_SA_TOKEN_FILE", "/var/run/secrets/kubernetes.io/serviceaccount/token"))
	if err != nil {
		return nil, fmt.Errorf("read service account token: %w", err)
	}
	body, _ := json.Marshal(map[string]string{"role": role, "jwt": strings.TrimSpace(string(jwt))})
	var resp struct {
		Auth struct {
			ClientToken string `json:"client_token"`
		} `json:"auth"`
	}
	if err := v.do(ctx, http.MethodPost, "/v1/auth/"+mount+"/login", body, &resp); err != nil {
		return nil, fmt.Errorf("vault login (role %s): %w", role, err)
	}
	if resp.Auth.ClientToken == "" {
		return nil, fmt.Errorf("vault login (role %s): no client token in response", role)
	}
	v.token = resp.Auth.ClientToken
	return v, nil
}

// read returns the data of a KV v2 secret such as "secret/cd/clusters/test".
func (v *vault) read(ctx context.Context, ref string) (map[string]any, error) {
	// shortcut: assumes KV v2 with the mount as first path segment, add a mount setting when someone uses KV v1 or nested mounts
	mount, p, ok := strings.Cut(strings.Trim(ref, "/"), "/")
	if !ok || p == "" {
		return nil, fmt.Errorf("vault secret %q: expected <mount>/<path>", ref)
	}
	var resp struct {
		Data struct {
			Data map[string]any `json:"data"`
		} `json:"data"`
	}
	if err := v.do(ctx, http.MethodGet, "/v1/"+mount+"/data/"+p, nil, &resp); err != nil {
		return nil, fmt.Errorf("vault secret %q: %w", ref, err)
	}
	if resp.Data.Data == nil {
		return nil, fmt.Errorf("vault secret %q: no data", ref)
	}
	return resp.Data.Data, nil
}

func (v *vault) do(ctx context.Context, method, path string, body []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, v.addr+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if v.token != "" {
		req.Header.Set("X-Vault-Token", v.token)
	}
	if v.namespace != "" {
		req.Header.Set("X-Vault-Namespace", v.namespace)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := v.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		// Vault error bodies only carry messages, never secret data.
		var e struct {
			Errors []string `json:"errors"`
		}
		_ = json.Unmarshal(data, &e)
		return fmt.Errorf("%s: %s", res.Status, strings.Join(e.Errors, "; "))
	}
	return json.Unmarshal(data, out)
}

// secretString returns key from a secret. Missing required keys are an error
// that names the key but never a value.
func secretString(data map[string]any, ref, key string, required bool) (string, error) {
	raw, ok := data[key]
	if !ok || raw == nil {
		if required {
			return "", fmt.Errorf("vault secret %q has no key %q", ref, key)
		}
		return "", nil
	}
	s, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("vault secret %q: key %q is not a string", ref, key)
	}
	if required && s == "" {
		return "", fmt.Errorf("vault secret %q: key %q is empty", ref, key)
	}
	return s, nil
}

func envOr(getenv func(string) string, key, def string) string {
	if v := getenv(key); v != "" {
		return v
	}
	return def
}
