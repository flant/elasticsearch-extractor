// Package search - поиск по индексам: построение запроса из параметров UI, выдача
// страницами, подсчёт, группы индексов и поля. Отбирает найденное сам кластер: в
// результатах только документы, подходящие под строку поиска, фильтры и время.
package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"time"

	"github.com/flant/elasticsearch-extractor/modules/opensearch"
)

// PageSize - сколько документов отдаётся UI за один запрос.
const PageSize = 500

// ErrInvalid - неверные параметры поиска.
var ErrInvalid = errors.New("invalid search request")

type Filter struct {
	Field     string `json:"field,omitempty"`
	Operation string `json:"operation,omitempty"`
	Value     string `json:"value,omitempty"`
}

// Params - параметры поиска; JSON-ключи - поле "search" запроса к /api/.
type Params struct {
	Index      string            `json:"index,omitempty"`
	Cluster    string            `json:"cluster,omitempty"`
	Xql        string            `json:"xql,omitempty"`
	Fields     []string          `json:"fields,omitempty"`
	Filters    map[string]Filter `json:"filters,omitempty"`
	Mapping    []string          `json:"mapping,omitempty"`
	Timefields []string          `json:"timefields,omitempty"`
	DateStart  string            `json:"date_start,omitempty"`
	DateEnd    string            `json:"date_end,omitempty"`
	// Продолжение выдачи: значение next из предыдущего ответа
	SearchAfter string `json:"search_after,omitempty"`
	Count       bool   `json:"count,omitempty"`
	Fname       string `json:"fname,omitempty"`
}

// Timefield - поле времени для промежутка и сортировки, "" - его нет.
func (p Params) Timefield() string {
	if len(p.Timefields) > 0 {
		return p.Timefields[0]
	}
	return ""
}

// Формат дат из UI: время в UTC.
const dateLayout = "2006-01-02 15:04:05"

// Query - тело "query" для _search и _count. Строится через map и json.Marshal, поэтому
// кавычки и спецсимволы в значениях не ломают запрос.
func Query(p Params) (map[string]any, error) {
	if p.Index == "" {
		return nil, fmt.Errorf("%w: index is required", ErrInvalid)
	}
	must := []any{}
	filter := []any{}
	mustNot := []any{}

	if p.Xql != "" {
		must = append(must, map[string]any{"simple_query_string": map[string]any{"query": p.Xql}})
	}
	if tf := p.Timefield(); tf != "" {
		ds, err := time.Parse(dateLayout, p.DateStart)
		if err != nil {
			return nil, fmt.Errorf("%w: date_start: %v", ErrInvalid, err)
		}
		de, err := time.Parse(dateLayout, p.DateEnd)
		if err != nil {
			return nil, fmt.Errorf("%w: date_end: %v", ErrInvalid, err)
		}
		filter = append(filter, map[string]any{"range": map[string]any{tf: map[string]any{
			"gte":    ds.Format("2006-01-02T15:04:05.000Z"),
			"lte":    de.Format("2006-01-02T15:04:05.000Z"),
			"format": "strict_date_optional_time",
		}}})
	}
	// Фильтры в порядке ключей: одинаковые параметры дают одинаковый запрос
	ids := make([]string, 0, len(p.Filters))
	for id := range p.Filters {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		f := p.Filters[id]
		switch f.Operation {
		case "is":
			filter = append(filter, map[string]any{"match_phrase": map[string]any{f.Field: f.Value}})
		case "exists":
			filter = append(filter, map[string]any{"exists": map[string]any{"field": f.Field}})
		case "is_not":
			mustNot = append(mustNot, map[string]any{"match_phrase": map[string]any{f.Field: f.Value}})
		case "does_not_exists":
			mustNot = append(mustNot, map[string]any{"exists": map[string]any{"field": f.Field}})
		default:
			return nil, fmt.Errorf("%w: unknown filter operation %q", ErrInvalid, f.Operation)
		}
	}
	filter = append(filter, map[string]any{"match_all": map[string]any{}})

	return map[string]any{"bool": map[string]any{
		"must": must, "filter": filter, "should": []any{}, "must_not": mustNot,
	}}, nil
}

