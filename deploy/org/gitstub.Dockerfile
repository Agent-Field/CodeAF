# syntax=docker/dockerfile:1.10
FROM golang:1.23-alpine AS build
ENV CGO_ENABLED=0 GOFLAGS=-buildvcs=false
WORKDIR /src
COPY gitstub/ ./
RUN go build -trimpath -ldflags="-s -w" -o /out/gitstub .

FROM alpine:3.21
RUN apk add --no-cache git
COPY --from=build /out/gitstub /usr/local/bin/gitstub
EXPOSE 8891
ENTRYPOINT ["/usr/local/bin/gitstub"]
