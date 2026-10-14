package export

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/flant/elasticsearch-extractor/modules/opensearch"
	"github.com/flant/elasticsearch-extractor/modules/search"
)

// scrollES отдаёт pages страниц по perPage документов, затем пустую; запоминает очистку.
type scrollES struct {
	mu      sync.Mutex
	pages   int
	perPage int
	served  int
	cleared []string
	fields  bool
}

func (s *scrollES) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.Method == http.MethodDelete {
		s.cleared = append(s.cleared, string(b))
		_, _ = io.WriteString(w, `{"succeeded":true}`)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/_search") {
		var q map[string]any
		_ = json.Unmarshal(b, &q)
		s.fields = q["_source"] == false
		s.served = 0
	}
	hits := []string{}
	if s.served < s.pages {
		for i := 0; i < s.perPage; i++ {
			n := s.served*s.perPage + i
			if s.fields {
				hits = append(hits, fmt.Sprintf(`{"fields":{"@timestamp":["2026-10-13T10:00:%02dZ"],"msg":["line \"%d\"\nnext"]}}`, n%60, n))
			} else {
				hits = append(hits, fmt.Sprintf(`{"_source":{"@timestamp":"2026-10-13T10:00:%02dZ","msg":"line %d","k8s":{"pod":"p%d"},"tags":["a","b"]}}`, n%60, n, n))
			}
		}
		s.served++
	}
	_, _ = io.WriteString(w, `{"_scroll_id":"SID","hits":{"total":{"value":0},"hits":[`+strings.Join(hits, ",")+`]}}`)
}

func setup(t *testing.T, es *scrollES, maxRows int, maxSize int64) (*Exporter, *opensearch.Client) {
	t.Helper()
	srv := httptest.NewServer(es)
	t.Cleanup(srv.Close)
	c, _ := opensearch.New(opensearch.Options{Name: "t", Host: srv.URL + "/"})
	e, err := New(Options{Dir: t.TempDir(), Batch: 2, MaxRows: maxRows, MaxSize: maxSize, TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	return e, c
}

func params() search.Params {
	return search.Params{
		Index: "app-*", Timefields: []string{"@timestamp"},
		DateStart: "2026-10-13 00:00:00", DateEnd: "2026-10-14 00:00:00",
		Mapping: []string{"msg", "k8s.pod", "tags", "missing"},
	}
}

func read(t *testing.T, e *Exporter, file string) string {
	t.Helper()
	b, err := os.ReadFile(e.Path(file))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestCSV(t *testing.T) {
	es := &scrollES{pages: 2, perPage: 2}
	e, c := setup(t, es, 100, 1<<30)
	res, err := e.Export(context.Background(), c, params(), "csv", "abc")
	if err != nil {
		t.Fatal(err)
	}
	if res.File != "abc.csv" || res.Rows != 4 || res.Truncated || res.Size == 0 {
		t.Errorf("%+v", res)
	}
	got := read(t, e, "abc.csv")
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if lines[0] != "@timestamp;msg;k8s.pod;tags;missing" || len(lines) != 5 {
		t.Errorf("%q", got)
	}
	// вложенные поля _source находятся по пути, массивы через ", ", пустые - "--"
	if lines[1] != `2026-10-13T10:00:00Z;"line 0";"p0";"a, b";--;` {
		t.Errorf("%q", lines[1])
	}
	if len(es.cleared) != 1 || !strings.Contains(es.cleared[0], "SID") {
		t.Errorf("scroll must be cleared: %v", es.cleared)
	}
}

func TestJSONWithFields(t *testing.T) {
	es := &scrollES{pages: 1, perPage: 2}
	e, c := setup(t, es, 100, 1<<30)
	p := params()
	p.Fields = []string{"msg"}
	res, err := e.Export(context.Background(), c, p, "json", "j1")
	if err != nil || res.File != "j1.json" || res.Rows != 2 {
		t.Fatalf("%+v %v", res, err)
	}
	first := strings.Split(read(t, e, "j1.json"), "\n")[0]
	var row map[string]any
	if err := json.Unmarshal([]byte(first), &row); err != nil {
		t.Fatalf("%q: %v", first, err)
	}
	if row["msg"].([]any)[0] != "line \"0\"\nnext" || len(row) != 2 {
		t.Errorf("%v", row)
	}
}

func TestLimits(t *testing.T) {
	es := &scrollES{pages: 10, perPage: 2}
	e, c := setup(t, es, 3, 1<<30)
	res, err := e.Export(context.Background(), c, params(), "json", "lim")
	if err != nil {
		t.Fatal(err)
	}
	// лимит проверяется после страницы: 4 строки при лимите 3
	if !res.Truncated || res.Rows != 4 {
		t.Errorf("rows limit: %+v", res)
	}

	es = &scrollES{pages: 10, perPage: 2}
	e, c = setup(t, es, 1000, 10)
	res, _ = e.Export(context.Background(), c, params(), "csv", "size")
	if !res.Truncated || res.Rows != 2 {
		t.Errorf("size limit: %+v", res)
	}
}

func TestInvalid(t *testing.T) {
	e, c := setup(t, &scrollES{}, 10, 1<<30)
	for name, f := range map[string]func() error{
		"format":    func() error { _, err := e.Export(context.Background(), c, params(), "xml", "a"); return err },
		"traversal": func() error { _, err := e.Export(context.Background(), c, params(), "csv", "../../etc/x"); return err },
		"empty":     func() error { _, err := e.Export(context.Background(), c, params(), "csv", ""); return err },
	} {
		if err := f(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	for _, bad := range []string{"../x.csv", "x.txt", "a/b.csv", "x.csv/..", ""} {
		if e.Path(bad) != "" {
			t.Errorf("%q must not resolve to a path", bad)
		}
	}
	if e.Path("ok_1.json") == "" {
		t.Error("valid name refused")
	}
}

func TestCleanup(t *testing.T) {
	e, _ := setup(t, &scrollES{}, 10, 1<<30)
	old := filepath.Join(e.opt.Dir, "old.csv")
	fresh := filepath.Join(e.opt.Dir, "fresh.csv")
	for _, p := range []string{old, fresh} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-2 * time.Hour)
	_ = os.Chtimes(old, past, past)
	e.clean(time.Now())
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("old file must be deleted")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Error("fresh file must stay")
	}
}
