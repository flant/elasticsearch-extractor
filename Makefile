GOPATH=$(shell pwd)/vendor:$(shell pwd)
GOBIN=$(shell pwd)/build/
GOFILES=./cmd/$(wildcard *.go)
GONAME=$(shell basename "$(PWD)")
PID=/tmp/go-$(GONAME).pid
GOOS=linux
#GOARCH=386
BUILD=`date +%FT%T%z`
# Версия из git: тег, а после него - число коммитов и хеш (v0.2.28-9-g72b3614), -dirty при правках
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS = -s -w -X main.version=$(VERSION) -X main.vBuild=${BUILD}
MKDIR_P = mkdir -p
NPM := $(shell command -v npm 2>/dev/null)
NODE_IMAGE ?= mirror.gcr.io/library/node:24-alpine

all :: build

get:
	@GOPATH=$(GOPATH) GOBIN=$(GOBIN) go get -d $(GOFILES)

# UI на Vue собирается в web/dist/ui и встраивается в бинарник через go:embed.
# Без локального Node.js сборка идёт в контейнере $(NODE_IMAGE).
ui:
ifdef NPM
	cd web && npm ci --no-audit --no-fund && npm run build
else
	docker run --rm --user $$(id -u):$$(id -g) -e HOME=/tmp -v $(CURDIR)/web:/web -w /web $(NODE_IMAGE) \
		sh -c 'npm ci --no-audit --no-fund && npm run build'
endif

build: ui
	@echo "Building $(GOFILES) to ./build"
	@GOPATH=$(GOPATH) GOBIN=$(GOBIN) GOOS=$(GOOS) GOARCH=$(GOARCH) go build -mod=mod -ldflags "$(LDFLAGS)" -o build/$(GONAME) $(GOFILES)
	strip ./build/$(GONAME)

run:
	@GOPATH=$(GOPATH) GOBIN=$(GOBIN) go run $(GOFILES)

clear:
	@clear

clean:
	@echo "Cleaning"
	@GOPATH=$(GOPATH) GOBIN=$(GOBIN) go clean

# Docker-образ: .git в образ не попадает, версию передаёт аргумент VERSION
docker:
	docker build --build-arg VERSION=$(VERSION) --build-arg BUILD=${BUILD} -t elasticsearch-extractor:$(VERSION) .

# Тестовый стенд: заглушка Elasticsearch, extractor из текущего кода и UI (Vite) на порту STAND_PORT
stand:
	@./stand/stand.sh start

stand-stop:
	@./stand/stand.sh stop

stand-status:
	@./stand/stand.sh status

.PHONY:	build ui docker get install run clean dirs stand stand-stop stand-status

