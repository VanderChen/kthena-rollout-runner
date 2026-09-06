# Kthena rolling-update runner

Internal development/CI tool: one Go process in a Kubernetes Job. First scope:
RUN-001 through RUN-060 from feature020, against production/release-1.0 at
e2578d01859bb98d9a85846bafbfb2c771a6f117. No controller implementation is imported.

Implementation and Kind verification are in progress. This file will include
reproducible build/run instructions and actual verification results before completion.

Each case preserves raw omissions and explicit values. New-version workloads start
NotReady. The runner observes Pod, ModelServing, Volcano PodGroup and revision
changes, checks every destructive start, and releases Ready one logical unit at a
time. Process violations remain failures even if the final state is healthy.
