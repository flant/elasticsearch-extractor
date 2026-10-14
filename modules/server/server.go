// Package server - HTTP extractor: API (POST /api/ с полем action), скачивание выгрузок,
// встроенный UI, проверки. Аутентификация - задача ingress перед сервисом.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/flant/elasticsearch-extractor/modules/export"
	"github.com/flant/elasticsearch-extractor/modules/opensearch"
	"github.com/flant/elasticsearch-extractor/modules/search"
	"github.com/flant/elasticsearch-extractor/modules/snapshots"
)

const (
	// Типы кластеров, как их присылает UI в search.cluster
	ClusterSnapshot = "Snapshot"
	ClusterSearch   = "Search"

	apiTimeout = 2 * time.Minute
	// Выгрузка большого индекса идёт долго; запрос живёт, пока клиент ждёт
	exportTimeout = 30 * time.Minute
	maxBody       = 1 << 20
)

type Options struct {
	Snapshot  *opensearch.Client // кластер со снапшотами
	Search    *opensearch.Client // кластер для поиска
	Snapshots *snapshots.Service
	Exporter  *export.Exporter
	UI        fs.FS // собранный UI, nil - не собран
	Kibana    string
	Version   string
	Log       *slog.Logger
	Now       func() time.Time
}

type Server struct {
	opt    Options
	static *static
}

