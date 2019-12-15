FROM golang:1.13.5-alpine3.10

WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -ldflags '-w -s -extldflags "-static"' ./cmd/shfmt

FROM busybox:1.31.1-musl
COPY --from=0 /src/shfmt /bin/shfmt
ENTRYPOINT ["/bin/shfmt"]
