FROM golang:1.11.2-alpine3.8

COPY . /go/src/mvdan.cc/sh
RUN CGO_ENABLED=0 go install -ldflags '-w -s -extldflags "-static"' mvdan.cc/sh/cmd/shfmt

FROM busybox:1.29.3-musl
COPY --from=0 /go/bin/shfmt /bin/shfmt
ENTRYPOINT ["/bin/shfmt"]
