// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"kthena.local/rollout-runner/internal/runner"
)

func main() {
	var o runner.Options
	flag.StringVar(&o.FaultProxyAPI, "fault-proxy-api", "", "exact TLS API proxy URL configured in the tested controller")
	flag.StringVar(&o.FaultProxyControl, "fault-proxy-control", "", "authenticated external fault control URL")
	flag.StringVar(&o.FaultProxyTokenFile, "fault-proxy-token-file", "", "private control token file; never recorded in artifacts")
	flag.StringVar(&o.CaseDir, "cases", "cases/core", "directory of independent YAML cases")
	flag.StringVar(&o.OutDir, "artifacts", "artifacts", "persistent output directory")
	flag.StringVar(&o.Kubeconfig, "kubeconfig", "", "empty uses in-cluster service account")
	flag.StringVar(&o.RunID, "run-id", "", "unique attempt name; existing attempt is never overwritten")
	flag.StringVar(&o.Select, "select", "", "comma-separated IDs; empty executes all files")
	flag.StringVar(&o.ControllerImage, "controller-image", "", "required exact tested controller image")
	flag.StringVar(&o.ControllerCommit, "controller-commit", "", "tested controller source commit; recorded separately from catalogue baseline")
	flag.DurationVar(&o.Hold, "hold", 0, "override each case hold window; 0 keeps its declared duration")
	flag.DurationVar(&o.Timeout, "phase-timeout", 180*time.Second, "bounded wait for each expected transition")
	flag.Parse()
	if o.Hold < 0 {
		fmt.Fprintln(os.Stderr, "hold cannot be negative")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runner.Run(ctx, o); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
