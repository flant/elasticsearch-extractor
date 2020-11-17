GOPATH=$(shell pwd)/vendor:$(shell pwd)
GOBIN=$(shell pwd)/build/
GOFILES=./cmd/$(wildcard *.go)
GONAME=$(shell basename "$(PWD)")
PID=/tmp/go-$(GONAME).pid
GOOS=linux
#GOARCH=386
BUILD=`date +%FT%T%z`
MKDIR_P = mkdir -p

all :: build

get:
	@GOPATH=$(GOPATH) GOBIN=$(GOBIN) go get -d $(GOFILES)

build:
	@echo "Building $(GOFILES) to ./build"
	cd front && go-bindata -pkg front -o ../modules/front/front.go ./...
	cd ../
	go mod vendor
	@GOPATH=$(GOPATH) GOBIN=$(GOBIN) GOOS=$(GOOS) GOARCH=$(GOARCH) go build -ldflags "-s -w -X main.vBuild=${BUILD}" -o build/$(GONAME) $(GOFILES)
	strip ./build/$(GONAME)

run:
	@GOPATH=$(GOPATH) GOBIN=$(GOBIN) go run $(GOFILES)

clear:
	@clear

clean:
	@echo "Cleaning"
	@GOPATH=$(GOPATH) GOBIN=$(GOBIN) go clean

.PHONY:	build get install run clean dirs

