**elasticsearch-extractor** is a simple web UI for end users to extract any index from the desired Elasticsearch snapshot within repository (S3-compatible or any other registered in your cluster).

It requires Elasticsearch v7.0 or greater.

# Motivation / idea

*[This announcement](https://blog.flant.com/announcing-elasticsearch-extractor-open-source-tool/) of the project (Jan'21) sheds some light on why elasticsearch-extractor has emerged.*

We deal with lots of logs stored in Elasticsearch clusters. They are regularly archived into snapshots and stored in S3. Being available in snapshots for a long period, these logs are quite often needed by engineers (to be examined for different reasons). That's why we want to have a simple interface for getting these logs from snapshots.

While we really like [Cerebro](https://github.com/lmenezes/cerebro/), it gives you *too much* power over Elasticsearch clusters and doesn't allow you to control permissions for its users. Here's how we've ended up with creating a much simpler tool that implements the only function: to extract an index from Elasticsearch snapshot.

elasticsearch-extractor operates as:
* a simple web UI for end users;
* a server proxying requests to Elasticsearch.

Since there's no authentication layer implemented in elasticsearch-extractor, you have to use your relevant infrastructure components for that, e.g. Ingress controller (if Elasticsearch is in Kubernetes) or nginx/Apache built-in capabilities.

# Using

## Building

The UI is written in Vue 3 + TypeScript (`web/`) and is embedded into the Go binary with `go:embed`.
`make` builds the UI with Node.js when `npm` is available, otherwise in a `node:24-alpine` container, and then the binary:

```
$ git clone https://github.com/flant/elasticsearch-extractor.git
$ cd elasticsearch-extractor
$ make
```

The binary is `./build/elasticsearch-extractor`. Without a UI build the server answers that the UI is not built.

The version comes from git: `git describe --tags --always --dirty`, e.g. `v0.2.28` on a tag or `v0.2.28-9-g72b3614` after it. To release a version, tag the commit (`git tag v0.3.0`) and build. `extractor -V` prints it.

## Installing & running in Linux with systemd

```
$ sudo cp ./build/elasticsearch-extractor /usr/local/sbin/extractor
$ sudo cp ./examples/main.yml /usr/local/etc/extractor.yml
$ sudo cp ./examples/extractor.service /etc/systemd/system/
$ vim /usr/local/etc/extractor.yml # config is small and self-descriptive
$ sudo systemctl daemon-reload && systemctl start extractor
$ sudo systemctl enable extractor
```

## Running in Docker

The Dockerfile builds the UI and the binary itself. `.git` is not sent to the build, so the version is passed as an argument; `make docker` does it:

```
make docker
# or
VERSION=$(git describe --tags --always --dirty) docker-compose up --build --force-recreate -d
```

## Development

### Test stand

`make stand` starts an Elasticsearch mock with demo data (`stand/mock_es.py`), the extractor built from the current code and the UI on the Vite dev server, available on the network at port 5173 (`STAND_PORT`). Changes in `web/src` are applied without a restart. Node.js is not required, Vite runs in a container.

```
make stand          # (re)start
make stand-status   # listening ports
make stand-stop     # stop
```

### Hot reload of the backend

Use `air`:

```bash
$ go install github.com/air-verse/air@latest
$ air
```

# Further information

Please feel free to use [issues](https://github.com/flant/elasticsearch-extractor/issues) and [discussions](https://github.com/flant/elasticsearch-extractor/discussions) to get help from maintainers & community.
