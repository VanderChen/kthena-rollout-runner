// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

// contract-audit inventories historical accepted inputs that conflict with the
// reviewed current contract. It does not contact Kubernetes or rewrite cases.
package main

import (
	"encoding/json"
	"fmt"
	"kthena.local/rollout-runner/internal/runner"
	"os"
	"path/filepath"
)

func main() {
	root := "cases"
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	dirs, err := os.ReadDir(root)
	if err != nil {
		panic(err)
	}
	conflicts := map[string][]string{}
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		path := filepath.Join(root, dir.Name())
		run, _ := filepath.Glob(filepath.Join(path, "RUN-*.yaml"))
		deny, _ := filepath.Glob(filepath.Join(path, "DENY-*.yaml"))
		if len(run)+len(deny) == 0 {
			continue
		}
		cases, err := runner.LoadCases(path)
		if err != nil {
			panic(err)
		}
		for _, c := range cases {
			if reasons := runner.ContractConflicts(c); len(reasons) > 0 {
				conflicts[dir.Name()+"/"+c.ID] = reasons
			}
		}
	}
	b, err := json.MarshalIndent(conflicts, "", "  ")
	if err != nil {
		panic(err)
	}
	fmt.Println(string(b))
}
