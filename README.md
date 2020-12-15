**elasticsearch-extractor** is a simple web UI for end users to extract any index from the desired Elasticsearch snapshot within repository (S3-compatible or any other registered in your cluster).

It requires Elasticsearch v7.0 or greater.

# Installing

To build & install elasticsearch-extractor on Linux (with systemd), please use the following commands:

```
$ go get -u github.com/jteeuwen/go-bindata/...
$ git clone https://github.com/flant/elasticsearch-extractor.git
$ cd elasticsearch-extractor
$ make
$ sudo cp ./build/elasticsearch-extractor /usr/local/sbin/extractor
$ sudo cp ./examples/main.yml /usr/local/etc/extractor.yml
$ sudo cp ./examples/extractor.service /etc/systemd/system/
$ vim /usr/local/etc/extractor.yml # config is small and self-descriptive
$ sudo systemctl daemon-reload && systemctl start extractor
$ sudo systemctl enable extractor
```

# Using
    docker-compose up --build --force-recreate -d