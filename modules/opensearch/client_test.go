package opensearch

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type seen struct {
	method, path, body, user, pass string
}

func fake(t *testing.T, status int, resp string) (*Client, *seen) {
	t.Helper()
	s := &seen{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		s.method, s.path, s.body = r.Method, r.URL.EscapedPath()+"?"+r.URL.RawQuery, string(b)
		s.user, s.pass, _ = r.BasicAuth()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(resp))
	}))
	t.Cleanup(srv.Close)
	c, err := New(Options{Name: "test", Host: srv.URL + "/", Username: "u", Password: "p"})
	if err != nil {
		t.Fatal(err)
	}
	return c, s
}

func TestEscaping(t *testing.T) {
	c, s := fake(t, 200, `{"snapshots":[]}`)
	if _, err := c.SnapshotStatus(context.Background(), "repo/../x", "snap?y"); err != nil {
		t.Fatal(err)
	}
	if s.path != "/_snapshot/repo%2F..%2Fx/snap%3Fy/_status?" {
		t.Errorf("names must not change the request path: %s", s.path)
	}
	if s.user != "u" || s.pass != "p" {
		t.Error("basic auth is not sent")
	}
}

func TestPatternsKeepWildcards(t *testing.T) {
	c, s := fake(t, 200, `[{"index":"app-2026.10.13"}]`)
	got, err := c.Indices(context.Background(), "*-2026.10.13*", "-.*")
	if err != nil || len(got) != 1 || got[0] != "app-2026.10.13" {
		t.Fatalf("%v %v", got, err)
	}
	if !strings.HasPrefix(s.path, "/_cat/indices/*-2026.10.13*,-.*?") {
		t.Errorf("path %s", s.path)
	}
}

func TestErrors(t *testing.T) {
	c, _ := fake(t, 404, `{"error":{"root_cause":[{"type":"index_not_found_exception","reason":"no such index [x]"}],"type":"index_not_found_exception","reason":"no such index [x]"},"status":404}`)
	err := c.DeleteIndex(context.Background(), "x")
	if !IsNotFound(err) || !strings.Contains(err.Error(), "no such index [x]") {
		t.Errorf("%v", err)
	}

	c, _ = fake(t, 500, `{"error":"plain string"}`)
	if _, err := c.Repositories(context.Background()); err == nil || !strings.Contains(err.Error(), "plain string") {
		t.Errorf("%v", err)
	}

	c, _ = fake(t, 502, `<html>bad gateway</html>`)
	if _, err := c.Repositories(context.Background()); err == nil || !strings.Contains(err.Error(), "HTTP 502") {
		t.Errorf("%v", err)
	}
}

func TestNodes(t *testing.T) {
	c, _ := fake(t, 200, `[{"ip":"10.0.0.1","name":"n1","dt":"2000","du":"1500","dup":"75.00","d":"500"}]`)
	n, err := c.Nodes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n[0].Total != 2000 || n[0].Used != 1500 || n[0].Avail != 500 || n[0].UsedPercent != 75 {
		t.Errorf("%+v", n[0])
	}
	// ключи JSON остаются как у _cat/nodes - их читает UI
	b, _ := json.Marshal(n[0])
	if !strings.Contains(string(b), `"d":500`) || !strings.Contains(string(b), `"dup":75`) {
		t.Errorf("%s", b)
	}
}

func TestRestoreBody(t *testing.T) {
	c, s := fake(t, 200, `{"accepted":true}`)
	err := c.Restore(context.Background(), "r", "s", RestoreRequest{Indices: []string{"a"}, RenamePattern: "(.+)", RenameReplacement: "extracted_$1-13-10-2026"})
	if err != nil {
		t.Fatal(err)
	}
	if s.method != http.MethodPost || !strings.HasPrefix(s.path, "/_snapshot/r/s/_restore?wait_for_completion=false") {
		t.Errorf("%s %s", s.method, s.path)
	}
	if !strings.Contains(s.body, `"indices":["a"]`) || !strings.Contains(s.body, `"include_global_state":false`) {
		t.Errorf("%s", s.body)
	}
}

func TestTLSErrors(t *testing.T) {
	if _, err := New(Options{Name: "x", Host: "https://h/", CACert: "/nonexistent.pem"}); err == nil {
		t.Error("missing CA file must be an error, not a crash")
	}
}
