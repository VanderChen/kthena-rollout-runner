FROM busybox:1.36
COPY bin/rollout-runner /usr/local/bin/rollout-runner
COPY cases /cases
ENTRYPOINT ["/usr/local/bin/rollout-runner"]
