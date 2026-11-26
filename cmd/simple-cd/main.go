// Command simple-cd is the delivery server.
//
//	simple-cd [serve]               run the server (default)
//	simple-cd validate config.yaml  check a target config, for CI in the config repo
//	simple-cd migrate               apply database migrations and exit
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Mtze/simple-cd/internal/api"
	"github.com/Mtze/simple-cd/internal/auth"
	"github.com/Mtze/simple-cd/internal/config"
	"github.com/Mtze/simple-cd/internal/core"
	"github.com/Mtze/simple-cd/internal/github"
	"github.com/Mtze/simple-cd/internal/ingest"
	"github.com/Mtze/simple-cd/internal/registry"
	"github.com/Mtze/simple-cd/internal/runner"
	"github.com/Mtze/simple-cd/internal/store"
	"github.com/Mtze/simple-cd/internal/worker"
	"github.com/Mtze/simple-cd/web"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	var err error
	switch cmd {
	case "serve":
		err = serve()
	case "validate":
		err = validate(os.Args[2:])
	case "migrate":
		err = migrate()
	default:
		err = fmt.Errorf("unknown command %q (serve, validate, migrate)", cmd)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func validate(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: simple-cd validate <config.yaml>")
	}
	cfg, err := config.Load(args[0])
	if err != nil {
		return err
	}
	fmt.Printf("ok: %d projects, %d targets\n", len(cfg.ProjectNames()), len(cfg.Targets))
	return nil
}

