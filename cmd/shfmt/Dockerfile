FROM golang:1.14.1 AS build

WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -ldflags "-w -s -extldflags '-static' -X main.version=$(git describe --always --dirty)" ./cmd/shfmt

FROM alpine:3.11.5 AS alpine
COPY --from=build /src/shfmt /bin/shfmt
COPY "./cmd/shfmt/docker-entrypoint.sh" "/init"
ENTRYPOINT ["/init"]

FROM scratch
COPY --from=build /src/shfmt /bin/shfmt
ENTRYPOINT ["/bin/shfmt"]
CMD ["-h"]
