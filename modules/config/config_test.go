package config

import (
	"strings"
	"testing"
	"time"
)

// Пример из examples/main.yml: старые конфиги должны читаться как раньше.
const example = `
app:
  port: 9400
  bind: 0.0.0.0
  timeout: 60
  kibana: http://kibana.host
snapshot:
  host: https://localhost:9200/
  name: recoverer
  username: admin
  password: admin
  ssl: false
  insecure: true
  is_s3: true
  include_system: false
search:
  host: https://localhost:9200
  name: opensearch
  username: admin
  password: admin
  insecure: true
  file_limit:
    rows: 1000000
    size: 5
`

func TestParseExample(t *testing.T) {
	c, err := Parse([]byte(example))
	if err != nil {
		t.Fatal(err)
	}
	if c.App.Port != "9400" || c.App.TimeOut != 60 || c.App.Kibana != "http://kibana.host" {
		t.Errorf("app: %+v", c.App)
	}
	if c.Snapshot.Name != "recoverer" || !c.Snapshot.IsS3 || !c.Snapshot.InsecureSkipVerify || c.Snapshot.Username != "admin" {
		t.Errorf("snapshot: %+v", c.Snapshot)
	}
	if c.Search.Host != "https://localhost:9200/" {
		t.Errorf("search host must end with /: %q", c.Search.Host)
	}
	if c.Search.Name != "opensearch" || c.Search.RequestBatch != DefaultBatch {
		t.Errorf("search: %+v", c.Search)
	}
	if c.Search.FileLimit.Rows != 1000000 || c.Search.FileLimit.Size != 5<<30 {
		t.Errorf("file_limit: %+v", c.Search.FileLimit)
	}
	if c.App.DataDir != DefaultDataDir || c.App.ExportTTL != time.Hour {
		t.Errorf("new keys must default: %q %v", c.App.DataDir, c.App.ExportTTL)
	}
}

func TestParseDefaults(t *testing.T) {
	c, err := Parse([]byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	if c.App.Port != DefaultPort || c.App.Bind != DefaultBind || c.App.TimeOut != DefaultTimeout {
		t.Errorf("app defaults: %+v", c.App)
	}
	if c.Snapshot.Host != DefaultHost || c.Search.Host != DefaultHost {
		t.Errorf("hosts: %q %q", c.Snapshot.Host, c.Search.Host)
	}
	if c.Snapshot.Name != "127.0.0.1" {
		t.Errorf("name from host: %q", c.Snapshot.Name)
	}
}

func TestParseNewKeys(t *testing.T) {
	c, err := Parse([]byte("app:\n  data_dir: /var/lib/x\n  export_ttl: 30m\n  log_format: json\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.App.DataDir != "/var/lib/x" || c.App.ExportTTL != 30*time.Minute || c.App.LogFormat != "json" {
		t.Errorf("%+v", c.App)
	}
}

func TestParseErrors(t *testing.T) {
	for name, raw := range map[string]string{
		"bad yaml":      "app: [",
		"host scheme":   "snapshot:\n  host: localhost:9200\n",
		"cert alone":    "search:\n  client_cert: /c.pem\n",
		"batch":         "search:\n  request_batch: 20000\n",
		"ttl":           "app:\n  export_ttl: soon\n",
		"log format":    "app:\n  log_format: xml\n",
		"zero timeout":  "app:\n  timeout: 0\n",
		"negative rows": "search:\n  file_limit:\n    rows: -1\n",
	} {
		if _, err := Parse([]byte(raw)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	_, err := Parse([]byte("snapshot:\n  host: a\nsearch:\n  host: b\n"))
	if err == nil || !strings.Contains(err.Error(), "snapshot.host") || !strings.Contains(err.Error(), "search.host") {
		t.Errorf("all problems must be reported: %v", err)
	}
}
