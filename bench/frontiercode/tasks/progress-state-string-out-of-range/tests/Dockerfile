# The verifier image for the progress-state-string-out-of-range candidate.
# Built FROM the task environment image; the ONLY place the reference solution
# exists. Same recipe as the fixture's verifier image: clone upstream, pin the
# reference commit, extract the reference patch and the test-side overlay, and
# generate the negative control from the base tree's own text.
ARG BASE_IMAGE
FROM ${BASE_IMAGE}

ARG BASE_COMMIT=fc707bb7ea0161405bb6c653ec93f6a9c6a72fe1
ARG REFERENCE_COMMIT=d05da3e8ad90a7489535cc28298aafde07166a65
# The reference commit is the merged PR's HEAD; unless upstream merged with a
# fast-forward it is reachable only through GitHub's pull refs, so the clone
# is followed by an explicit fetch of that ref before pinning.
ARG PR_HEAD_REF=pull/1748/head

RUN git clone https://github.com/charmbracelet/bubbletea.git /solution/full \
    && cd /solution/full \
    && git fetch origin "$PR_HEAD_REF" \
    && git checkout "$REFERENCE_COMMIT" \
    && git config --global --add safe.directory /solution/full

RUN cd /solution/full \
    && git diff --binary "$BASE_COMMIT" "$REFERENCE_COMMIT" > /solution/upstream-reference.patch \
    && git diff "$BASE_COMMIT" "$REFERENCE_COMMIT" -- tea_test.go > /solution/test-overlay.patch \
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
