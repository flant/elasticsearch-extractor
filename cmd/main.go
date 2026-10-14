// Copyright © 2020 Uzhinskiy Boris
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// extractor - веб-интерфейс для извлечения индексов из снапшотов Elasticsearch/OpenSearch
// и выгрузки найденного в CSV/JSON.
//
//	extractor [-config FILE] [-listen ADDR]   UI, API
//	extractor -V                              версия
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/flant/elasticsearch-extractor/modules/config"
	"github.com/flant/elasticsearch-extractor/modules/export"
	"github.com/flant/elasticsearch-extractor/modules/opensearch"
	"github.com/flant/elasticsearch-extractor/modules/server"
	"github.com/flant/elasticsearch-extractor/modules/snapshots"
	"github.com/flant/elasticsearch-extractor/web"
)

// Версию и метку сборки подставляет сборка: -ldflags "-X main.version=... -X main.vBuild=...".
// Версия - git describe --tags (Makefile, аргумент VERSION в Dockerfile); без них - "dev".
var (
	version = "dev"
	vBuild  = "dev"
)

func main() {
	os.Exit(run())
}

func run() int {
	var cfgPath, listen string
	flag.StringVar(&cfgPath, "config", "main.yml", "read configuration from this file")
	flag.StringVar(&cfgPath, "f", "main.yml", "read configuration from this file")
	flag.StringVar(&listen, "listen", "", "listen address host:port, overrides app.bind and app.port")
	showVersion := flag.Bool("V", false, "show version")
	flag.Parse()
	if *showVersion {
		fmt.Printf("version: %s (%s)\n", version, vBuild)
		return 0
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("bad configuration", "file", cfgPath, "error", err)
		return 2
	}
	log := newLogger(cfg.App.LogFormat)
	slog.SetDefault(log)

	timeout := time.Duration(cfg.App.TimeOut) * time.Second
	snapshotClient, err := opensearch.New(clusterOptions(cfg.Snapshot, timeout))
	if err != nil {
		log.Error("bad configuration", "error", err)
		return 2
	}
	searchClient, err := opensearch.New(clusterOptions(cfg.Search.Cluster, timeout))
	if err != nil {
		log.Error("bad configuration", "error", err)
		return 2
	}
	exporter, err := export.New(export.Options{
		Dir:     cfg.App.DataDir,
		Batch:   cfg.Search.RequestBatch,
		MaxRows: cfg.Search.FileLimit.Rows,
		MaxSize: cfg.Search.FileLimit.Size,
		TTL:     cfg.App.ExportTTL,
		Log:     log,
	})
	if err != nil {
		log.Error("cannot start", "error", err)
		return 1
	}

	srv := server.New(server.Options{
		Snapshot: snapshotClient,
		Search:   searchClient,
		Snapshots: snapshots.New(snapshots.Options{
			Client:        snapshotClient,
			IncludeSystem: cfg.Snapshot.Include,
			S3:            cfg.Snapshot.IsS3,
			Log:           log,
		}),
		Exporter: exporter,
		UI:       web.FS(),
		Kibana:   cfg.App.Kibana,
		Version:  version,
		Log:      log,
	})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); exporter.Cleanup(ctx) }()

	addr := listen
	if addr == "" {
		addr = cfg.App.Bind + ":" + cfg.App.Port
	}
	hs := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		// WriteTimeout не задан: выгрузка большого индекса отвечает через минуты
	}
	go func() {
		<-ctx.Done()
		// текущим запросам, в том числе выгрузкам, даётся время закончиться
		shutdown, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		_ = hs.Shutdown(shutdown)
	}()

	log.Info("listening", "address", addr, "version", version, "build", vBuild,
		"snapshot", cfg.Snapshot.Host, "search", cfg.Search.Host, "ui", web.FS() != nil)
	if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("server failed", "error", err)
		return 1
	}
	wg.Wait()
	log.Info("stopped")
	return 0
}

func clusterOptions(c config.Cluster, timeout time.Duration) opensearch.Options {
	return opensearch.Options{
		Name:       c.Name,
		Host:       c.Host,
		Username:   c.Username,
		Password:   c.Password,
		CACert:     c.CAcert,
		ClientCert: c.ClientCert,
		ClientKey:  c.ClientKey,
		Insecure:   c.InsecureSkipVerify,
		Timeout:    timeout,
	}
}

func newLogger(format string) *slog.Logger {
	if format == "json" {
		return slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, nil))
}
