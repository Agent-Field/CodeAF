# syntax=docker/dockerfile:1.10
#
# The codeaf image. The organization server (`codeaf factory serve`) and the
# factory runner both run from it, picked by the first argument of the
# entrypoint (scripts/entrypoint.sh).
#
# It runs a PUBLISHED, PINNED codeaf build: the dev-channel release below, checked
# against its sha256 before it enters the image. That is the same binary people
# download, with the manuals packed in. To move to a newer build, change all
# three of CODEAF_RELEASE and the two checksums together (they are in that
# release's checksums.txt); changing the strings also busts Docker's layer cache,
# which a floating "latest" would not.
#
# Build context is this folder:   docker build -f codeaf.Dockerfile -t codeaf-org .

ARG CODEAF_RELEASE=dev-20261124-3791ee6467c7
ARG CODEAF_SHA256_AMD64=8ad946abcaadad4f753d2dd9095b08a099f97bfa9c09fb133857c36d31a32fe1
ARG CODEAF_SHA256_ARM64=f4e21cc0df14ac3ec3696331a6013020b4a322c5a295a66d101d60c986579c9a
ARG RUNTIME_IMAGE=debian:bookworm-slim
# Where the binary comes from: `fetch` (the published, checksummed release above, the default) or
# `local`, a codeaf built on this machine and placed beside this file as `codeaf-local`. Only the
# second is for a build that needs a command no release has yet (the hosted-chats service, until a
# release carries it):
#   docker build --build-arg CODEAF_BINARY_FROM=local -f codeaf.Dockerfile -t codeaf-org/codeaf:local .
ARG CODEAF_BINARY_FROM=fetch

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
COPY codeaf-local /out/codeaf

# The stage whose /out/codeaf the runtime image takes. `COPY --from` does not expand a build argument,
# but `FROM` does, so the chosen stage is given one fixed name here.
FROM ${CODEAF_BINARY_FROM} AS binary

FROM ${RUNTIME_IMAGE} AS runtime
# git: every checkout and branch the runner makes. python3: the runner's driver.
# openssh-client: git over ssh. tini: reaps the processes a harness leaves behind.
RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates curl git openssh-client python3 tini \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 10001 codeaf \
    && useradd --uid 10001 --gid 10001 --create-home --home-dir /home/codeaf --shell /bin/bash codeaf \
    && mkdir -p /data && chown codeaf:codeaf /data
COPY --from=binary /out/codeaf /usr/local/bin/codeaf
COPY scripts/ /opt/org/
RUN chmod 0755 /opt/org/*.sh /opt/org/*.py
ENV HOME=/home/codeaf CODEAF_HOME=/data/codeaf \
    CODEAF_TELEMETRY=off DO_NOT_TRACK=1 CODEAF_NO_UPDATE_CHECK=1
WORKDIR /data
EXPOSE 8787
ENTRYPOINT ["/usr/bin/tini", "--", "/opt/org/entrypoint.sh"]
CMD ["server"]
