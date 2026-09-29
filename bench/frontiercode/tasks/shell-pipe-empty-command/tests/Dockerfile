# The verifier image for the shell-pipe-empty-command candidate.
# Built FROM the task environment image; the ONLY place the reference solution
# exists. Same recipe as the fixture's verifier image: clone upstream, pin the
# reference commit, extract the reference patch and the test-side overlay, and
# generate the negative control from the base tree's own text.
ARG BASE_IMAGE
FROM ${BASE_IMAGE}

ARG BASE_COMMIT=21eefb16cfc5346f9cbe8fb0513ee1840353c324
ARG REFERENCE_COMMIT=1d569f4a85c9477f0fc2e13508a82ace8b124c90

RUN git clone https://github.com/goreleaser/goreleaser.git /solution/full \
    && cd /solution/full \
    && git checkout "$REFERENCE_COMMIT" \
    && git config --global --add safe.directory /solution/full

RUN cd /solution/full \
    && git format-patch -1 "$REFERENCE_COMMIT" --stdout > /solution/upstream-reference.patch \
    && git diff "$BASE_COMMIT" "$REFERENCE_COMMIT" -- internal/shell/shell_test.go > /solution/test-overlay.patch \
    && test -s /solution/upstream-reference.patch \
    && test -s /solution/test-overlay.patch

COPY make-negative.py /solution/make-negative.py
RUN cd /solution/full \
    && python3 /solution/make-negative.py \
        --base "$BASE_COMMIT" \
        --out /solution/negative-control.patch \
    && test -s /solution/negative-control.patch