func New(o Options) *Server {
	if o.Log == nil {
		o.Log = slog.Default()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	s := &Server{opt: o, static: newStatic(o.UI, o.Kibana)}
	return s
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/", s.api)
	mux.HandleFunc("/data/", s.download)
	mux.Handle("/", s.static)
	return s.accessLog(securityHeaders(mux))
}

// Запрос к /api/. Поля прежние; search - параметры поиска и выгрузок.
type apiRequest struct {
	Action string `json:"action"`
	Values struct {
		Indices   []string `json:"indices,omitempty"`
		Repo      string   `json:"repo,omitempty"`
		OrderDir  string   `json:"odir,omitempty"`
		OrderType string   `json:"otype,omitempty"`
		Snapshot  string   `json:"snapshot,omitempty"`
		Index     string   `json:"index,omitempty"`
	} `json:"values"`
	Search search.Params `json:"search"`
}

type cluster struct {
	Name string
	Host string
	Type string
}

// errBadRequest - ошибка в запросе клиента.
var errBadRequest = errors.New("bad request")

func (s *Server) api(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req apiRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad JSON: "+err.Error())
		return
	}
	info(r).action = req.Action

	timeout := apiTimeout
	if req.Action == "prepare_csv" || req.Action == "prepare_json" {
		timeout = exportTimeout
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	resp, err := s.handle(ctx, r, req)
	code := http.StatusOK
	if err != nil {
		code = status(err)
		if code >= 500 {
			s.opt.Log.Error("api error", "action", req.Action, "error", err)
		}
		writeError(w, code, err.Error())
		return
	}
	writeJSON(w, resp)
}

func (s *Server) handle(ctx context.Context, r *http.Request, req apiRequest) (any, error) {
	v := req.Values
	switch req.Action {
	case "get_config":
		rows, size := s.opt.Exporter.Limits()
		return map[string]any{
			"kibana":   s.opt.Kibana,
			"version":  s.opt.Version,
			"clusters": s.clusters(),
			"limits":   map[string]any{"rows": rows, "size": size},
		}, nil
	case "get_clusters":
		return s.clusters(), nil
	case "get_repositories":
		return s.opt.Snapshot.Repositories(ctx)
	case "get_nodes":
		return s.opt.Snapshot.Nodes(ctx)
	case "get_snapshots":
		return s.opt.Snapshots.List(ctx, v.Repo, v.OrderType, v.OrderDir)
	case "get_snapshots_sorted":
		return s.opt.Snapshots.Cached(ctx, v.Repo, v.OrderType, v.OrderDir)
	case "get_snapshot":
		return s.opt.Snapshots.Status(ctx, v.Repo, v.Snapshot)
	case "restore":
		res, err := s.opt.Snapshots.Restore(ctx, v.Repo, v.Snapshot, v.Indices)
		if err == nil {
			s.opt.Log.Info("restore", "user", who(r), "repository", v.Repo, "snapshot", v.Snapshot,
				"restoring", res.Restoring, "skipped", res.Skipped)
		}
		return res, err
	case "get_indices":
		return s.opt.Snapshots.Restored(ctx)
	case "del_index":
		if err := s.opt.Snapshots.Delete(ctx, v.Index); err != nil {
			return nil, err
		}
		s.opt.Log.Info("restored index deleted", "user", who(r), "index", v.Index)
		return map[string]bool{"acknowledged": true}, nil
	case "get_index_groups":
		c, err := s.cluster(req.Search.Cluster)
		if err != nil {
			return nil, err
		}
		return search.Groups(ctx, c, s.opt.Now())
	case "get_mapping":
		c, err := s.cluster(req.Search.Cluster)
		if err != nil {
			return nil, err
		}
		return search.Mapping(ctx, c, req.Search.Index, s.opt.Now())
	case "search":
		c, err := s.cluster(req.Search.Cluster)
		if err != nil {
			return nil, err
		}
		if req.Search.Count {
			n, err := search.Count(ctx, c, req.Search)
			return map[string]int64{"count": n}, err
		}
		return search.Search(ctx, c, req.Search)
	case "prepare_csv", "prepare_json":
		c, err := s.cluster(req.Search.Cluster)
		if err != nil {
			return nil, err
		}
		format := strings.TrimPrefix(req.Action, "prepare_")
		start := time.Now()
		res, err := s.opt.Exporter.Export(ctx, c, req.Search, format, req.Search.Fname)
		if err == nil {
			s.opt.Log.Info("export", "user", who(r), "cluster", req.Search.Cluster, "index", req.Search.Index,
				"file", res.File, "rows", res.Rows, "bytes", res.Size, "truncated", res.Truncated,
				"duration", time.Since(start).Round(time.Millisecond).String())
		}
		return res, err
	}
	return nil, fmt.Errorf("%w: unknown action %q", errBadRequest, req.Action)
}

func (s *Server) clusters() []cluster {
	return []cluster{
		{Name: s.opt.Snapshot.Name(), Host: s.opt.Snapshot.Host(), Type: ClusterSnapshot},
		{Name: s.opt.Search.Name(), Host: s.opt.Search.Host(), Type: ClusterSearch},
	}
}

func (s *Server) cluster(t string) (*opensearch.Client, error) {
	switch t {
	case ClusterSnapshot:
		return s.opt.Snapshot, nil
	case ClusterSearch:
		return s.opt.Search, nil
	}
	return nil, fmt.Errorf("%w: unknown cluster %q", errBadRequest, t)
}

// status - HTTP-код ответа по ошибке.
func status(err error) int {
	var oe *opensearch.Error
	switch {
	case errors.Is(err, errBadRequest), errors.Is(err, snapshots.ErrInvalid),
		errors.Is(err, search.ErrInvalid), errors.Is(err, export.ErrInvalid):
		return http.StatusBadRequest
	case errors.Is(err, snapshots.ErrBusy):
		return http.StatusTooManyRequests
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout
	case errors.As(err, &oe) && oe.Status == http.StatusNotFound:
		return http.StatusNotFound
	case errors.As(err, &oe) && oe.Status == http.StatusBadRequest:
		// кластер не принял запрос: неверная строка поиска, поле и т.п.
		return http.StatusBadRequest
	}
	return http.StatusBadGateway
}

// download отдаёт готовый файл выгрузки по имени.
func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/data/")
	p := s.opt.Exporter.Path(name)
	if p == "" {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(p)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ctype := "text/csv; charset=utf-8"
	if path.Ext(name) == ".json" {
		ctype = "application/x-ndjson"
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeContent(w, r, name, st.ModTime(), f)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// writeError - ошибки API всегда в JSON: {"error": "..."}.
func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
