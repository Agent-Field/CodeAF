# syntax=docker/dockerfile:1.10@sha256:865e5dd094beca432e8c0a1d5e1c465db5f998dca4e439981029b3b81fb39ed5
#
# A keyless stand-in for an OpenRouter-compatible model endpoint, for smoke tests
# only (the `stub` profile in compose.yaml). Every answer is free and scripted:
# a step that must write a JSON file gets the smallest document its schema
# allows, and anything else gets a short reply quoting the last words it was sent.
# It is NOT a model and produces no useful work.
#
# Build context is this folder:   docker build -f modelstub.Dockerfile -t modelstub-org .

FROM golang:1.23-bookworm@sha256:167053a2bb901972bf2c1611f8f52c44d5fe7e762e5cab213708d82c421614db AS build
ENV CGO_ENABLED=0 GOFLAGS=-buildvcs=false
WORKDIR /src
COPY modelstub/ ./
RUN go build -trimpath -ldflags="-s -w" -o /out/modelstub .

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /out/modelstub /usr/local/bin/modelstub
EXPOSE 8890
ENTRYPOINT ["/usr/local/bin/modelstub"]
CMD ["-addr", ":8890"]
