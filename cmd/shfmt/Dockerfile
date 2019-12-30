FROM golang:1.13.5-alpine3.10 as build

WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -ldflags '-w -s -extldflags "-static"' ./cmd/shfmt

FROM busybox:1.31.1-musl AS busybox
COPY --from=build /src/shfmt /bin/shfmt
ENTRYPOINT ["/bin/shfmt"]

FROM scratch
COPY --from=build /src/shfmt /bin/shfmt
ENTRYPOINT ["/bin/shfmt"]

# Usage: 
#  docker run --rm -v $PWD:/mnt -w /mnt mvdan/shfmt:<version> <shfmt arguments>
# E.g.:
#  docker run --rm -v $PWD:/mnt -w /mnt mvdan/shfmt:latest -i 2 -l .
