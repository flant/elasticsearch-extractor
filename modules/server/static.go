package server

import (
	"bytes"
	"compress/gzip"
	"html"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"
)

// static отдаёт собранный UI из web/dist/ui. Vite называет файлы в assets/ по хешу
// содержимого, поэтому они кэшируются навсегда и сжимаются gzip один раз в памяти;
// страницы не кэшируются, в них подставляется адрес Kibana.
type static struct {
	fsys   fs.FS
	kibana string
	mu     sync.Mutex
	gz     map[string][]byte
}

// Страницы UI по адресам, которые знает backend: / - снапшоты, /search/ - поиск.
var pages = map[string]string{
	"":        "index.html",
	"search":  "search.html",
	"search/": "search.html",
}

func newStatic(fsys fs.FS, kibana string) *static {
	return &static{fsys: fsys, kibana: kibana, gz: map[string][]byte{}}
}

func (s *static) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.fsys == nil {
		http.Error(w, "UI is not built", http.StatusNotFound)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/")
	if p, ok := pages[name]; ok {
		name = p
	} else {
		name = strings.TrimPrefix(path.Clean("/"+name), "/")
	}
	data, err := fs.ReadFile(s.fsys, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ctype := mime.TypeByExtension(path.Ext(name))
	if ctype == "" {
		ctype = http.DetectContentType(data)
	}
	h := w.Header()
	h.Set("Content-Type", ctype)
	if strings.HasSuffix(name, ".html") {
		// Адрес Kibana UI читает из <meta name="xtractor-kibana" content="{{.}}">
		data = bytes.ReplaceAll(data, []byte("{{.}}"), []byte(html.EscapeString(s.kibana)))
		h.Set("Cache-Control", "no-cache")
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
		return
	}
	if strings.HasPrefix(name, "assets/") {
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	h.Add("Vary", "Accept-Encoding")
	if len(data) > 1024 && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") && compressible(ctype) {
		h.Set("Content-Encoding", "gzip")
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(s.gzipped(name, data)))
		return
	}
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
}

func (s *static) gzipped(name string, data []byte) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if z, ok := s.gz[name]; ok {
		return z
	}
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	_, _ = io.Copy(zw, bytes.NewReader(data))
	_ = zw.Close()
	s.gz[name] = buf.Bytes()
	return s.gz[name]
}

func compressible(ctype string) bool {
	for _, p := range []string{"text/", "application/javascript", "application/json", "image/svg"} {
		if strings.HasPrefix(ctype, p) {
			return true
		}
	}
	return false
}
