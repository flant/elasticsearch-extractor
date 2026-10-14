package opensearch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

/* ---------- Репозитории, узлы, снапшоты ---------- */

type Repository struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

func (c *Client) Repositories(ctx context.Context) ([]Repository, error) {
	var out []Repository
	err := c.do(ctx, "cat_repositories", http.MethodGet, "_cat/repositories?format=json", nil, &out)
	return out, err
}

// Node - диск узла в байтах. JSON-ключи - как у _cat/nodes, их ждёт UI.
type Node struct {
	IP          string  `json:"ip"`
	Name        string  `json:"name"`
	Total       int64   `json:"dt"`
	Used        int64   `json:"du"`
	UsedPercent float64 `json:"dup"`
	Avail       int64   `json:"d"`
}

func (c *Client) Nodes(ctx context.Context) ([]Node, error) {
	var raw []struct {
		IP   string `json:"ip"`
		Name string `json:"name"`
		Dt   string `json:"dt"`
		Du   string `json:"du"`
		Dup  string `json:"dup"`
		D    string `json:"d"`
	}
	if err := c.do(ctx, "cat_nodes", http.MethodGet, "_cat/nodes?format=json&bytes=b&h=ip,name,dt,du,dup,d&s=name", nil, &raw); err != nil {
		return nil, err
	}
	out := make([]Node, 0, len(raw))
	for _, n := range raw {
		dup, _ := strconv.ParseFloat(n.Dup, 64)
		out = append(out, Node{IP: n.IP, Name: n.Name, Total: atoi64(n.Dt), Used: atoi64(n.Du), UsedPercent: dup, Avail: atoi64(n.D)})
	}
	return out, nil
}

