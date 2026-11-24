# syntax=docker/dockerfile:1.10@sha256:865e5dd094beca432e8c0a1d5e1c465db5f998dca4e439981029b3b81fb39ed5
FROM golang:1.23-alpine@sha256:383395b794dffa5b53012a212365d40c8e37109a626ca30d6151c8348d380b5f AS build
ENV CGO_ENABLED=0 GOFLAGS=-buildvcs=false
WORKDIR /src
COPY gitstub/ ./
RUN go build -trimpath -ldflags="-s -w" -o /out/gitstub .

FROM alpine:3.21@sha256:bc21487101ae47172509130ee1092cc273a831968d64b617776d1f69c81e42ab
# Alpine ships git-http-backend, the smart-HTTP server the stub runs, in its own package.
RUN apk add --no-cache git git-daemon
COPY --from=build /out/gitstub /usr/local/bin/gitstub
EXPOSE 8891
ENTRYPOINT ["/usr/local/bin/gitstub"]
