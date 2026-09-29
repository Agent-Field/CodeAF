# The verifier image for the conflicted-files-refname-crash candidate.
# Built FROM the task environment image; the ONLY place the reference solution
# exists. Same recipe as the fixture's verifier image, plus the explicit pull
# ref: unless upstream merged fast-forward, the PR head commit is reachable
# only through GitHub's pull refs.
ARG BASE_IMAGE
FROM ${BASE_IMAGE}

ARG BASE_COMMIT=7b88c63ae691cb243c3137bce8fb870523e0a884
ARG REFERENCE_COMMIT=b7eb412c798424a94ca83c72eed6f97271545dc4
ARG PR_HEAD_REF=pull/3425/head

RUN git clone https://github.com/pre-commit/pre-commit.git /solution/full \
    && cd /solution/full \
    && git fetch origin "$PR_HEAD_REF" \
    && git checkout "$REFERENCE_COMMIT" \
    && git config --global --add safe.directory /solution/full

RUN cd /solution/full \
    && git format-patch -1 "$REFERENCE_COMMIT" --stdout > /solution/upstream-reference.patch \
    && git diff "$BASE_COMMIT" "$REFERENCE_COMMIT" -- tests/git_test.py > /solution/test-overlay.patch \
    && test -s /solution/upstream-reference.patch \
    && test -s /solution/test-overlay.patch

# The negative control is generated against the BASE tree.
COPY make-negative.py /solution/full/../make-negative.py
RUN cd /solution/full \
    && git checkout "$BASE_COMMIT" \
    && python3 /solution/full/../make-negative.py \
        --base "$BASE_COMMIT" \
        --out /solution/negative-control.patch \
    && test -s /solution/negative-control.patch
