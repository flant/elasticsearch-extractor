# extractor: UI на Vue собирает Node.js, Go встраивает его в бинарник через go:embed.
FROM mirror.gcr.io/library/node:24-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM mirror.gcr.io/library/golang:1.23 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY modules/ modules/
COPY web/embed.go web/
COPY --from=web /web/dist/ web/dist/
# .git в контекст сборки не попадает: версию передают снаружи,
# docker build --build-arg VERSION=$(git describe --tags --always --dirty) (так делает make docker)
ARG VERSION=dev
ARG BUILD=docker
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION} -X main.vBuild=${BUILD}" -o /extractor ./cmd/

FROM mirror.gcr.io/library/debian:12-slim AS extractor
LABEL maintainer="Uzhinskiy Boris <boris.uzhinsky@flant.com>"
# сертификаты нужны для TLS к кластерам с публичными сертификатами
RUN apt-get update \
 && apt-get install -qy --no-install-recommends ca-certificates \
 && rm -rf /var/lib/apt/lists/*
COPY --from=build /extractor /app/extractor
COPY examples/main.yml /app/main.yml
# выгрузки пишутся в /tmp/data, root не нужен
USER 65532:65532
EXPOSE 9400/tcp
ENTRYPOINT [ "/app/extractor" ]
CMD [ "-config", "/app/main.yml" ]
