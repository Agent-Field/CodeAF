# The task environment for the jsonschema-log-warning fixture.
#
# What a FrontierCode-style task hands the agent: a git checkout of the task's
# repository pinned to the base commit, with NO future history — no origin
# remote, no tags, no branches past the base commit, no reflog entries that
# name the fix — and the project's own build prerequisites installed, so the
# agent starts compiling and testing without installing anything.
#
# The repository is placed at /root/repos/jsonschema, which is the layout the
# graded build command uses. The image is built for the host's own
# architecture by the rig (lib.sh picks the platform), so on this arm64
# workstation it runs natively and needs no emulation guard.
#
# Licensing: no upstream source bytes are committed to the rig. The tree is
# cloned from upstream at build time and pinned to one commit. The reference
# solution is fetched only into the VERIFIER image (tests/Dockerfile), which
# the agent never sees.
FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive

# Build prerequisites. gcc-13 (build-essential) compiles the project warning
# clean under -DCMAKE_COMPILE_WARNING_AS_ERROR=ON, which upstream's own CI
# exercises on Linux. clang-format is pinned to the version the project's
# cmake module fetches its pre-built binary for (20.1.6) so the format check
# judges the tree by the same style rules upstream does; the cmake module's
# own download path is a build-time convenience and the PATH binary is the
# fallback the project's Find module already allows.
RUN apt-get update && apt-get install -y --no-install-recommends \
      build-essential \
      cmake \
      make \
      ninja-build \
      ccache \
      git \
      curl \
      wget \
      ca-certificates \
      shellcheck \
      python3 \
      python3-pip \
      jq \
      ripgrep \
      tmux \
    && rm -rf /var/lib/apt/lists/* \
    && pip3 install --break-system-packages --no-cache-dir clang-format==20.1.6

# The task checkout: full clone, then pinned, then stripped of every future
# history reference. The clone leaves a local branch at the remote's tip —
# the future — which is deleted first, with the fetch bookkeeping files, and
# `git gc --prune=now` then drops the commits past the base so not even a
# packed ref carries the fix. The history BELOW the base commit stays: that is
# the repository's real past and part of the environment.
ARG BASE_COMMIT
ARG REFERENCE_COMMIT
RUN git clone --no-tags https://github.com/sourcemeta/jsonschema.git /root/repos/jsonschema \
    && cd /root/repos/jsonschema \
    && git checkout "$BASE_COMMIT" \
    && git for-each-ref --format='%(refname:short)' refs/heads | xargs git branch -D \
    && git remote remove origin \
    && rm -f .git/FETCH_HEAD .git/ORIG_HEAD \
    && git reflog expire --all --expire=now --expire-unreachable=now \
    && git gc --prune=now --quiet \
    && git config --global --add safe.directory /root/repos/jsonschema \
    && git config user.name "Bench Agent" \
    && git config user.email "bench-agent@example.invalid" \
    && git rev-parse HEAD | grep -qx "$BASE_COMMIT" \
    && ! git cat-file -e "$REFERENCE_COMMIT" 2>/dev/null \
    && ! git for-each-ref --format='%(refname)' | grep -q .

# The agent's shell should feel like a workstation, not a bare container.
ENV LANG=C.UTF-8 LC_ALL=C.UTF-8

# Prewarm the base tree's build so a grader that re-runs the build on a patched
# tree pays only the incremental cost. The tree stays byte-identical to the
# base commit afterwards (build/ is in the project's own .gitignore), which
# the build asserts so a prewarm that edited sources can never ship.
RUN cd /root/repos/jsonschema \
    && make configure compile > /tmp/prewarm.log 2>&1 \
    && git status --porcelain | grep -q . && exit 1 || true

WORKDIR /root/repos/jsonschema
CMD ["/bin/bash"]