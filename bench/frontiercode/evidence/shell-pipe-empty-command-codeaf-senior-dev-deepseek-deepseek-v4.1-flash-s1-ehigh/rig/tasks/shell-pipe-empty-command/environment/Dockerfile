# The task environment for the shell-pipe-empty-command candidate.
# Same shape as the fixture's environment image: a git checkout of
# goreleaser/goreleaser pinned to the base commit with no future history, and
# the build prerequisites installed. No upstream source bytes are committed to
# the rig; the tree is cloned from upstream at build time.
FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive

# Build prerequisites: git, curl, ca-certs, plus the common agent toolbelt the
# fixture image carries. The Go toolchain is installed from the official
# tarball (the distro's golang is too old for this tree's go.mod).
ARG TARGETARCH

RUN apt-get update && apt-get install -y --no-install-recommends \
      build-essential \
      git \
      curl \
      wget \
      ca-certificates \
      python3 \
      python3-pip \
      jq \
      ripgrep \
      tmux \
      xz-utils \
    && rm -rf /var/lib/apt/lists/* \
    && curl -fsSL https://go.dev/dl/go1.24.5.linux-${TARGETARCH}.tar.gz | tar -C /usr/local -xz
ENV PATH="/usr/local/go/bin:${PATH}"
ENV GOTOOLCHAIN=local

# The task checkout: full clone, then pinned, then stripped of every future
# history reference — same recipe as the fixture's image.
ARG BASE_COMMIT=21eefb16cfc5346f9cbe8fb0513ee1840353c324
ARG REFERENCE_COMMIT=1d569f4a85c9477f0fc2e13508a82ace8b124c90
RUN git clone --no-tags https://github.com/goreleaser/goreleaser.git /root/repos/goreleaser \
    && cd /root/repos/goreleaser \
    && git checkout "$BASE_COMMIT" \
    && git for-each-ref --format='%(refname:short)' refs/heads | xargs git branch -D \
    && git remote remove origin \
    && rm -f .git/FETCH_HEAD .git/ORIG_HEAD \
    && git reflog expire --all --expire=now --expire-unreachable=now \
    && git gc --prune=now --quiet \
    && git rev-parse HEAD | grep -qx "$BASE_COMMIT" \
    && ! git cat-file -e "$REFERENCE_COMMIT" 2>/dev/null \
    && ! git for-each-ref --format='%(refname)' | grep -q .

# The harness-user layout: the repository in the agent's home, /root path a
# symlink, git identity system-wide.
RUN useradd -m -s /bin/bash agent \
    && mkdir -p /home/agent/repos /logs \
    && mv /root/repos/goreleaser /home/agent/repos/goreleaser \
    && ln -s /home/agent/repos/goreleaser /root/repos/goreleaser \
    && git config --system user.name "Bench Agent" \
    && git config --system user.email "bench-agent@example.invalid" \
    && git config --system --add safe.directory /home/agent/repos/goreleaser \
    && git config --system --add safe.directory /root/repos/goreleaser

ENV LANG=C.UTF-8 LC_ALL=C.UTF-8

# Prewarm the module cache and the build so the agent starts with a warm tree.
# A prewarm failure is a BUILD failure — never swallowed.
RUN cd /home/agent/repos/goreleaser \
    && (go mod download && go build ./...) > /tmp/prewarm.log 2>&1; code=$?; \
    if [ "$code" -ne 0 ]; then echo "prewarm failed (exit $code), tail:" >&2; tail -40 /tmp/prewarm.log >&2; exit 1; fi \
    && if [ -n "$(git status --porcelain)" ]; then echo "prewarm edited the tree:" >&2; git status --porcelain >&2; exit 1; fi \
    && chown -R agent:agent /home/agent/repos/goreleaser /logs

WORKDIR /home/agent/repos/goreleaser
CMD ["/bin/bash"]
