# The task environment for the mangen-hidden-positionals candidate.
# Same shape as the shell-pipe-empty-command's environment image: a git
# checkout of clap-rs/clap pinned to the base commit with no future history,
# and the build prerequisites installed. This one is Rust: a pinned stable
# toolchain from rustup (the tree's MSRV is 1.85; the pin matches it), so the
# image is slower and larger than the Go fixtures' — expected.
# No upstream source bytes are committed to the rig; the tree is cloned from
# upstream at build time.
FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive

# Build prerequisites: git, curl, ca-certs, a C toolchain (rustc needs a
# linker), plus the common agent toolbelt the fixture image carries.
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
      file \
    && rm -rf /var/lib/apt/lists/*

# The Rust toolchain: rustup with a PINNED stable toolchain (1.85.0, the
# tree's MSRV), not the floating default — a drifted toolchain must fail the
# build, not the task.
ENV RUSTUP_HOME=/usr/local/rustup \
    CARGO_HOME=/usr/local/cargo
ENV PATH="/usr/local/cargo/bin:${PATH}"
ARG RUST_VERSION=1.85.0
RUN curl -fsSL https://sh.rustup.rs | sh -s -- -y \
      --default-toolchain "$RUST_VERSION" --profile minimal \
    && rustc --version | grep -q "rustc $RUST_VERSION "

# The task checkout: full clone, then pinned, then stripped of every future
# history reference — same recipe as the fixture's image.
ARG BASE_COMMIT=8e50ec891e30cd2af4f9a62a405a5b7ee31aac4f
ARG REFERENCE_COMMIT=07c6fd2d7cef1601d0308d51dff9d695ff790bbc
RUN git clone --no-tags https://github.com/clap-rs/clap.git /root/repos/clap \
    && cd /root/repos/clap \
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
    && mv /root/repos/clap /home/agent/repos/clap \
    && ln -s /home/agent/repos/clap /root/repos/clap \
    && git config --system user.name "Bench Agent" \
    && git config --system user.email "bench-agent@example.invalid" \
    && git config --system --add safe.directory /home/agent/repos/clap

ENV LANG=C.UTF-8 LC_ALL=C.UTF-8

# Prewarm the cargo registry and the man-page crate (deps and tests) so the
# agent starts with a warm tree. A prewarm failure is a BUILD failure — never
# swallowed.
RUN cd /home/agent/repos/clap \
    && (cargo build -p clap_mangen && cargo test -p clap_mangen --no-run) \
        > /logs/prewarm.log 2>&1; code=$?; \
    if [ "$code" -ne 0 ]; then echo "prewarm failed (exit $code), tail:" >&2; tail -40 /logs/prewarm.log >&2; exit 1; fi \
    && if [ -n "$(git status --porcelain)" ]; then echo "prewarm edited the tree:" >&2; git status --porcelain >&2; exit 1; fi \
    && chown -R agent:agent /home/agent/repos/clap /logs

WORKDIR /home/agent/repos/clap
CMD ["/bin/bash"]
