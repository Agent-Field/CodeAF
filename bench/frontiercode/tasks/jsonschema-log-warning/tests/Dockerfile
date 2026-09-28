# The verifier image for the jsonschema-log-warning fixture.
#
# Built FROM the task environment image, which the agent runs in. This image is
# what the grader runs, and it is the ONLY place the reference solution ever
# exists: it fetches the task's pinned upstream reference commit at build time,
# extracts the reference patch and the test-side overlay from it, and ships
# them under /solution/. The agent's container never carries this image.
#
# Licensing: no upstream source bytes are committed to the rig — the tree, the
# reference patch and the overlay are all fetched or generated at image build
# time from the pinned commits recorded in task.toml.
ARG BASE_IMAGE
FROM ${BASE_IMAGE}

ARG BASE_COMMIT
ARG REFERENCE_COMMIT

# A full clone beside the task tree, pinned to the reference commit, is where
# the reference patch, the test overlay and the negative control come from.
# The task tree at /root/repos/jsonschema stays at the base commit: that is
# what the agent's patch is applied on top of at grade time.
RUN git clone https://github.com/sourcemeta/jsonschema.git /solution/full \
    && cd /solution/full \
    && git checkout "$REFERENCE_COMMIT" \
    && git config --global --add safe.directory /solution/full

# The reference patch, verbatim, and the test-side overlay (the reference
# commit's test/ diff alone), which the classical criterion applies over the
# agent's tree.
RUN cd /solution/full \
    && git format-patch -1 "$REFERENCE_COMMIT" --stdout > /solution/upstream-reference.patch \
    && git diff "$BASE_COMMIT" "$REFERENCE_COMMIT" -- test/ > /solution/test-overlay.patch \
    && test -s /solution/upstream-reference.patch \
    && test -s /solution/test-overlay.patch

# The first-line-only negative control: the labelled example's shape — every
# non-blocker conversion done, the two multi-line warnings converted only down
# to their first line. Generated from the reference patch by our own script so
# the rig commits no upstream bytes and the control is reproducible.
COPY make-negative.py /solution/make-negative.py
RUN cd /solution/full \
    && python3 /solution/make-negative.py \
        --base "$BASE_COMMIT" \
        --out /solution/first-line-only.patch \
    && test -s /solution/first-line-only.patch

# The graded checks run with no network, so the cmake configure inside the
# grader must not need one. The clang-format wheel download the project's
# module attempts is a warning with a PATH fallback, and 20.1.6 is already on
# PATH from the base image — pinning the same version the project pins.
RUN rm -rf /solution/full/.git