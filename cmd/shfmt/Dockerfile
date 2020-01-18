FROM golang:1.13.5-alpine3.11 AS build

WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -ldflags '-w -s -extldflags "-static"' ./cmd/shfmt

FROM alpine:3.11 AS alpine
COPY --from=build /src/shfmt /bin/shfmt
ENTRYPOINT ["/bin/shfmt"]

FROM scratch
COPY --from=build /src/shfmt /bin/shfmt
ENTRYPOINT ["/bin/shfmt"]
CMD ["-h"]
