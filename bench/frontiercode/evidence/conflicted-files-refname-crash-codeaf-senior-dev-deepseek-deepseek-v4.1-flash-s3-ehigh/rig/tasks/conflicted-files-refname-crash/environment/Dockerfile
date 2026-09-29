# The task environment for the conflicted-files-refname-crash candidate.
# Same shape as the fixture's environment image: a git checkout of
# pre-commit/pre-commit pinned to the base commit with no future history, and
# the project installed editable so the agent tests immediately.
FROM python:3.12-slim

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
      git \
      curl \
      wget \
      ca-certificates \
      jq \
      ripgrep \
      tmux \
    && rm -rf /var/lib/apt/lists/*

# The task checkout: full clone, then pinned, then stripped of every future
# history reference — same recipe as the fixture's image.
ARG BASE_COMMIT=7b88c63ae691cb243c3137bce8fb870523e0a884
ARG REFERENCE_COMMIT=b7eb412c798424a94ca83c72eed6f97271545dc4
RUN git clone --no-tags https://github.com/pre-commit/pre-commit.git /root/repos/pre-commit \
    && cd /root/repos/pre-commit \
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
    && mv /root/repos/pre-commit /home/agent/repos/pre-commit \
    && ln -s /home/agent/repos/pre-commit /root/repos/pre-commit \
    && git config --system user.name "Bench Agent" \
    && git config --system user.email "bench-agent@example.invalid" \
    && git config --system --add safe.directory /home/agent/repos/pre-commit \
    && git config --system --add safe.directory /root/repos/pre-commit

ENV LANG=C.UTF-8 LC_ALL=C.UTF-8

# Prewarm: editable install plus the test dependencies. A prewarm failure is
# a BUILD failure — never swallowed.
RUN cd /home/agent/repos/pre-commit \
    && (pip install --no-cache-dir -e . && pip install --no-cache-dir pytest re-assert pytest-env) > /tmp/prewarm.log 2>&1; code=$?; \
    if [ "$code" -ne 0 ]; then echo "prewarm failed (exit $code), tail:" >&2; tail -40 /tmp/prewarm.log >&2; exit 1; fi \
    && if [ -n "$(git status --porcelain)" ]; then echo "prewarm edited the tree:" >&2; git status --porcelain >&2; exit 1; fi \
    && chown -R agent:agent /home/agent/repos/pre-commit /logs

WORKDIR /home/agent/repos/pre-commit
CMD ["/bin/bash"]