// Body - полное тело _search: размер, сортировка по времени (новые первыми), поля и
// продолжение выдачи после p.SearchAfter.
func Body(p Params, size int64) (map[string]any, error) {
	q, err := Query(p)
	if err != nil {
		return nil, err
	}
	fields := []string{}
	body := map[string]any{"size": size, "_source": len(p.Fields) == 0, "query": q}
	if tf := p.Timefield(); tf != "" {
		fields = append(fields, tf)
		body["sort"] = []any{map[string]any{tf: "desc"}}
		if p.SearchAfter != "" {
			var after []any
			if err := json.Unmarshal([]byte(p.SearchAfter), &after); err != nil || len(after) == 0 {
				return nil, fmt.Errorf("%w: search_after", ErrInvalid)
			}
			body["search_after"] = after
		}
	}
	body["fields"] = append(fields, p.Fields...)
	return body, nil
}

// Result - страница выдачи. JSON повторяет форму ответа _search (hits.hits), чтобы UI
// читал её так же; Next - продолжение выдачи, "" - страниц больше нет.
type Result struct {
	Took int `json:"took"`
	Hits struct {
		Hits []opensearch.Hit `json:"hits"`
	} `json:"hits"`
	Next string `json:"next,omitempty"`
}

// Search - страница из PageSize документов. Следующая страница - по Next, если есть
// поле времени: выдача идёт после последнего документа (search_after).
func Search(ctx context.Context, c *opensearch.Client, p Params) (*Result, error) {
	body, err := Body(p, PageSize)
	if err != nil {
		return nil, err
	}
	resp, err := c.Search(ctx, p.Index, body, "")
	if err != nil {
		return nil, err
	}
	out := &Result{Took: resp.Took}
	out.Hits.Hits = resp.Hits.Hits
	if out.Hits.Hits == nil {
		out.Hits.Hits = []opensearch.Hit{}
	}
	if n := len(resp.Hits.Hits); n == PageSize && p.Timefield() != "" && len(resp.Hits.Hits[n-1].Sort) > 0 {
		out.Next = string(resp.Hits.Hits[n-1].Sort)
	}
	return out, nil
}

func Count(ctx context.Context, c *opensearch.Client, p Params) (int64, error) {
	q, err := Query(p)
	if err != nil {
		return 0, err
	}
	return c.Count(ctx, p.Index, map[string]any{"query": q})
}

var (
	groupYmd = regexp.MustCompile(`^([\w\d\-_\.]+)-(\d{4}\.\d{2}\.\d{2}(-\d{2})*)`)
	groupDmy = regexp.MustCompile(`^([\w\d\-_\.]+)-\d{2}-\d{2}-\d{4}$`)
)

// Group - группа индексов по имени без даты: app-2026.10.13 -> "app-*".
type Group struct {
	Index string `json:"index"`
}

