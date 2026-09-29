# The verifier image for the dns-extra-records-lowercase candidate.
# Built FROM the task environment image; the ONLY place the reference solution
# exists. Same recipe as the fixture's verifier image: clone upstream, pin the
# reference commit, extract the reference patch and the test-side overlay, and
# generate the negative control from the base tree's own text. The reference
# patch takes the CODE and TEST files only — the PR also touches CHANGELOG.md,
# which is not part of the solution.
ARG BASE_IMAGE
FROM ${BASE_IMAGE}

ARG BASE_COMMIT=f3f6c982209b08fe2734c38832edf74468221069
ARG REFERENCE_COMMIT=aeb8d29fc967e3cb6cea3ffcc434a4dfa9cd3ca2
# The reference commit is the single-commit PR's HEAD; it is reachable only
# through GitHub's pull refs, so the clone is followed by an explicit fetch of
# that ref before pinning.
ARG PR_HEAD_REF=pull/3366/head

RUN git clone https://github.com/juanfont/headscale.git /solution/full \
    && cd /solution/full \
    && git fetch origin "$PR_HEAD_REF" \
    && git checkout "$REFERENCE_COMMIT" \
    && git config --global --add safe.directory /solution/full

RUN cd /solution/full \
    && git format-patch -1 "$REFERENCE_COMMIT" --stdout \
        -- hscontrol/types/config.go hscontrol/types/config_test.go \
        > /solution/upstream-reference.patch \
    && git diff "$BASE_COMMIT" "$REFERENCE_COMMIT" -- hscontrol/types/config_test.go \
        > /solution/test-overlay.patch \
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
