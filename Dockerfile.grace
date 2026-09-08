FROM busybox:1.36
COPY bin/rollout-runner-grace /usr/local/bin/rollout-runner
COPY cases/grace-restart /cases/grace-restart
ENTRYPOINT ["/usr/local/bin/rollout-runner"]