// env reads a setting; NAME_FILE takes precedence so secrets can come from mounted files.
func env(name, def string) string {
	if f := os.Getenv(name + "_FILE"); f != "" {
		data, err := os.ReadFile(f)
		if err != nil {
			slog.Error("read secret file", "var", name+"_FILE", "err", err)
			os.Exit(1)
		}
		return strings.TrimSpace(string(data))
	}
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

func required(names ...string) error {
	var missing []string
	for _, n := range names {
		if env(n, "") == "" {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing settings: %s", strings.Join(missing, ", "))
	}
	return nil
}

func migrate() error {
	ctx := context.Background()
	st, err := store.Open(ctx, env("SIMPLECD_DATABASE_URL", ""))
	if err != nil {
		return err
	}
	defer st.Close()
	return st.Migrate(ctx)
}

func serve() error {
	if err := required("SIMPLECD_CONFIG", "SIMPLECD_DATABASE_URL", "SIMPLECD_BASE_URL",
		"SIMPLECD_OIDC_ISSUER", "SIMPLECD_OIDC_CLIENT_ID", "SIMPLECD_OIDC_CLIENT_SECRET",
		"SIMPLECD_GITHUB_APP_ID", "SIMPLECD_GITHUB_PRIVATE_KEY", "SIMPLECD_GITHUB_WEBHOOK_SECRET"); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	baseURL := strings.TrimSuffix(env("SIMPLECD_BASE_URL", ""), "/")

	cfg, err := config.NewHolder(env("SIMPLECD_CONFIG", ""))
	if err != nil {
		return err
	}
	go cfg.Watch(ctx, 10*time.Second)

	st, err := store.Open(ctx, env("SIMPLECD_DATABASE_URL", ""))
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	sessionTTL, err := config.ParseDuration(env("SIMPLECD_SESSION_TTL", "4h"))
	if err != nil {
		return err
	}
	authn, err := auth.New(ctx, auth.Config{
		Issuer: env("SIMPLECD_OIDC_ISSUER", ""), ClientID: env("SIMPLECD_OIDC_CLIENT_ID", ""),
		ClientSecret: env("SIMPLECD_OIDC_CLIENT_SECRET", ""), BaseURL: baseURL, SessionTTL: sessionTTL,
	}, st)
	if err != nil {
		return err
	}

	key, err := github.ParsePrivateKey([]byte(env("SIMPLECD_GITHUB_PRIVATE_KEY", "")))
	if err != nil {
		return err
	}
	var appID int64
	if _, err := fmt.Sscan(env("SIMPLECD_GITHUB_APP_ID", ""), &appID); err != nil {
		return fmt.Errorf("SIMPLECD_GITHUB_APP_ID: %w", err)
	}
	app := &github.App{ID: appID, Key: key, BaseURL: env("SIMPLECD_GITHUB_API_URL", "https://api.github.com")}
	reporter := &github.Reporter{App: app, BaseURL: baseURL}

	reg := &registry.Checker{PlainHTTP: map[string]bool{}}
	for _, host := range strings.Split(env("SIMPLECD_REGISTRY_PLAIN_HTTP", ""), ",") {
		if host != "" {
			slog.Warn("registry is reached over plain http: for test setups only", "host", host)
			reg.PlainHTTP[host] = true
		}
	}
	if f := env("SIMPLECD_REGISTRY_AUTH_FILE", ""); f != "" {
		if reg.Credentials, err = registry.LoadDockerConfig(f); err != nil {
			return err
		}
	}

	run, err := newRunner(baseURL)
	if err != nil {
		return err
	}

	var checker core.RegistryChecker = reg
	if env("SIMPLECD_REGISTRY_CHECK", "on") == "off" {
		slog.Warn("registry checks are off: for local development only")
		checker = nil
	}
	svc := &core.Service{Store: st, Config: cfg, Registry: checker, Notifier: reporter}
	gitHost := env("SIMPLECD_GIT_SSH_HOST", "github.com")
	w := &worker.Worker{
		Store: st, Config: cfg, Core: svc, Runner: run, Reporter: reporter,
		GitURL: func(repo string) string { return "git@" + gitHost + ":" + repo + ".git" },
	}
	go w.Run(ctx)

	events := &api.Hub{}
	go st.Listen(ctx, events.Publish)

	srv := &api.Server{
		Store: st, Config: cfg, Core: svc, Auth: authn, Runner: run, BaseURL: baseURL, Events: events,
		ClientID:      env("SIMPLECD_OIDC_CLIENT_ID", ""),
		WebhookSecret: []byte(env("SIMPLECD_GITHUB_WEBHOOK_SECRET", "")), UI: web.Dist(),
		Ingest: &ingest.Ingester{Config: cfg, Store: st, Core: svc, Registry: checker,
			GitHub: func(id int64) ingest.RepoAPI { return app.Client(id) }},
	}
	httpSrv := &http.Server{
		Addr: env("SIMPLECD_LISTEN", ":8080"), Handler: srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		httpSrv.Shutdown(shutdown)
	}()
	slog.Info("simple-cd listening", "addr", httpSrv.Addr, "baseURL", baseURL, "ui", srv.UI != nil)
	if err := httpSrv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// executorEnv collects the settings passed through to every executor run.
func executorEnv() map[string]string {
	out := map[string]string{}
	for _, k := range []string{"VAULT_ADDR", "VAULT_NAMESPACE", "SIMPLECD_VAULT_ROLE", "SIMPLECD_VAULT_AUTH_MOUNT", "SIMPLECD_REGISTRY_PLAIN_HTTP"} {
		if v := os.Getenv(k); v != "" {
			out[k] = v
		}
	}
	return out
}

func newRunner(baseURL string) (runner.Runner, error) {
	switch mode := env("SIMPLECD_RUNNER", "kubernetes"); mode {
	case "kubernetes":
		k, err := runner.InCluster()
		if err != nil {
			return nil, err
		}
		k.Image = env("SIMPLECD_EXECUTOR_IMAGE", "")
		k.AnsibleImage = env("SIMPLECD_EXECUTOR_ANSIBLE_IMAGE", k.Image)
		k.ServiceAccount = env("SIMPLECD_EXECUTOR_SERVICE_ACCOUNT", "simple-cd-executor")
		k.Env = executorEnv()
		if ns := env("SIMPLECD_JOB_NAMESPACE", ""); ns != "" {
			k.Namespace = ns
		}
		if k.Image == "" {
			return nil, errors.New("SIMPLECD_EXECUTOR_IMAGE is required for the kubernetes runner")
		}
		return k, nil
	case "process":
		// The process runner hands the server's own secrets to every deploy.
		if strings.HasPrefix(baseURL, "https://") {
			return nil, errors.New("the process runner is for local development only and refuses an https SIMPLECD_BASE_URL")
		}
		slog.Warn("using the process runner: for local development only")
		e := executorEnv()
		if t := os.Getenv("VAULT_TOKEN"); t != "" {
			e["VAULT_TOKEN"] = t
		}
		return &runner.Process{
			Binary: env("SIMPLECD_EXECUTOR_BINARY", "simple-cd-executor"),
			Env:    e,
			Dir:    filepath.Join(os.TempDir(), "simple-cd-runs"),
		}, nil
	default:
		return nil, fmt.Errorf("SIMPLECD_RUNNER must be kubernetes or process, not %q", mode)
	}
}
