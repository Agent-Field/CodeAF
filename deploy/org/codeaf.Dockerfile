# syntax=docker/dockerfile:1.10@sha256:865e5dd094beca432e8c0a1d5e1c465db5f998dca4e439981029b3b81fb39ed5
#
# The codeaf image. The organization server (`codeaf factory serve`), the
# factory runner and the hosted chats' sessionhost all run from it, picked by the
# first argument of the entrypoint (scripts/entrypoint.sh).
#
# WHERE THE BINARY COMES FROM (CODEAF_BINARY_FROM):
#   fetch   a PUBLISHED, PINNED codeaf build: the dev-channel release below, checked
#           against its sha256 before it enters the image. The default of this file
#           when it is built by hand; compose.yaml passes `source` instead, until the
#           release below has `codeaf sessionhost` (compose.yaml says how to tell).
#           To move to a newer build change all three of CODEAF_RELEASE and the two
#           checksums together (they are in that release's checksums.txt); changing the
#           strings also busts Docker's layer cache, which a floating "latest" would not.
#   source  built here from the source in the build context, with `make build`, the
#           same build the release makes. This is what the published image is
#           (.github/workflows/org-images.yml): it carries exactly the commit it is
#           labelled with, including a sessionhost no release has shipped yet. The
#           context must be the REPOSITORY ROOT:
#               docker build -f deploy/org/codeaf.Dockerfile --build-arg ORG_DIR=deploy/org \
#                   --build-arg CODEAF_BINARY_FROM=source --build-arg SOURCE_COMMIT=$(git rev-parse HEAD) .
#           codeaf.Dockerfile.dockerignore is deny-by-default, so only the files the build
#           needs enter it.
#   local   a codeaf built on this machine and placed beside this file as `codeaf-local`.
#               docker build --build-arg CODEAF_BINARY_FROM=local -f codeaf.Dockerfile -t codeaf-org/codeaf:local .
#
# Built by hand, the context for fetch and local is this folder:   docker build -f codeaf.Dockerfile -t codeaf-org .
# Compose builds all three modes from the repository root (ORG_DIR=deploy/org), because source needs it.
#
# EVERY BASE IMAGE IS PINNED BY DIGEST (tag kept for the reader). To move one, resolve the tag again
# (`docker buildx imagetools inspect <tag> --format '{{.Manifest.Digest}}'`) and change tag and digest together.

ARG CODEAF_RELEASE=dev-20261124-3791ee6467c7
ARG CODEAF_SHA256_AMD64=8ad946abcaadad4f753d2dd9095b08a099f97bfa9c09fb133857c36d31a32fe1
ARG CODEAF_SHA256_ARM64=f4e21cc0df14ac3ec3696331a6013020b4a322c5a295a66d101d60c986579c9a
ARG RUNTIME_IMAGE=debian:bookworm-slim@sha256:e67eeead736ff7bbe258ef088bff683635c561b1fcfefad0806c1df4ab49ce0e
ARG GO_IMAGE=golang:1.26-bookworm@sha256:4ebd099cae89439fff17af404a9c724079d439b4d92178819318735312ee9eb3
ARG CODEAF_BINARY_FROM=fetch
# Where deploy/org is inside the build context: `.` when the context is this folder, `deploy/org` when it is the repository root.
ARG ORG_DIR=.

FROM ${RUNTIME_IMAGE} AS fetch
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

# A locally built binary, when asked for (see CODEAF_BINARY_FROM above). Nothing is read from
# codeaf-local unless that stage is the one chosen.
FROM scratch AS local
ARG ORG_DIR
COPY ${ORG_DIR}/codeaf-local /out/codeaf

# The source build (see CODEAF_BINARY_FROM above). Nothing here runs unless this stage is the one chosen.
# CGO is off as in the release build, so the binary is static. The module cache is its own layer: it is
# rebuilt only when go.mod or go.sum change. SOURCE_COMMIT is public (a commit id), never a secret: it is
# stamped into `codeaf version`, and the image's revision label names the same commit.
FROM ${GO_IMAGE} AS source
ARG SOURCE_COMMIT=unknown
# Railway names the commit it builds RAILWAY_GIT_COMMIT_SHA and hands it to a Dockerfile build that declares it,
# so a service built there needs no SOURCE_COMMIT variable (one set to a Railway reference came through empty).
ARG RAILWAY_GIT_COMMIT_SHA=
ENV CGO_ENABLED=0 GOFLAGS=-buildvcs=false
WORKDIR /src
COPY go.mod go.sum ./
COPY third_party/raft/ ./third_party/raft/
RUN go mod download
COPY . .
# BUILD_REV and BUILD_DIRTY are given because there is no .git here to ask; make then never runs git.
RUN rev="${RAILWAY_GIT_COMMIT_SHA:-${SOURCE_COMMIT:-unknown}}" \
    && make build BUILD_REV="$(printf '%.10s' "$rev")" BUILD_DIRTY=false \
    && mkdir -p /out && cp bin/codeaf /out/codeaf && /out/codeaf version

# What the build context holds, for the check that no secret can enter it: build this target with
# `--output type=local,dest=<dir>` and list <dir> (deploy/org/gate/context-check.sh does).
FROM scratch AS context-probe
COPY . /ctx

# The stage whose /out/codeaf the runtime image takes. `COPY --from` does not expand a build argument,
# but `FROM` does, so the chosen stage is given one fixed name here.
FROM ${CODEAF_BINARY_FROM} AS binary

FROM ${RUNTIME_IMAGE} AS runtime
ARG ORG_DIR
# git: every checkout and branch the runner makes. python3: the runner's driver.
# openssh-client: git over ssh. tini: reaps the processes a harness leaves behind.
RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates curl git openssh-client python3 tini \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 10001 codeaf \
    && useradd --uid 10001 --gid 10001 --create-home --home-dir /home/codeaf --shell /bin/bash codeaf \
    && mkdir -p /data && chown codeaf:codeaf /data
COPY --from=binary /out/codeaf /usr/local/bin/codeaf
COPY ${ORG_DIR}/scripts/ /opt/org/
RUN chmod 0755 /opt/org/*.sh /opt/org/*.py
ENV HOME=/home/codeaf CODEAF_HOME=/data/codeaf \
    CODEAF_TELEMETRY=off DO_NOT_TRACK=1 CODEAF_NO_UPDATE_CHECK=1
WORKDIR /data
EXPOSE 8787
ENTRYPOINT ["/usr/bin/tini", "--", "/opt/org/entrypoint.sh"]
CMD ["server"]
