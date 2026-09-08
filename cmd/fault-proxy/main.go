// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"k8s.io/client-go/rest"
	"kthena.local/rollout-runner/internal/faultproxy"
)

func run() error {
	listen := flag.String("listen", ":8080", "controller API proxy address")
	control := flag.String("control-listen", ":8081", "authenticated test control address")
	tlsCert := flag.String("tls-cert", "", "API listener TLS certificate; required with --tls-key")
	tlsKey := flag.String("tls-key", "", "API listener TLS private key")
	upstream := flag.String("upstream", "", "fixed API server origin; defaults to in-cluster API server")
	ca := flag.String("upstream-ca", "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt", "trusted API server CA file")
	tokenFile := flag.String("control-token-file", "", "file containing test control token")
	journalPath := flag.String("journal", "", "new append-only execution evidence file")
	flag.Parse()
	if (*tlsCert == "") != (*tlsKey == "") {
		return fmt.Errorf("--tls-cert and --tls-key must be supplied together")
	}
	if *journalPath == "" || *tokenFile == "" {
		return fmt.Errorf("--journal and --control-token-file are required")
	}
	if *upstream == "" {
		config, err := rest.InClusterConfig()
		if err != nil {
			return err
		}
		*upstream = config.Host
	}
	origin, err := url.Parse(*upstream)
	if err != nil {
		return fmt.Errorf("invalid upstream origin")
	}
	// Only TLS trust is taken from the local configuration. Authentication is
	// forwarded from the actual caller, never replaced with the proxy account.
	transport, err := rest.TransportFor(&rest.Config{Host: *upstream, TLSClientConfig: rest.TLSClientConfig{CAFile: *ca}})
	if err != nil {
		return err
	}
	token, err := os.ReadFile(*tokenFile)
	if err != nil {
		return err
	}
	journal, err := os.OpenFile(*journalPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer journal.Close()
	proxy, err := faultproxy.New(origin, transport, strings.TrimSpace(string(token)), journal)
	if err != nil {
		return err
	}
	defer proxy.Close()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	api := &http.Server{Addr: *listen, Handler: proxy, ReadHeaderTimeout: 10 * time.Second}
	admin := &http.Server{Addr: *control, Handler: proxy.ControlHandler(), ReadHeaderTimeout: 10 * time.Second}
	errors := make(chan error, 2)
	go func() {
		if *tlsCert != "" {
			errors <- api.ListenAndServeTLS(*tlsCert, *tlsKey)
		} else {
			errors <- api.ListenAndServe()
		}
	}()
	go func() { errors <- admin.ListenAndServe() }()
	select {
	case <-ctx.Done():
	case err = <-errors:
		if err == http.ErrServerClosed {
			err = nil
		}
	}
	proxy.Close()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = api.Shutdown(shutdown)
	_ = admin.Shutdown(shutdown)
	_ = api.Close()
	_ = admin.Close()
	proxy.Wait()
	if err != nil {
		return err
	}
	return journal.Sync()
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
