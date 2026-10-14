package server

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func uiFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":           {Data: []byte(`<meta name="xtractor-kibana" content="{{.}}"><title>Снапшоты</title>`)},
		"search.html":          {Data: []byte(`<title>Поиск</title>`)},
		"assets/index-abc.js":  {Data: []byte(strings.Repeat("console.log(1);", 200))},
		"assets/index-abc.css": {Data: []byte("body{}")},
	}
}

func get(t *testing.T, h http.Handler, path string, hdr ...string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Result()
}

func body(t *testing.T, r *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestStaticPages(t *testing.T) {
	s := newStatic(uiFS(), `kibana.example.com"><script>`)
	for path, want := range map[string]string{"/": "Снапшоты", "/search/": "Поиск", "/search": "Поиск"} {
		r := get(t, s, path)
		b := body(t, r)
		if r.StatusCode != http.StatusOK || !strings.Contains(b, want) {
			t.Errorf("%s: %d %q", path, r.StatusCode, b)
		}
		if r.Header.Get("Cache-Control") != "no-cache" {
			t.Errorf("%s: pages must not be cached", path)
		}
	}
	// адрес Kibana подставляется экранированным
	if b := body(t, get(t, s, "/")); !strings.Contains(b, `content="kibana.example.com&#34;&gt;&lt;script&gt;"`) {
		t.Errorf("kibana is not escaped: %s", b)
	}
}

func TestStaticAssets(t *testing.T) {
	s := newStatic(uiFS(), "")
	r := get(t, s, "/assets/index-abc.js", "Accept-Encoding", "gzip")
	if r.StatusCode != http.StatusOK || r.Header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("status %d, encoding %q", r.StatusCode, r.Header.Get("Content-Encoding"))
	}
	if !strings.Contains(r.Header.Get("Cache-Control"), "immutable") {
		t.Error("hashed assets must be cached forever")
	}
	zr, err := gzip.NewReader(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := io.ReadAll(zr); !strings.HasPrefix(string(b), "console.log(1);") {
		t.Error("wrong gzipped body")
	}
	if r := get(t, s, "/assets/index-abc.css"); r.Header.Get("Content-Encoding") != "" {
		t.Error("small files are not compressed")
	}
}

func TestStaticMissing(t *testing.T) {
	s := newStatic(uiFS(), "")
	for _, p := range []string{"/nope.html", "/../../etc/passwd", "/assets/missing.js"} {
		if r := get(t, s, p); r.StatusCode != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", p, r.StatusCode)
		}
	}
	if r := get(t, newStatic(nil, ""), "/"); r.StatusCode != http.StatusNotFound || !strings.Contains(body(t, r), "UI is not built") {
		t.Error("without a build the server must say that the UI is not built")
	}
}
