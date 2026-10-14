package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/flant/elasticsearch-extractor/modules/export"
	"github.com/flant/elasticsearch-extractor/modules/opensearch"
	"github.com/flant/elasticsearch-extractor/modules/snapshots"
)

// es - поддельный кластер: отвечает на вызовы, которые делают обработчики, и
// запоминает запросы.
type es struct {
	mu    sync.Mutex
	calls []string
}

func (e *es) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	e.mu.Lock()
	e.calls = append(e.calls, r.Method+" "+r.URL.Path+" "+string(b))
	e.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	p := r.URL.Path
	switch {
	case strings.HasSuffix(p, "/_status"):
		_, _ = io.WriteString(w, `{"snapshots":[{"snapshot":"s","indices":{"huge":{"shards_stats":{"total":1},"stats":{"total":{"size_in_bytes":5000000000000}},"shards":{"0":{"stats":{"total":{"size_in_bytes":5000000000000}}}}}}}]}`)
	case strings.HasPrefix(p, "/_cluster/health"):
		_, _ = io.WriteString(w, `{"initializing_shards":0,"unassigned_shards":0}`)
	case strings.HasPrefix(p, "/_cat/nodes"):
		_, _ = io.WriteString(w, `[{"ip":"1","name":"n","dt":"100","du":"50","dup":"50","d":"50"}]`)
	case p == "/_cat/repositories":
		_, _ = io.WriteString(w, `[{"id":"r","type":"s3"}]`)
	case p == "/missing-repo/_recovery" || strings.HasPrefix(p, "/_snapshot/missing"):
		w.WriteHeader(404)
		_, _ = io.WriteString(w, `{"error":{"type":"repository_missing_exception","reason":"[missing] missing"},"status":404}`)
	case strings.HasSuffix(p, "/_search") && strings.Contains(string(b), `"bad query`):
		w.WriteHeader(400)
		_, _ = io.WriteString(w, `{"error":{"type":"query_shard_exception","reason":"failed to create query"},"status":400}`)
	case strings.HasSuffix(p, "/_search") || p == "/_search/scroll":
		if r.Method == http.MethodDelete {
			_, _ = io.WriteString(w, `{}`)
			return
		}
		hits := `[{"_source":{"@timestamp":"2026-10-13T10:00:00Z","msg":"x"},"sort":[1]}]`
		if p == "/_search/scroll" {
			hits = `[]`
		}
		_, _ = io.WriteString(w, `{"took":1,"_scroll_id":"SID","hits":{"hits":`+hits+`}}`)
	default:
		w.WriteHeader(404)
		_, _ = io.WriteString(w, `{"error":"unexpected `+p+`"}`)
	}
}

func (e *es) seen(substr string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, c := range e.calls {
		if strings.Contains(c, substr) {
			return true
		}
	}
	return false
}

