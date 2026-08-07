# Image for the GitHub Action.
#
# The runtime stage deliberately installs bash and dash alongside Alpine's own
# busybox: egglint syntax-checks each install script with the interpreter the egg
# declares, so all three need to be present for the check to be faithful rather
# than approximated.

FROM golang:1.22-alpine AS build

WORKDIR /src

# Copy manifests first so dependency download is cached independently of source.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=docker
RUN CGO_ENABLED=0 go build \
        -trimpath \
        -ldflags "-s -w -X main.version=${VERSION}" \
        -o /out/egglint ./cmd/egglint

FROM alpine:3.20

RUN apk add --no-cache bash dash

COPY --from=build /out/egglint /usr/local/bin/egglint
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]
