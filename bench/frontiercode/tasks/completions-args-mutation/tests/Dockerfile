# The verifier image for the completions-args-mutation candidate.
# Built FROM the task environment image; the ONLY place the reference solution
# exists. Same recipe as the fixture's verifier image: clone upstream, pin the
# reference commit, extract the reference patch and the test-side overlay, and
# generate the negative control from the base tree's own text.
ARG BASE_IMAGE
FROM ${BASE_IMAGE}

ARG BASE_COMMIT=61968e893eee2f27696c2fbc8e34fa5c4afaf7c4
ARG REFERENCE_COMMIT=a36b43475f0a868310fdce4ead976a0cfb8173ea
# The reference commit is the merged PR's HEAD; unless upstream merged with a
# fast-forward it is reachable only through GitHub's pull refs, so the clone
# is followed by an explicit fetch of that ref before pinning.
ARG PR_HEAD_REF=pull/2356/head

RUN git clone https://github.com/spf13/cobra.git /solution/full \
    && cd /solution/full \
    && git fetch origin "$PR_HEAD_REF" \
    && git checkout "$REFERENCE_COMMIT" \
    && git config --global --add safe.directory /solution/full

RUN cd /solution/full \
    && git diff "$BASE_COMMIT" "$REFERENCE_COMMIT" > /solution/upstream-reference.patch \
    && git diff "$BASE_COMMIT" "$REFERENCE_COMMIT" -- completions_test.go > /solution/test-overlay.patch \
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
