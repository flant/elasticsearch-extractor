package snapshots

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/flant/elasticsearch-extractor/modules/opensearch"
)

// fakeES отвечает на запросы восстановления и запоминает их.
type fakeES struct {
	mu           sync.Mutex
	calls        []string
	initializing int
}

func (f *fakeES) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path+" "+string(b))
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	p := r.URL.Path
	switch {
	case strings.HasSuffix(p, "/_status"):
		// small: 2 шарда по 1 ГБ, huge: 1 шард на 5000 ГБ, empty: пустой шард
		_, _ = io.WriteString(w, `{"snapshots":[{"snapshot":"s","state":"SUCCESS","indices":{
			"small":{"shards_stats":{"total":2},"stats":{"total":{"size_in_bytes":2147483648}},"shards":{"0":{"stats":{"total":{"size_in_bytes":1073741824}}},"1":{"stats":{"total":{"size_in_bytes":1073741824}}}}},
			"huge":{"shards_stats":{"total":1},"stats":{"total":{"size_in_bytes":5368709120000}},"shards":{"0":{"stats":{"total":{"size_in_bytes":5368709120000}}}}},
			"empty":{"shards_stats":{"total":1},"stats":{"total":{"size_in_bytes":0}},"shards":{"0":{"stats":{"total":{"size_in_bytes":0}}}}}}}]}`)
	case strings.HasPrefix(p, "/_cluster/health"):
		_, _ = io.WriteString(w, `{"status":"green","initializing_shards":`+itoa(f.initializing)+`,"unassigned_shards":0}`)
	case strings.HasPrefix(p, "/_cat/nodes"):
		_, _ = io.WriteString(w, `[{"ip":"1","name":"a","dt":"0","du":"0","dup":"50","d":"107374182400"}]`)
	case strings.HasSuffix(p, "/_restore"):
		_, _ = io.WriteString(w, `{"accepted":true}`)
	case strings.HasPrefix(p, "/_snapshot/"):
		_, _ = io.WriteString(w, `{"snapshots":[{"snapshot":"logs-2026.10.12","uuid":"a","state":"SUCCESS"},
			{"snapshot":"logs-2026.10.13","uuid":"b","state":"IN_PROGRESS"},{"snapshot":"manual","uuid":"c"},
			{"snapshot":".kibana-2026.10.13","uuid":"d"},{"snapshot":"bad-2026.13.40","uuid":"e"}]}`)
	case r.Method == http.MethodDelete:
		_, _ = io.WriteString(w, `{"acknowledged":true}`)
	default:
		w.WriteHeader(404)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func (f *fakeES) restored() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		if strings.Contains(c, "/_restore") {
			out = append(out, c)
		}
	}
	return out
}

func service(t *testing.T, s3 bool) (*Service, *fakeES) {
	t.Helper()
	f := &fakeES{}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	c, err := opensearch.New(opensearch.Options{Name: "t", Host: srv.URL + "/"})
	if err != nil {
		t.Fatal(err)
	}
	now := func() time.Time { return time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC) }
	return New(Options{Client: c, S3: s3, Now: now}), f
}

func TestPlan(t *testing.T) {
	restore, skip := Plan(map[string][]int64{
		"fits":  {10, 10},
		"big":   {1000},
		"empty": {0},
	}, []int64{100}, false)
	if strings.Join(restore, ",") != "empty,fits" || strings.Join(skip, ",") != "big" {
		t.Errorf("restore %v, skip %v", restore, skip)
	}
	// в S3 место не проверяется
	restore, skip = Plan(map[string][]int64{"big": {1000}}, nil, true)
	if len(restore) != 1 || len(skip) != 0 {
		t.Errorf("s3: restore %v, skip %v", restore, skip)
	}
}

func TestList(t *testing.T) {
	s, _ := service(t, false)
	items, err := s.List(context.Background(), "repo", "time", "desc")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, i := range items {
		names = append(names, i.Snapshot)
	}
	// без даты, системные и с неверной датой пропускаются; новые первыми
	if strings.Join(names, ",") != "logs-2026.10.13,logs-2026.10.12" {
		t.Errorf("%v", names)
	}
	if items[0].CreateDate != "2026.10.13" || items[0].State != "IN_PROGRESS" {
		t.Errorf("%+v", items[0])
	}
	cached, _ := s.Cached(context.Background(), "repo", "name", "asc")
	if cached[0].Snapshot != "logs-2026.10.12" {
		t.Errorf("cached sort: %+v", cached)
	}
	if _, err := s.List(context.Background(), "", "", ""); !errors.Is(err, ErrInvalid) {
		t.Errorf("empty repo: %v", err)
	}
}

func TestRestoreSkipsWhatDoesNotFit(t *testing.T) {
	s, f := service(t, false)
	res, err := s.Restore(context.Background(), "repo", "s", []string{"small", "huge"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(res.Restoring, ",") != "small" || strings.Join(res.Skipped, ",") != "huge" {
		t.Errorf("%+v", res)
	}
	calls := f.restored()
	if len(calls) != 1 || !strings.Contains(calls[0], `"indices":["small"]`) || !strings.Contains(calls[0], `extracted_$1-13-10-2026`) {
		t.Errorf("restore call: %v", calls)
	}
}

func TestRestoreNothingFits(t *testing.T) {
	s, f := service(t, false)
	res, err := s.Restore(context.Background(), "repo", "s", []string{"huge"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Restoring) != 0 || strings.Join(res.Skipped, ",") != "huge" {
		t.Errorf("%+v", res)
	}
	// кластер не вызывается: пустой список индексов он понял бы как "все"
	if calls := f.restored(); len(calls) != 0 {
		t.Errorf("restore must not be called: %v", calls)
	}
}

func TestRestoreS3(t *testing.T) {
	s, f := service(t, true)
	res, err := s.Restore(context.Background(), "repo", "s", []string{"huge"})
	if err != nil || len(res.Restoring) != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	for _, c := range f.calls {
		if strings.Contains(c, "_cat/nodes") {
			t.Error("nodes are not checked for S3")
		}
	}
}

func TestRestoreErrors(t *testing.T) {
	s, f := service(t, false)
	if _, err := s.Restore(context.Background(), "repo", "s", []string{"missing"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("missing index: %v", err)
	}
	if _, err := s.Restore(context.Background(), "repo", "s", nil); !errors.Is(err, ErrInvalid) {
		t.Errorf("no indices: %v", err)
	}
	f.initializing = 9
	if _, err := s.Restore(context.Background(), "repo", "s", []string{"small"}); !errors.Is(err, ErrBusy) {
		t.Errorf("busy: %v", err)
	}
}

func TestDelete(t *testing.T) {
	s, f := service(t, false)
	for _, bad := range []string{"*", "_all", "logs-2026", "extracted_", "extracted_*", "extracted_a,logs", "extracted_a/b"} {
		if err := s.Delete(context.Background(), bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q must be refused: %v", bad, err)
		}
	}
	if len(f.calls) != 0 {
		t.Errorf("refused deletes must not reach the cluster: %v", f.calls)
	}
	if err := s.Delete(context.Background(), "extracted_app-2026.10.12-13-10-2026"); err != nil {
		t.Error(err)
	}
}
