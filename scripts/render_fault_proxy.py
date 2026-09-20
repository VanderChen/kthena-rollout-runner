#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Render a fault-proxy Pod and Service with an explicit image address."""

import argparse
import json
from pathlib import Path
import sys

from render_job import name


def build_proxy(args):
    proxy_name = name(args.name, "fault-proxy name")
    namespace = name(args.namespace, "namespace")
    labels = {"app.kubernetes.io/name": "kthena-fault-proxy", "app.kubernetes.io/instance": proxy_name}
    pod = {"apiVersion": "v1", "kind": "Pod", "metadata": {"name": proxy_name, "namespace": namespace,
                                                               "labels": labels},
           "spec": {"serviceAccountName": "rollout-runner", "restartPolicy": "Never",
                    "containers": [{"name": "proxy", "image": args.proxy_image,
                                    "imagePullPolicy": args.image_pull_policy,
                                    "args": ["--control-token-file=/control/token", "--journal=/evidence/trace.jsonl",
                                             "--tls-cert=/tls/tls.crt", "--tls-key=/tls/tls.key"],
                                    "ports": [{"name": "api", "containerPort": 8080},
                                              {"name": "control", "containerPort": 8081}],
                                    "readinessProbe": {"httpGet": {"path": "/healthz", "port": "api",
                                                                   "scheme": "HTTPS"}, "periodSeconds": 2},
                                    "resources": {"requests": {"cpu": "25m", "memory": "64Mi"},
                                                  "limits": {"memory": "256Mi"}},
                                    "volumeMounts": [{"name": "control", "mountPath": "/control", "readOnly": True},
                                                     {"name": "tls", "mountPath": "/tls", "readOnly": True},
                                                     {"name": "evidence", "mountPath": "/evidence"}]}],
                    "volumes": [{"name": "control", "secret": {"secretName": args.token_secret,
                                                                "defaultMode": 256}},
                                {"name": "tls", "secret": {"secretName": args.tls_secret,
                                                            "defaultMode": 256}},
                                {"name": "evidence", "emptyDir": {}}]}}
    service = {"apiVersion": "v1", "kind": "Service", "metadata": {"name": proxy_name,
                                                                     "namespace": namespace},
               "spec": {"selector": labels,
                        "ports": [{"name": "api", "port": 8080, "targetPort": "api"},
                                  {"name": "control", "port": 8081, "targetPort": "control"}]}}
    return {"apiVersion": "v1", "kind": "List", "items": [pod, service]}


def main(argv=None):
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--proxy-image", required=True, help="exact fault-proxy image address/tag")
    p.add_argument("--name", required=True, help="unique Pod/Service name for this experiment")
    p.add_argument("--namespace", default="rollout-runner")
    p.add_argument("--token-secret", required=True, help="Secret with key token")
    p.add_argument("--tls-secret", required=True, help="TLS Secret with tls.crt and tls.key")
    p.add_argument("--image-pull-policy", choices=("Always", "IfNotPresent", "Never"), default="IfNotPresent")
    p.add_argument("--out", required=True, type=Path)
    args = p.parse_args(argv)
    try:
        manifest = build_proxy(args)
        args.out.parent.mkdir(parents=True, exist_ok=True)
        args.out.write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
    except (OSError, ValueError) as exc:
        print(exc, file=sys.stderr)
        return 2
    print(f"FAULT_PROXY {args.namespace}/{args.name} image={args.proxy_image} -> {args.out}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
