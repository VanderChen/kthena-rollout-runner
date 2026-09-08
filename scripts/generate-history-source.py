#!/usr/bin/env python3
# Copyright 2026 The Kthena Rollout Runner Authors.
# SPDX-License-Identifier: Apache-2.0
"""Collect the five actual mixed Role history sources for a bounded supplement."""
import hashlib
import json
import pathlib

ROOT=pathlib.Path(__file__).resolve().parents[1]
out=ROOT/'cases/history-source';out.mkdir(exist_ok=True)
rows=[];inputs={}
for n in (528,529,530,531,532):
    source=ROOT/'cases'/('history-read' if n==529 else 'history-object')
    manifest=json.loads((source/'suite.json').read_text())
    caseid=f'RUN-{n}';raw=(source/(caseid+'.yaml')).read_bytes()
    (out/(caseid+'.yaml')).write_bytes(raw)
    inputs[caseid]=hashlib.sha256(raw).hexdigest()
    rows.append(next(r for r in manifest['cases'] if r['id']==caseid))
manifest.update(developmentScope='Five Role historical-fault sources: actual protected A and eligible B, preparation-only process replacement and finite orphan cleanup, then complete stable source before historical fault. Earlier preparation cleanup findings remain recorded.',cases=rows,inputs=inputs)
(out/'suite.json').write_text(json.dumps(manifest,ensure_ascii=False,indent=2)+'\n')
print('Generated five mixed Role historical fault supplements.')
