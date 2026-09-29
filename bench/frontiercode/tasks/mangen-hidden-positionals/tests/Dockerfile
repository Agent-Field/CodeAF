# The verifier image for the mangen-hidden-positionals candidate.
# Built FROM the task environment image; the ONLY place the reference solution
# exists. Same recipe as the fixture's verifier image: clone upstream, pin the
# reference commit, extract the reference patch and the test-side overlay, and
# generate the negative control from the base tree's own text.
ARG BASE_IMAGE
FROM ${BASE_IMAGE}

ARG BASE_COMMIT=8e50ec891e30cd2af4f9a62a405a5b7ee31aac4f
ARG REFERENCE_COMMIT=07c6fd2d7cef1601d0308d51dff9d695ff790bbc
# The reference commit is the merged PR's HEAD; unless upstream merged with a
# fast-forward it is reachable only through GitHub's pull refs, so the clone
# is followed by an explicit fetch of that ref before pinning. The PR is
# test-first with TWO commits (the test lands before the fix), so the
# reference patch spans the whole PR — a single-commit format-patch of the
# head would not apply to the base, whose snapshot the fix commit edits.
ARG PR_HEAD_REF=pull/6482/head

RUN git clone https://github.com/clap-rs/clap.git /solution/full \
    && cd /solution/full \
    && git fetch origin "$PR_HEAD_REF" \
    && git checkout "$REFERENCE_COMMIT" \
    && git config --global --add safe.directory /solution/full

RUN cd /solution/full \
    && git format-patch "$BASE_COMMIT..$REFERENCE_COMMIT" --stdout > /solution/upstream-reference.patch \
    && git diff "$BASE_COMMIT" "$REFERENCE_COMMIT" -- clap_mangen/tests > /solution/test-overlay.patch \
    && test -s /solution/upstream-reference.patch \
    && test -s /solution/test-overlay.patch

# The negative control is generated against the BASE tree (the reference tree
# already carries the fix the control must not carry).
COPY make-negative.py /solution/make-negative.py
RUN cd /solution/full \
    && git checkout "$BASE_COMMIT" \
    && python3 /solution/make-negative.py \
        --base "$BASE_COMMIT" \
        --out /solution/negative-control.patch \
    && test -s /solution/negative-control.patch
