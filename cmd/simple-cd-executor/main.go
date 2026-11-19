// Command simple-cd-executor runs one deployment inside a Kubernetes Job.
// It reads the spec from SIMPLECD_SPEC and exits non-zero on failure.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Mtze/simple-cd/internal/executor"
)

func main() {
	// Kubernetes sends SIGTERM when activeDeadlineSeconds is reached.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	var spec executor.Spec
	if err := json.Unmarshal([]byte(os.Getenv("SIMPLECD_SPEC")), &spec); err != nil {
		fmt.Println("==> FAILED: SIMPLECD_SPEC is not a valid spec:", err)
		os.Exit(2)
	}
	if err := executor.Run(ctx, spec, os.Getenv, os.Stdout); err != nil {
		fmt.Println("==> FAILED:", err)
		os.Exit(1)
	}
	fmt.Println("==> Done")
}
