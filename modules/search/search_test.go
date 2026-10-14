package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/flant/elasticsearch-extractor/modules/opensearch"
)

func params() Params {
	return Params{
		Index:      "app-*",
		Cluster:    "Search",
		Timefields: []string{"@timestamp"},
		DateStart:  "2026-10-13 10:00:00",
		DateEnd:    "2026-10-13 10:15:00",
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestQueryEscapesValues(t *testing.T) {
	p := params()
	p.Xql = `say "hi"`
	p.Filters = map[string]Filter{"1": {Field: "msg", Operation: "is", Value: `a"} }, {"match_all`}}
	q, err := Query(p)
	if err != nil {
		t.Fatal(err)
	}
	s := mustJSON(t, q)
	if !strings.Contains(s, `"match_phrase":{"msg":"a\"} }, {\"match_all"}`) || !strings.Contains(s, `"query":"say \"hi\""`) {
		t.Errorf("values must stay strings: %s", s)
	}
}

func TestQueryDatesAndFilters(t *testing.T) {
	p := params()
	p.Filters = map[string]Filter{
		"b": {Field: "level", Operation: "is_not", Value: "debug"},
		"a": {Field: "trace_id", Operation: "exists"},
		"c": {Field: "x", Operation: "does_not_exists"},
	}
	q, err := Query(p)
	if err != nil {
		t.Fatal(err)
	}
	s := mustJSON(t, q)
	for _, want := range []string{
		`"gte":"2026-10-13T10:00:00.000Z"`, `"lte":"2026-10-13T10:15:00.000Z"`,
		`"exists":{"field":"trace_id"}`, `"must_not":[{"match_phrase":{"level":"debug"}},{"exists":{"field":"x"}}]`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("no %s in %s", want, s)
		}
	}
}

func TestQueryErrors(t *testing.T) {
	for name, p := range map[string]Params{
		"no index":   {},
		"wrong date": func() Params { p := params(); p.DateStart = "yesterday"; return p }(),
		"wrong op": func() Params {
			p := params()
			p.Filters = map[string]Filter{"1": {Field: "a", Operation: "like"}}
			return p
		}(),
	} {
		if _, err := Query(p); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// без поля времени даты не нужны
	if _, err := Query(Params{Index: "x"}); err != nil {
		t.Error(err)
	}
}

func TestBody(t *testing.T) {
	p := params()
	b, _ := Body(p, 10)
	if got := b["fields"].([]string); len(got) != 1 || got[0] != "@timestamp" || b["_source"] != true {
		t.Errorf("no fields selected: %v", b)
	}
	p.Fields = []string{"msg"}
	p.SearchAfter = `[1791849600000]`
	b, err := Body(p, 10)
	if err != nil {
		t.Fatal(err)
	}
	if got := b["fields"].([]string); len(got) != 2 || b["_source"] != false {
		t.Errorf("fields: %v", b)
	}
	if s := mustJSON(t, b); !strings.Contains(s, `"search_after":[1791849600000]`) || !strings.Contains(s, `"sort":[{"@timestamp":"desc"}]`) {
		t.Errorf("%s", s)
	}
	p.SearchAfter = "not json"
	if _, err := Body(p, 10); !errors.Is(err, ErrInvalid) {
		t.Errorf("bad search_after: %v", err)
	}
}

// es отвечает на поиск страницей из n документов с sort.
func es(t *testing.T, n int, extra func(w http.ResponseWriter, r *http.Request) bool) *opensearch.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if extra != nil && extra(w, r) {
			return
		}
		_, _ = io.ReadAll(r.Body)
		hits := make([]string, n)
		for i := range hits {
			hits[i] = fmt.Sprintf(`{"_id":"%d","_source":{"m":"x"},"sort":[%d]}`, i, 1000-i)
		}
		_, _ = io.WriteString(w, `{"took":3,"hits":{"total":{"value":9999},"hits":[`+strings.Join(hits, ",")+`]}}`)
	}))
	t.Cleanup(srv.Close)
	c, _ := opensearch.New(opensearch.Options{Name: "t", Host: srv.URL + "/"})
	return c
}

func TestSearchPages(t *testing.T) {
	full, err := Search(context.Background(), es(t, PageSize, nil), params())
	if err != nil {
		t.Fatal(err)
	}
	if len(full.Hits.Hits) != PageSize || full.Next != fmt.Sprintf("[%d]", 1000-PageSize+1) {
		t.Errorf("full page: %d hits, next %q", len(full.Hits.Hits), full.Next)
	}
	last, _ := Search(context.Background(), es(t, 3, nil), params())
	if last.Next != "" {
		t.Errorf("a short page is the last one: %q", last.Next)
	}
	// без поля времени продолжать нечем
	p := params()
	p.Timefields = nil
	nt, _ := Search(context.Background(), es(t, PageSize, nil), p)
	if nt.Next != "" {
		t.Errorf("no timefield: %q", nt.Next)
	}
	// пустая выдача - пустой массив, не null
	empty, _ := Search(context.Background(), es(t, 0, nil), params())
	if s := mustJSON(t, empty); !strings.Contains(s, `"hits":{"hits":[]}`) {
		t.Errorf("%s", s)
	}
}

