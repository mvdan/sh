FROM golang:1.11.2-alpine3.8

ENV VERSION 2.6.1

RUN apk add --no-cache git \
      && go get -d mvdan.cc/sh/cmd/shfmt \
      && git -C $GOPATH/src/mvdan.cc/sh checkout -q v$VERSION \
      && CGO_ENABLED=0 go install -ldflags '-w -s -extldflags "-static"' mvdan.cc/sh/cmd/shfmt

FROM busybox:1.29.3-musl
COPY --from=0 /go/bin/shfmt /bin/shfmt
