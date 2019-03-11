FROM golang:1.12-alpine3.9

COPY . /go/src/mvdan.cc/sh
RUN CGO_ENABLED=0 go install -ldflags '-w -s -extldflags "-static"' mvdan.cc/sh/cmd/shfmt

FROM busybox:1.30.1-musl
COPY --from=0 /go/bin/shfmt /bin/shfmt
ENTRYPOINT ["/bin/shfmt"]
