# syntax=docker/dockerfile:1.10@sha256:865e5dd094beca432e8c0a1d5e1c465db5f998dca4e439981029b3b81fb39ed5
#
# The AgentField control plane, from the PUBLISHED image, pinned by tag and digest.
#
# That image is built with `-tags "embedded sqlite_fts5"` (see AgentField's
# deployments/docker/Dockerfile.control-plane), which matters: a control plane
# built without sqlite_fts5 registers nodes happily and then fails every
# execution insert with "no such module: fts5". Do not swap in a binary built
# another way.
#
# The base image is distroless, so it has no shell and no curl for a health check, and it starts as
# an unprivileged user that cannot write a root-owned platform volume. This file adds one statically
# linked (musl) busybox binary and an entrypoint that hands the data folder to that user and then drops
# to it: the control plane itself never runs as root.
#
# Build context is this folder:   docker build -f agentfield.Dockerfile -t agentfield-org .
# To move to a newer control plane change the tag and the digest together.

ARG AGENTFIELD_IMAGE=agentfield/control-plane:staging-0.1.147-rc.11@sha256:1c85c2f1cf53239b1a68b0fb3df772c6e77dd18292071bd98829259261be7d15

FROM busybox:1.37.0-musl@sha256:f6a95d611e0a8780fabdb2aec43e31275b2720dfefa3e41b16835bfc42454478 AS busybox

FROM ${AGENTFIELD_IMAGE}
COPY --from=busybox /bin/busybox /usr/local/bin/busybox
COPY --chmod=0755 scripts/agentfield-entrypoint.sh /usr/local/bin/agentfield-entrypoint.sh
USER 0:0
ENTRYPOINT ["/usr/local/bin/agentfield-entrypoint.sh"]
CMD ["server", "--open=false"]
HEALTHCHECK --interval=15s --timeout=5s --start-period=20s --retries=5 \
    CMD ["/usr/local/bin/busybox", "wget", "-q", "-O", "/dev/null", "http://127.0.0.1:8080/api/v1/health"]