func atoi64(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

type SnapshotInfo struct {
	Snapshot string `json:"snapshot"`
	UUID     string `json:"uuid"`
	State    string `json:"state,omitempty"`
}

// Snapshots - снапшоты репозитория без подробностей (verbose=false: быстро и для S3).
func (c *Client) Snapshots(ctx context.Context, repo string) ([]SnapshotInfo, error) {
	var out struct {
		Snapshots []SnapshotInfo `json:"snapshots"`
	}
	err := c.do(ctx, "get_snapshots", http.MethodGet, "_snapshot/"+seg(repo)+"/*?verbose=false", nil, &out)
	return out.Snapshots, err
}

// SnapshotStatus - ответ _snapshot/<repo>/<snapshot>/_status в части, нужной extractor.
// JSON-ключи - как у Elasticsearch: UI читает этот ответ как есть.
type SnapshotStatus struct {
	Snapshots []struct {
		Snapshot string                 `json:"snapshot"`
		State    string                 `json:"state"`
		Indices  map[string]IndexStatus `json:"indices"`
	} `json:"snapshots"`
}

type IndexStatus struct {
	ShardsStats struct {
		Total int `json:"total"`
	} `json:"shards_stats"`
	Stats struct {
		Total struct {
			Size int64 `json:"size_in_bytes"`
		} `json:"total"`
	} `json:"stats"`
	Shards map[string]struct {
		Stats struct {
			Total struct {
				Size int64 `json:"size_in_bytes"`
			} `json:"total"`
		} `json:"stats"`
	} `json:"shards"`
}

// ShardSizes - размеры шардов индекса в байтах.
func (s IndexStatus) ShardSizes() []int64 {
	out := make([]int64, 0, len(s.Shards))
	for _, sh := range s.Shards {
		out = append(out, sh.Stats.Total.Size)
	}
	return out
}

func (c *Client) SnapshotStatus(ctx context.Context, repo, snapshot string) (*SnapshotStatus, error) {
	var out SnapshotStatus
	err := c.do(ctx, "snapshot_status", http.MethodGet, "_snapshot/"+seg(repo)+"/"+seg(snapshot)+"/_status", nil, &out)
	return &out, err
}

type Health struct {
	Status             string `json:"status"`
	InitializingShards int    `json:"initializing_shards"`
	UnassignedShards   int    `json:"unassigned_shards"`
}

func (c *Client) Health(ctx context.Context, pattern string) (*Health, error) {
	var out Health
	err := c.do(ctx, "cluster_health", http.MethodGet, "_cluster/health/"+seg(pattern), nil, &out)
	return &out, err
}

// RestoreRequest - тело _restore; индексы обязательны: пустой список ES понял бы как "все".
type RestoreRequest struct {
	Indices            []string       `json:"indices"`
	IgnoreUnavailable  bool           `json:"ignore_unavailable"`
	IncludeGlobalState bool           `json:"include_global_state"`
	IncludeAliases     bool           `json:"include_aliases"`
	RenamePattern      string         `json:"rename_pattern"`
	RenameReplacement  string         `json:"rename_replacement"`
	IndexSettings      map[string]any `json:"index_settings,omitempty"`
}

func (c *Client) Restore(ctx context.Context, repo, snapshot string, r RestoreRequest) error {
	return c.do(ctx, "restore", http.MethodPost, "_snapshot/"+seg(repo)+"/"+seg(snapshot)+"/_restore?wait_for_completion=false", r, nil)
}

// IndexRecovery - _recovery индекса; JSON-ключи как у Elasticsearch, UI читает их.
type IndexRecovery struct {
	Shards []ShardRecovery `json:"shards"`
}

type ShardRecovery struct {
	Type      string `json:"type"`
	Stage     string `json:"stage"`
	StartTime int64  `json:"start_time_in_millis,omitempty"`
	Source    struct {
		Repository string `json:"repository,omitempty"`
		Snapshot   string `json:"snapshot,omitempty"`
		Index      string `json:"index,omitempty"`
	} `json:"source"`
	Index struct {
		Size struct {
			Total     int64  `json:"total_in_bytes"`
			Recovered int64  `json:"recovered_in_bytes"`
			Percent   string `json:"percent"`
		} `json:"size"`
	} `json:"index"`
}

func (c *Client) Recovery(ctx context.Context, pattern string) (map[string]IndexRecovery, error) {
	out := map[string]IndexRecovery{}
	err := c.do(ctx, "recovery", http.MethodGet, seg(pattern)+"/_recovery", nil, &out)
	return out, err
}

func (c *Client) DeleteIndex(ctx context.Context, index string) error {
	return c.do(ctx, "delete_index", http.MethodDelete, seg(index), nil, nil)
}

/* ---------- Индексы и поиск ---------- */

// Indices - имена индексов по маскам через запятую; "-.*" исключает системные.
func (c *Client) Indices(ctx context.Context, patterns ...string) ([]string, error) {
	var raw []struct {
		Index string `json:"index"`
	}
	escaped := make([]string, len(patterns))
	for i, p := range patterns {
		escaped[i] = seg(p)
	}
	err := c.do(ctx, "cat_indices", http.MethodGet, "_cat/indices/"+strings.Join(escaped, ",")+"?format=json&h=index", nil, &raw)
	out := make([]string, 0, len(raw))
	for _, r := range raw {
		out = append(out, r.Index)
	}
	return out, err
}

// Mapping - маппинги индексов по маскам: индекс -> {"mappings": {...}}.
func (c *Client) Mapping(ctx context.Context, patterns ...string) (map[string]struct {
	Mappings map[string]any `json:"mappings"`
}, error) {
	out := map[string]struct {
		Mappings map[string]any `json:"mappings"`
	}{}
	escaped := make([]string, len(patterns))
	for i, p := range patterns {
		escaped[i] = seg(p)
	}
	err := c.do(ctx, "get_mapping", http.MethodGet, strings.Join(escaped, ",")+"/_mapping", nil, &out)
	return out, err
}

type Hit struct {
	Index  string          `json:"_index,omitempty"`
	ID     string          `json:"_id,omitempty"`
	Source map[string]any  `json:"_source,omitempty"`
	Fields map[string]any  `json:"fields,omitempty"`
	Sort   json.RawMessage `json:"sort,omitempty"`
}

type SearchResponse struct {
	Took     int    `json:"took"`
	ScrollID string `json:"_scroll_id,omitempty"`
	Hits     struct {
		Total struct {
			Value    int64  `json:"value"`
			Relation string `json:"relation,omitempty"`
		} `json:"total"`
		Hits []Hit `json:"hits"`
	} `json:"hits"`
}

// Search - _search по индексу (маске); scroll - время жизни scroll-контекста или "".
func (c *Client) Search(ctx context.Context, index string, body any, scroll string) (*SearchResponse, error) {
	path := seg(index) + "/_search"
	if scroll != "" {
		path += "?scroll=" + url.QueryEscape(scroll)
	}
	var out SearchResponse
	err := c.do(ctx, "search", http.MethodPost, path, body, &out)
	return &out, err
}

func (c *Client) Scroll(ctx context.Context, id, keep string) (*SearchResponse, error) {
	var out SearchResponse
	err := c.do(ctx, "scroll", http.MethodPost, "_search/scroll", map[string]string{"scroll": keep, "scroll_id": id}, &out)
	return &out, err
}

func (c *Client) ClearScroll(ctx context.Context, id string) error {
	return c.do(ctx, "clear_scroll", http.MethodDelete, "_search/scroll", map[string][]string{"scroll_id": {id}}, nil)
}

func (c *Client) Count(ctx context.Context, index string, body any) (int64, error) {
	var out struct {
		Count int64 `json:"count"`
	}
	err := c.do(ctx, "count", http.MethodPost, seg(index)+"/_count", body, &out)
	return out.Count, err
}
