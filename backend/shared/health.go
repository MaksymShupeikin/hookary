// Package shared holds the concrete helpers every Hookary service needs.
// It is a toolbox, not an abstraction layer: nothing here exists only to be
// mocked or to satisfy a dependency rule.
package shared

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// ReadyFunc reports whether a service can do useful work right now. Returning
// an error makes /health/ready answer 503 while /health/live stays 200, so an
// orchestrator pulls the service out of rotation instead of restarting it.
type ReadyFunc func(context.Context) error

const readyProbeTimeout = 3 * time.Second

// ReadyAll runs probes in order and reports the first failure. Order them
// cheapest first; there is no point dialing storage when the pool is already
// gone.
func ReadyAll(probes ...ReadyFunc) ReadyFunc {
	return func(ctx context.Context) error {
		for _, probe := range probes {
			if err := probe(ctx); err != nil {
				return err
			}
		}
		return nil
	}
}

// LiveHandler answers whether the process is running. It never consults a
// dependency: a failing database means "stop sending me traffic", not
// "restart me", and conflating the two turns an outage into a crash loop.
func LiveHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writePlain(w, http.StatusOK, "ok")
	}
}

// ReadyHandler answers whether the service can serve traffic. A nil ready means
// ready as soon as the process is listening.
func ReadyHandler(ready ReadyFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if ready == nil {
			writePlain(w, http.StatusOK, "ready")
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), readyProbeTimeout)
		defer cancel()

		if err := ready(ctx); err != nil {
			writePlain(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		writePlain(w, http.StatusOK, "ready")
	}
}

// RunHealthServer serves the health endpoints on addr and blocks until ctx is
// cancelled, then shuts down gracefully.
//
// Workers have no public API but still run one, so compose and later a load
// balancer can see them. The API serves the same handlers from its own router
// instead.
func RunHealthServer(ctx context.Context, addr string, ready ReadyFunc) error {
	mux := http.NewServeMux()
	mux.Handle("GET /health/live", LiveHandler())
	mux.Handle("GET /health/ready", ReadyHandler(ready))

	return RunHTTPServer(ctx, addr, mux)
}

// RunHTTPServer runs srv until ctx is cancelled, then drains it. Shutdown gets
// its own timeout budget because ctx is already cancelled by the time draining
// starts.
func RunHTTPServer(ctx context.Context, addr string, handler http.Handler) error {
	server := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	listenErr := make(chan error, 1)
	go func() {
		err := server.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		listenErr <- err
	}()

	select {
	case err := <-listenErr:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	return server.Shutdown(shutdownCtx)
}

func writePlain(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body + "\n"))
}
