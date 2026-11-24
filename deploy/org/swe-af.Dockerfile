# syntax=docker/dockerfile:1.10@sha256:865e5dd094beca432e8c0a1d5e1c465db5f998dca4e439981029b3b81fb39ed5
#
# SWE-AF, the build-an-issue-into-a-pull-request reasoner node (`swe-planner`),
# built from a PINNED commit of its public repository, with codeaf as its
# coding harness. It dials the AgentField control plane over the node link, so
# it needs no address of its own.
#
# Build context is this folder:   docker build -f swe-af.Dockerfile -t swe-af-org .
# To move to another SWE-AF commit change SWEAF_REF (a full 40-character sha).

ARG SWEAF_REF=0de536488f279c982fc03f5f5660011a7501e0cb
ARG CODEAF_RELEASE=dev-20261124-3791ee6467c7
ARG CODEAF_SHA256_AMD64=8ad946abcaadad4f753d2dd9095b08a099f97bfa9c09fb133857c36d31a32fe1
ARG CODEAF_SHA256_ARM64=f4e21cc0df14ac3ec3696331a6013020b4a322c5a295a66d101d60c986579c9a

FROM debian:bookworm-slim@sha256:e67eeead736ff7bbe258ef088bff683635c561b1fcfefad0806c1df4ab49ce0e AS fetch
ARG CODEAF_RELEASE
ARG CODEAF_SHA256_AMD64
ARG CODEAF_SHA256_ARM64
ARG TARGETARCH
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /out
RUN set -eu; \
    arch="${TARGETARCH:-$(dpkg --print-architecture)}"; \
    case "$arch" in amd64) sum="$CODEAF_SHA256_AMD64";; arm64) sum="$CODEAF_SHA256_ARM64";; *) echo "unsupported architecture: $arch" >&2; exit 1;; esac; \
    curl -fsSL "https://github.com/Agent-Field/codeaf/releases/download/${CODEAF_RELEASE}/codeaf-linux-${arch}" -o codeaf; \
    echo "${sum}  codeaf" | sha256sum -c -; \
    chmod 0755 codeaf; \
    ./codeaf version

FROM golang:1.23-bookworm@sha256:167053a2bb901972bf2c1611f8f52c44d5fe7e762e5cab213708d82c421614db AS build
ARG SWEAF_REF
ENV CGO_ENABLED=0 GOFLAGS=-buildvcs=false
RUN git init -q /src && cd /src \
    && git remote add origin https://github.com/Agent-Field/SWE-AF.git \
    && git fetch --depth 1 origin "${SWEAF_REF}" \
    && git checkout -q FETCH_HEAD \
    && test "$(git rev-parse HEAD)" = "${SWEAF_REF}"
WORKDIR /src/go
RUN go mod download
RUN go build -trimpath -ldflags="-s -w" -o /out/swe-planner ./cmd/swe-planner

FROM debian:bookworm-slim@sha256:e67eeead736ff7bbe258ef088bff683635c561b1fcfefad0806c1df4ab49ce0e AS runtime
# git and gh: clone, branch, and open the pull request. python3 and pytest: repositories
# under build are often Python and the verifier has to be able to run their tests.
# jq: the agents' shell steps. tini: reaps what a harness leaves behind.
RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates curl git gh jq openssh-client python3 python3-pytest tini \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 10002 sweaf \
    && useradd --uid 10002 --gid 10002 --create-home --home-dir /home/sweaf --shell /bin/bash sweaf \
    && mkdir -p /workspaces && chown sweaf:sweaf /workspaces
COPY --from=build /out/swe-planner /usr/local/bin/swe-planner
COPY --from=fetch /out/codeaf /usr/local/bin/codeaf
COPY scripts/node-entrypoint.sh scripts/drop-root.sh /opt/org/
RUN chmod 0755 /opt/org/node-entrypoint.sh
# legacy-name: AgentField's SDKs still call the codeaf coding harness "aforge" (the provider
# and its command variable), and CODEAF_BIN below says which binary it runs. Both spellings are theirs.
# ORG_*: who the entrypoint hands the platform volume to, and runs the node as (scripts/drop-root.sh).
ENV ORG_UID=10002 ORG_HOME=/home/sweaf ORG_DIRS=/workspaces \
    HOME=/home/sweaf \
    NODE_ID=swe-planner \
    AGENTFIELD_NODE_LINK_ONLY=1 \
    SWE_WORKSPACE_ROOT=/workspaces \
    SWE_DEFAULT_RUNTIME=aforge \
    AGENTFIELD_AFORGE_COMMAND=exec \
    CODEAF_BIN=/usr/local/bin/codeaf \
    CODEAF_TELEMETRY=off DO_NOT_TRACK=1 CODEAF_NO_UPDATE_CHECK=1
WORKDIR /workspaces
ENTRYPOINT ["/usr/bin/tini", "--", "/opt/org/node-entrypoint.sh"]
CMD ["/usr/local/bin/swe-planner"]
