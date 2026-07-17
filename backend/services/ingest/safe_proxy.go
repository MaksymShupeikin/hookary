package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/MaksymShupeikin/hookary/backend/shared"
)

type safeProxy struct {
	listener  net.Listener
	server    *http.Server
	transport *http.Transport
}

func startSafeProxy(ctx context.Context) (*safeProxy, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen for ingest proxy: %w", err)
	}
	proxy := &safeProxy{listener: listener}
	proxy.transport = &http.Transport{
		Proxy:                 nil,
		DialContext:           dialPublicAddress,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   20 * time.Second,
		ResponseHeaderTimeout: 45 * time.Second,
		IdleConnTimeout:       30 * time.Second,
	}
	proxy.server = &http.Server{
		Handler:           http.HandlerFunc(proxy.serveHTTP),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		_ = proxy.server.Serve(listener)
	}()
	go func() {
		<-ctx.Done()
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = proxy.Close(shutdownContext)
	}()
	return proxy, nil
}

func (p *safeProxy) URL() string { return "http://" + p.listener.Addr().String() }

func (p *safeProxy) Close(ctx context.Context) error {
	p.transport.CloseIdleConnections()
	err := p.server.Shutdown(ctx)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (p *safeProxy) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		p.connect(w, r)
		return
	}
	if _, err := shared.ValidatePublicURL(r.Context(), r.URL.String()); err != nil {
		http.Error(w, "destination rejected", http.StatusForbidden)
		return
	}
	outgoing := r.Clone(r.Context())
	outgoing.RequestURI = ""
	removeHopHeaders(outgoing.Header)
	response, err := p.transport.RoundTrip(outgoing)
	if err != nil {
		http.Error(w, "destination unavailable", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	copyHeader(w.Header(), response.Header)
	removeHopHeaders(w.Header())
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, response.Body)
}

func (p *safeProxy) connect(w http.ResponseWriter, r *http.Request) {
	address := r.Host
	if !strings.Contains(address, ":") {
		address = net.JoinHostPort(address, "443")
	}
	upstream, err := dialPublicAddress(r.Context(), "tcp", address)
	if err != nil {
		http.Error(w, "destination rejected", http.StatusForbidden)
		return
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		upstream.Close()
		http.Error(w, "proxy transport unavailable", http.StatusInternalServerError)
		return
	}
	client, buffered, err := hijacker.Hijack()
	if err != nil {
		upstream.Close()
		return
	}
	if _, err := buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		client.Close()
		upstream.Close()
		return
	}
	if err := buffered.Flush(); err != nil {
		client.Close()
		upstream.Close()
		return
	}

	go func() {
		_, _ = io.Copy(upstream, buffered)
		_ = upstream.Close()
	}()
	_, _ = io.Copy(client, upstream)
	_ = client.Close()
}

func dialPublicAddress(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errors.New("destination has no valid port")
	}
	if port != "80" && port != "443" {
		return nil, errors.New("destination port is not allowed")
	}
	addresses, err := shared.ResolvePublicHost(ctx, host)
	if err != nil {
		return nil, err
	}
	dialer := net.Dialer{Timeout: 20 * time.Second, KeepAlive: 30 * time.Second}
	var lastErr error
	for _, candidate := range addresses {
		connection, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(candidate.String(), port))
		if dialErr == nil {
			return connection, nil
		}
		lastErr = dialErr
	}
	return nil, fmt.Errorf("connect to public destination: %w", lastErr)
}

func removeHopHeaders(header http.Header) {
	for _, name := range []string{
		"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate",
		"Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade",
	} {
		header.Del(name)
	}
}

func copyHeader(destination, source http.Header) {
	for name, values := range source {
		for _, value := range values {
			destination.Add(name, value)
		}
	}
}
