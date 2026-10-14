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

// Package config читает YAML-конфиг extractor. Ключи остаются прежними, чтобы старые
// конфиги работали без правок; новые ключи только необязательные.
package config

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v2"
)

const (
	DefaultPort      = "9400"
	DefaultBind      = "0.0.0.0"
	DefaultTimeout   = 30
	DefaultHost      = "http://127.0.0.1:9200/"
	DefaultBatch     = 10000
	DefaultRows      = 1000000
	DefaultSizeGB    = 5
	DefaultDataDir   = "/tmp/data"
	DefaultExportTTL = time.Hour
)

type Config struct {
	App struct {
		Port       string `yaml:"port"`
		Bind       string `yaml:"bind"`
		Kibana     string `yaml:"kibana"`
		TimeOut    int    `yaml:"-"`
		TimeOutRaw *int   `yaml:"timeout"`
		// Каталог файлов выгрузок и срок их хранения ("1h", "30m"). Необязательные.
		DataDir      string        `yaml:"data_dir"`
		ExportTTL    time.Duration `yaml:"-"`
		ExportTTLRaw string        `yaml:"export_ttl"`
		// Формат журнала: text (по умолчанию) или json. Необязательный.
		LogFormat string `yaml:"log_format"`
		DebugMode bool   `yaml:"debug_mode,omitempty"`
	} `yaml:"app"`
	Snapshot Cluster `yaml:"snapshot"`
	Search   struct {
		Cluster      `yaml:",inline"`
		RequestBatch int64 `yaml:"request_batch,omitempty"`
		FileLimit    struct {
			Rows    int    `yaml:"-"`
			RowsRaw *int   `yaml:"rows,omitempty"`
			Size    int64  `yaml:"-"`
			SizeRaw *int64 `yaml:"size,omitempty"`
		} `yaml:"file_limit,omitempty"`
	} `yaml:"search,omitempty"`
}

// Cluster - подключение к кластеру Elasticsearch/OpenSearch.
type Cluster struct {
	Host               string `yaml:"host"`
	Name               string `yaml:"name,omitempty"`
	SSL                bool   `yaml:"ssl"`
	Username           string `yaml:"username"`
	Password           string `yaml:"password"`
	CAcert             string `yaml:"ca_cert"`
	ClientCert         string `yaml:"client_cert"`
	ClientKey          string `yaml:"client_key"`
	InsecureSkipVerify bool   `yaml:"insecure"`
	// Только для snapshot: показывать системные снапшоты (.kibana*, .opendistro*)
	Include bool `yaml:"include_system"`
	// Только для snapshot: репозиторий в S3, место на узлах не проверяется
	IsS3 bool `yaml:"is_s3"`
}

var hostName = regexp.MustCompile(`(?m)^https*://(?P<host>[\w\d-\._]+)*:*[\d]*/*$`)

// Load читает конфиг из файла.
func Load(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	return Parse(raw)
}

// Parse разбирает конфиг, подставляет значения по умолчанию и проверяет его.
func Parse(raw []byte) (Config, error) {
	var c Config
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return Config{}, err
	}

	if c.App.Port == "" {
		c.App.Port = DefaultPort
	}
	if c.App.Bind == "" {
		c.App.Bind = DefaultBind
	}
	c.App.TimeOut = DefaultTimeout
	if c.App.TimeOutRaw != nil {
		c.App.TimeOut = *c.App.TimeOutRaw
	}
	if c.App.DataDir == "" {
		c.App.DataDir = DefaultDataDir
	}
	c.App.ExportTTL = DefaultExportTTL
	if c.App.ExportTTLRaw != "" {
		d, err := time.ParseDuration(c.App.ExportTTLRaw)
		if err != nil {
			return Config{}, fmt.Errorf("app.export_ttl: %w", err)
		}
		c.App.ExportTTL = d
	}

	c.Snapshot.fill()
	c.Search.fill()

	if c.Search.RequestBatch == 0 {
		c.Search.RequestBatch = DefaultBatch
	}
	c.Search.FileLimit.Rows = DefaultRows
	if c.Search.FileLimit.RowsRaw != nil {
		c.Search.FileLimit.Rows = *c.Search.FileLimit.RowsRaw
	}
	c.Search.FileLimit.Size = DefaultSizeGB * 1024 * 1024 * 1024
	if c.Search.FileLimit.SizeRaw != nil {
		c.Search.FileLimit.Size = *c.Search.FileLimit.SizeRaw * 1024 * 1024 * 1024
	}

	return c, c.validate()
}

// fill подставляет адрес по умолчанию и имя кластера из адреса; адрес всегда
// заканчивается на "/", к нему дописываются пути API.
func (cl *Cluster) fill() {
	if cl.Host == "" {
		cl.Host = DefaultHost
	}
	if !strings.HasSuffix(cl.Host, "/") {
		cl.Host += "/"
	}
	if cl.Name == "" {
		if m := hostName.FindStringSubmatchIndex(cl.Host); m != nil {
			cl.Name = string(hostName.ExpandString(nil, "$host", cl.Host, m))
		}
	}
}

func (c Config) validate() error {
	var errs []error
	for name, cl := range map[string]Cluster{"snapshot": c.Snapshot, "search": c.Search.Cluster} {
		if !strings.HasPrefix(cl.Host, "http://") && !strings.HasPrefix(cl.Host, "https://") {
			errs = append(errs, fmt.Errorf("%s.host must start with http:// or https://, got %q", name, cl.Host))
		}
		if (cl.ClientCert == "") != (cl.ClientKey == "") {
			errs = append(errs, fmt.Errorf("%s: client_cert and client_key go together", name))
		}
	}
	if c.App.TimeOut <= 0 {
		errs = append(errs, errors.New("app.timeout must be positive"))
	}
	if c.Search.RequestBatch <= 0 || c.Search.RequestBatch > 10000 {
		errs = append(errs, errors.New("search.request_batch must be within 1..10000"))
	}
	if c.Search.FileLimit.Rows <= 0 || c.Search.FileLimit.Size <= 0 {
		errs = append(errs, errors.New("search.file_limit rows and size must be positive"))
	}
	if c.App.ExportTTL <= 0 {
		errs = append(errs, errors.New("app.export_ttl must be positive"))
	}
	if f := c.App.LogFormat; f != "" && f != "text" && f != "json" {
		errs = append(errs, fmt.Errorf("app.log_format must be text or json, got %q", f))
	}
	return errors.Join(errs...)
}