// Groups - группы сегодняшних индексов (name-YYYY.MM.DD и name-DD-MM-YYYY), без системных.
func Groups(ctx context.Context, c *opensearch.Client, now time.Time) ([]Group, error) {
	names, err := c.Indices(ctx, "*-"+now.Format("2006.01.02")+"*", "*-"+now.Format("02-01-2006"), "-.*")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := []Group{}
	for _, n := range names {
		m := groupYmd.FindStringSubmatch(n)
		if m == nil {
			m = groupDmy.FindStringSubmatch(n)
		}
		if m == nil || seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		out = append(out, Group{Index: m[1] + "-*"})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	return out, nil
}

// Mapping - поля сегодняшних индексов группы: плоская карта "поле -> тип". Маппинги
// нескольких индексов объединяются.
func Mapping(ctx context.Context, c *opensearch.Client, group string, now time.Time) (map[string]string, error) {
	if group == "" {
		return nil, fmt.Errorf("%w: index is required", ErrInvalid)
	}
	m, err := c.Mapping(ctx, group+"*"+now.Format("2006.01.02")+"*", group+"*"+now.Format("02-01-2006")+"*")
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, idx := range m {
		if props, ok := idx.Mappings["properties"].(map[string]any); ok {
			flatten("", props, out)
		}
	}
	return out, nil
}

func flatten(prefix string, props map[string]any, out map[string]string) {
	for key, value := range props {
		full := key
		if prefix != "" {
			full = prefix + "." + key
		}
		sub, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if t, ok := sub["type"].(string); ok {
			out[full] = t
		}
		if p, ok := sub["properties"].(map[string]any); ok {
			flatten(full, p, out)
		}
	}
}

/* ---------- Гистограмма ---------- */

// HistogramBuckets - примерно столько столбцов в гистограмме: интервал выбирается так,
// чтобы их было не больше.
const HistogramBuckets = 100

// Интервалы столбцов от секунды до недели: "круглые" значения, понятные человеку.
var histogramSteps = []time.Duration{
	time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 15 * time.Second, 30 * time.Second,
	time.Minute, 2 * time.Minute, 5 * time.Minute, 10 * time.Minute, 15 * time.Minute, 30 * time.Minute,
	time.Hour, 2 * time.Hour, 3 * time.Hour, 6 * time.Hour, 12 * time.Hour,
	24 * time.Hour, 48 * time.Hour, 7 * 24 * time.Hour,
}

// Interval - самый мелкий интервал, при котором на промежутке не больше HistogramBuckets столбцов.
func Interval(span time.Duration) time.Duration {
	for _, s := range histogramSteps {
		if span/s <= HistogramBuckets {
			return s
		}
	}
	return histogramSteps[len(histogramSteps)-1]
}

// Bucket - столбец гистограммы: начало (мс, UTC) и число найденных записей.
type Bucket struct {
	T int64 `json:"t"`
	N int64 `json:"n"`
}

// Histogram - число найденных записей по времени. Время в мс UTC.
type Histogram struct {
	From     int64    `json:"from"`
	To       int64    `json:"to"`
	Interval int64    `json:"interval"`
	Total    int64    `json:"total"`
	Buckets  []Bucket `json:"buckets"`
}

// HistogramBody - тело _search для гистограммы: тот же запрос, что у поиска, и агрегация
// date_histogram по полю времени с пустыми столбцами на всём промежутке.
func HistogramBody(p Params) (body map[string]any, h *Histogram, err error) {
	tf := p.Timefield()
	if tf == "" {
		return nil, nil, fmt.Errorf("%w: histogram needs a time field", ErrInvalid)
	}
	q, err := Query(p)
	if err != nil {
		return nil, nil, err
	}
	// Даты уже проверены в Query
	ds, _ := time.Parse(dateLayout, p.DateStart)
	de, _ := time.Parse(dateLayout, p.DateEnd)
	if !de.After(ds) {
		return nil, nil, fmt.Errorf("%w: date_end must be after date_start", ErrInvalid)
	}
	interval := Interval(de.Sub(ds))
	h = &Histogram{From: ds.UnixMilli(), To: de.UnixMilli(), Interval: interval.Milliseconds(), Buckets: []Bucket{}}
	body = map[string]any{
		"size":             0,
		"track_total_hits": true,
		"query":            q,
		"aggs": map[string]any{"histogram": map[string]any{"date_histogram": map[string]any{
			"field":           tf,
			"fixed_interval":  fmt.Sprintf("%ds", int64(interval/time.Second)),
			"min_doc_count":   0,
			"extended_bounds": map[string]any{"min": h.From, "max": h.To},
			"format":          "epoch_millis",
		}}},
	}
	return body, h, nil
}

// HistogramOf - гистограмма найденного по p на промежутке date_start..date_end.
func HistogramOf(ctx context.Context, c *opensearch.Client, p Params) (*Histogram, error) {
	body, h, err := HistogramBody(p)
	if err != nil {
		return nil, err
	}
	resp, err := c.Search(ctx, p.Index, body, "")
	if err != nil {
		return nil, err
	}
	var agg struct {
		Buckets []struct {
			Key   float64 `json:"key"`
			Count int64   `json:"doc_count"`
		} `json:"buckets"`
	}
	if raw, ok := resp.Aggregations["histogram"]; ok {
		if err := json.Unmarshal(raw, &agg); err != nil {
			return nil, fmt.Errorf("histogram: %w", err)
		}
	}
	for _, b := range agg.Buckets {
		h.Buckets = append(h.Buckets, Bucket{T: int64(b.Key), N: b.Count})
		h.Total += b.Count
	}
	return h, nil
}
