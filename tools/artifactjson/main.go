// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

// artifactjson converts saved YAML evidence to JSON using the runner's existing
// YAML dependency. It performs no Kubernetes requests or artifact modifications.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"sigs.k8s.io/yaml"
)

func main() {
	out := make(map[string]json.RawMessage)
	for _, path := range os.Args[1:] {
		key := filepath.Base(path)
		if _, exists := out[key]; exists {
			panic("duplicate artifact filename: " + key)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			panic(err)
		}
		out[key], err = yaml.YAMLToJSONStrict(data)
		if err != nil {
			panic(err)
		}
	}
	data, err := json.Marshal(out)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(data))
}
