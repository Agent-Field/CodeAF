# syntax=docker/dockerfile:1.10
#
# A keyless stand-in for an OpenRouter-compatible model endpoint, for smoke tests
# only (the `stub` profile in compose.yaml). Every answer is free and scripted:
# a step that must write a JSON file gets the smallest document its schema
# allows, and anything else gets a short reply quoting the last words it was sent.
# It is NOT a model and produces no useful work.
#
# Build context is this folder:   docker build -f modelstub.Dockerfile -t modelstub-org .

FROM golang:1.23-bookworm AS build
ENV CGO_ENABLED=0 GOFLAGS=-buildvcs=false
WORKDIR /src
COPY modelstub/ ./
RUN go build -trimpath -ldflags="-s -w" -o /out/modelstub .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/modelstub /usr/local/bin/modelstub
EXPOSE 8890
ENTRYPOINT ["/usr/local/bin/modelstub"]
CMD ["-addr", ":8890"]
