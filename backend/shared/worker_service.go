package shared

import (
	"context"
	"errors"
	"fmt"
	"os"
)

func RunWorkerService(
	ctx context.Context,
	addr string,
	ready ReadyFunc,
	runWorker func(context.Context) error,
) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	errs := make(chan error, 2)
	go func() { errs <- RunHealthServer(runCtx, addr, ready) }()
	go func() { errs <- runWorker(runCtx) }()

	first := <-errs
	parentStopped := ctx.Err() != nil
	cancel()
	second := <-errs
	if first != nil {
		return first
	}
	if second != nil {
		return second
	}
	if !parentStopped {
		return errors.New("worker service stopped unexpectedly")
	}
	return nil
}

func WorkerName(stage string) string {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = fmt.Sprintf("pid-%d", os.Getpid())
	}
	return stage + "-" + hostname
}
