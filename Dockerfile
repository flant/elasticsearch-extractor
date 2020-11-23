# stage 1: build
FROM golang:latest AS builder
LABEL maintainer="Uzhinskiy Boris <boris.uzhinsky@flant.com>"

# Add source code
RUN mkdir -p /go/src/extractor
ADD . /go/src/extractor

# Build binary
RUN go get -u github.com/jteeuwen/go-bindata/...
RUN cd /go/src/extractor/ && make

# stage 2: lightweight "release"
FROM debian:latest as extractor
LABEL maintainer="Uzhinskiy Boris <boris.uzhinsky@flant.com>"

EXPOSE 9400/tcp

COPY --from=builder /go/src/extractor/build/ /app
COPY --from=builder /go/src/extractor/main.yml /app/main.yml

ENTRYPOINT [ "/app/extractor" ]
CMD [ "-config", "/app/main.yml" ]