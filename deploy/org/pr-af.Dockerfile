# syntax=docker/dockerfile:1.10@sha256:865e5dd094beca432e8c0a1d5e1c465db5f998dca4e439981029b3b81fb39ed5
#
# PR-AF, the pull-request review reasoner node, built from a PINNED commit of its
# public repository, with codeaf available as the review harness. The control
# plane reaches this node over HTTP at AGENT_CALLBACK_URL.
#
# Build context is this folder:   docker build -f pr-af.Dockerfile -t pr-af-org .
# To move to another PR-AF commit change PRAF_REF (a full 40-character sha).

ARG PRAF_REF=c3009ec360658ad6b8b1cf6b8e773de909c6008d
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
ARG PRAF_REF
ENV CGO_ENABLED=0 GOFLAGS=-buildvcs=false
RUN git init -q /src && cd /src \
    && git remote add origin https://github.com/Agent-Field/pr-af.git \
    && git fetch --depth 1 origin "${PRAF_REF}" \
    && git checkout -q FETCH_HEAD \
    && test "$(git rev-parse HEAD)" = "${PRAF_REF}"
WORKDIR /src/go
RUN go mod download
RUN go build -trimpath -ldflags="-s -w" -o /out/pr-af ./cmd/pr-af

FROM debian:bookworm-slim@sha256:e67eeead736ff7bbe258ef088bff683635c561b1fcfefad0806c1df4ab49ce0e AS runtime
# git: reviews clone, fetch and check out the pull request. tini: reaps what a harness leaves behind.
RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates curl git tini \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 10003 praf \
    && useradd --uid 10003 --gid 10003 --create-home --home-dir /home/praf --shell /bin/sh praf \
    && mkdir -p /workspaces && chown praf:praf /workspaces
COPY --from=build /out/pr-af /usr/local/bin/pr-af
COPY --from=fetch /out/codeaf /usr/local/bin/codeaf
COPY scripts/node-entrypoint.sh scripts/drop-root.sh /opt/org/
RUN chmod 0755 /opt/org/node-entrypoint.sh
# legacy-name: AgentField's SDKs still call the codeaf coding harness "aforge" (the provider
# and its command variable), and CODEAF_BIN below says which binary it runs. Both spellings are theirs.
# ORG_*: who the entrypoint hands the platform volume to, and runs the node as (scripts/drop-root.sh).
ENV ORG_UID=10003 ORG_HOME=/home/praf ORG_DIRS=/workspaces \
    HOME=/home/praf \
    NODE_ID=pr-af \
    PORT=8007 \
    PR_AF_WORKDIR=/workspaces \
    PR_AF_PROVIDER=aforge \
    AGENTFIELD_AFORGE_COMMAND=exec \
    CODEAF_BIN=/usr/local/bin/codeaf \
    CODEAF_TELEMETRY=off DO_NOT_TRACK=1 CODEAF_NO_UPDATE_CHECK=1
WORKDIR /workspaces
EXPOSE 8007
ENTRYPOINT ["/usr/bin/tini", "--", "/opt/org/node-entrypoint.sh"]
CMD ["/usr/local/bin/pr-af"]