func TestGroupsAndMapping(t *testing.T) {
	now := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	c := es(t, 0, func(w http.ResponseWriter, r *http.Request) bool {
		switch {
		case strings.HasPrefix(r.URL.Path, "/_cat/indices/"):
			if r.URL.Path != "/_cat/indices/*-2026.10.13*,*-13-10-2026,-.*" {
				t.Errorf("path %s", r.URL.Path)
			}
			_, _ = io.WriteString(w, `[{"index":"app-2026.10.13"},{"index":"app-2026.10.13-01"},{"index":"web-13-10-2026"},{"index":"weird"}]`)
		case strings.HasSuffix(r.URL.Path, "/_mapping"):
			_, _ = io.WriteString(w, `{"app-2026.10.13":{"mappings":{"properties":{"@timestamp":{"type":"date"},"k8s":{"properties":{"pod":{"type":"keyword"}}}}}},
				"app-2026.10.13-01":{"mappings":{"properties":{"extra":{"type":"long"}}}},"empty":{"mappings":{}}}`)
		default:
			return false
		}
		return true
	})
	g, err := Groups(context.Background(), c, now)
	if err != nil {
		t.Fatal(err)
	}
	if s := mustJSON(t, g); s != `[{"index":"app-*"},{"index":"web-*"}]` {
		t.Errorf("%s", s)
	}
	m, err := Mapping(context.Background(), c, "app-*", now)
	if err != nil {
		t.Fatal(err)
	}
	if m["@timestamp"] != "date" || m["k8s.pod"] != "keyword" || m["extra"] != "long" || len(m) != 3 {
		t.Errorf("%v", m)
	}
}

func TestInterval(t *testing.T) {
	for span, want := range map[time.Duration]time.Duration{
		10 * time.Second:     time.Second,
		15 * time.Minute:     10 * time.Second,
		2 * time.Hour:        2 * time.Minute,
		24 * time.Hour:       15 * time.Minute,
		7 * 24 * time.Hour:   2 * time.Hour,
		400 * 24 * time.Hour: 7 * 24 * time.Hour,
	} {
		if got := Interval(span); got != want {
			t.Errorf("%v: %v, want %v", span, got, want)
		}
		if got := Interval(span); span/got > HistogramBuckets && got != 7*24*time.Hour {
			t.Errorf("%v: %d buckets", span, span/got)
		}
	}
}

func TestHistogramBody(t *testing.T) {
	p := params()
	p.DateStart, p.DateEnd = "2026-10-11 10:00:00", "2026-10-11 12:00:00"
	body, h, err := HistogramBody(p)
	if err != nil {
		t.Fatal(err)
	}
	s := mustJSON(t, body)
	for _, want := range []string{
		`"size":0`, `"field":"@timestamp"`, `"fixed_interval":"120s"`, `"min_doc_count":0`,
		`"extended_bounds":{"max":1791720000000,"min":1791712800000}`, `"range":{"@timestamp"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("no %s in %s", want, s)
		}
	}
	if h.Interval != 120000 || h.From != 1791712800000 || h.To != 1791720000000 {
		t.Errorf("%+v", h)
	}

	p.Timefields = nil
	if _, _, err := HistogramBody(p); !errors.Is(err, ErrInvalid) {
		t.Errorf("no time field: %v", err)
	}
	p = params()
	p.DateEnd = p.DateStart
	if _, _, err := HistogramBody(p); !errors.Is(err, ErrInvalid) {
		t.Errorf("empty range: %v", err)
	}
}

func TestHistogramOf(t *testing.T) {
	c := es(t, 0, func(w http.ResponseWriter, r *http.Request) bool {
		_, _ = io.WriteString(w, `{"took":1,"hits":{"hits":[]},"aggregations":{"histogram":{"buckets":[
			{"key_as_string":"1791712800000","key":1791712800000,"doc_count":3},
			{"key_as_string":"1791712920000","key":1791712920000,"doc_count":0},
			{"key_as_string":"1791713040000","key":1791713040000,"doc_count":7}]}}}`)
		return true
	})
	p := params()
	p.DateStart, p.DateEnd = "2026-10-11 10:00:00", "2026-10-11 12:00:00"
	h, err := HistogramOf(context.Background(), c, p)
	if err != nil {
		t.Fatal(err)
	}
	if h.Total != 10 || len(h.Buckets) != 3 || h.Buckets[2] != (Bucket{T: 1791713040000, N: 7}) {
		t.Errorf("%+v", h)
	}
}