func newServer(t *testing.T) (http.Handler, *es) {
	t.Helper()
	fake := &es{}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	c, _ := opensearch.New(opensearch.Options{Name: "recoverer", Host: srv.URL + "/"})
	ex, err := export.New(export.Options{Dir: t.TempDir(), Batch: 10, MaxRows: 100, MaxSize: 1 << 20, TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	s := New(Options{
		Snapshot: c, Search: c, Exporter: ex, Kibana: "kibana.example.com", Version: "test",
		Snapshots: snapshots.New(snapshots.Options{Client: c}),
		UI:        uiFS(),
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	return s.Handler(), fake
}

func call(t *testing.T, h http.Handler, body string) (int, map[string]any, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/", strings.NewReader(body)))
	raw := rec.Body.String()
	var m map[string]any
	_ = json.Unmarshal([]byte(raw), &m)
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("%s: content type %q", body, ct)
	}
	return rec.Code, m, raw
}

func TestErrorsAreJSON(t *testing.T) {
	h, _ := newServer(t)
	for body, want := range map[string]int{
		`{"action":"nope"}`: 400,
		`{bad json`:         400,
		`{"action":"get_snapshots","values":{"repo":"missing"}}`:                          404,
		`{"action":"search","search":{"cluster":"Other","index":"a"}}`:                    400,
		`{"action":"search","search":{"cluster":"Search","index":"a","xql":"bad query"}}`: 400,
	} {
		code, m, raw := call(t, h, body)
		if code != want || m["error"] == nil {
			t.Errorf("%s: %d %s", body, code, raw)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/", nil))
	if rec.Code != http.StatusMethodNotAllowed || !strings.Contains(rec.Body.String(), `"error"`) {
		t.Errorf("GET /api/: %d %s", rec.Code, rec.Body.String())
	}
}

func TestConfigAndClusters(t *testing.T) {
	h, _ := newServer(t)
	_, m, raw := call(t, h, `{"action":"get_config"}`)
	if m["kibana"] != "kibana.example.com" || m["version"] != "test" || m["limits"].(map[string]any)["rows"] != float64(100) {
		t.Errorf("%s", raw)
	}
	_, _, raw = call(t, h, `{"action":"get_clusters"}`)
	if !strings.Contains(raw, `"Name":"recoverer"`) || !strings.Contains(raw, `"Type":"Search"`) {
		t.Errorf("get_clusters keeps its keys: %s", raw)
	}
}

func TestRestoreNothingFits(t *testing.T) {
	h, fake := newServer(t)
	code, _, raw := call(t, h, `{"action":"restore","values":{"repo":"r","snapshot":"s","indices":["huge"]}}`)
	if code != 200 || strings.TrimSpace(raw) != `{"restoring":[],"skipped":["huge"]}` {
		t.Errorf("%d %s", code, raw)
	}
	if fake.seen("_restore") {
		t.Error("the cluster must not be asked to restore nothing")
	}
}

func TestDeleteOnlyRestored(t *testing.T) {
	h, fake := newServer(t)
	for _, idx := range []string{"*", "_all", "logs-2026.10.12"} {
		code, _, raw := call(t, h, `{"action":"del_index","values":{"index":"`+idx+`"}}`)
		if code != 400 {
			t.Errorf("%s: %d %s", idx, code, raw)
		}
	}
	if fake.seen("DELETE") {
		t.Error("refused deletes reached the cluster")
	}
}

func TestSearchPage(t *testing.T) {
	h, _ := newServer(t)
	code, m, raw := call(t, h, `{"action":"search","search":{"cluster":"Search","index":"app-*","timefields":["@timestamp"],"date_start":"2026-10-13 00:00:00","date_end":"2026-10-14 00:00:00"}}`)
	if code != 200 || len(m["hits"].(map[string]any)["hits"].([]any)) != 1 {
		t.Errorf("%d %s", code, raw)
	}
}

func TestExportAndDownload(t *testing.T) {
	h, fake := newServer(t)
	code, m, raw := call(t, h, `{"action":"prepare_csv","search":{"cluster":"Search","index":"app-*","fname":"abc","mapping":["msg"],"timefields":["@timestamp"],"date_start":"2026-10-13 00:00:00","date_end":"2026-10-14 00:00:00"}}`)
	if code != 200 || m["file"] != "abc.csv" || m["rows"] != float64(1) || m["truncated"] != false {
		t.Fatalf("%d %s", code, raw)
	}
	if !fake.seen("DELETE /_search/scroll") {
		t.Error("scroll must be cleared")
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data/abc.csv", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"x";`) ||
		rec.Header().Get("Content-Disposition") != `attachment; filename="abc.csv"` {
		t.Errorf("%d %q %v", rec.Code, rec.Body.String(), rec.Header())
	}
	for _, p := range []string{"/data/../etc/passwd", "/data/abc.txt", "/data/nope.csv", "/x/data/abc.csv"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		// ".." ServeMux перенаправляет на очищенный путь, остальное - 404; файл не отдаётся
		if rec.Code != 404 && rec.Code != 301 {
			t.Errorf("%s: %d", p, rec.Code)
		}
	}

	code, _, _ = call(t, h, `{"action":"prepare_json","search":{"cluster":"Search","index":"app-*","fname":"../x"}}`)
	if code != 400 {
		t.Errorf("bad file name: %d", code)
	}
}
