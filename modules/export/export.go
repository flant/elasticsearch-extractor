// Package export выгружает найденное в файлы CSV или JSON (по документу в строке) через
// scroll API, хранит их в каталоге выгрузок и удаляет старые.
package export

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/flant/elasticsearch-extractor/modules/opensearch"
	"github.com/flant/elasticsearch-extractor/modules/search"
)

const scrollKeep = "10m"

var (
	// ErrInvalid - неверные параметры выгрузки или имя файла.
	ErrInvalid = errors.New("invalid export request")
	// Имя файла приходит от клиента и подставляется в путь: только безопасные символы.
	nameRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	fileRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}\.(csv|json)$`)
)

type Options struct {
	Dir     string        // каталог файлов
	Batch   int64         // документов за один запрос scroll
	MaxRows int           // лимит строк
	MaxSize int64         // лимит размера файла, байт
	TTL     time.Duration // сколько хранить файлы
	Log     *slog.Logger
}

type Exporter struct {
	opt Options
}

// Result - готовый файл. Truncated - выгрузку остановил лимит строк или размера.
type Result struct {
	File      string `json:"file"`
	Format    string `json:"format"`
	Rows      int    `json:"rows"`
	Size      int64  `json:"size"`
	Truncated bool   `json:"truncated"`
}

func New(o Options) (*Exporter, error) {
	if o.Log == nil {
		o.Log = slog.Default()
	}
	if err := os.MkdirAll(o.Dir, 0o755); err != nil {
		return nil, fmt.Errorf("export dir: %w", err)
	}
	return &Exporter{opt: o}, nil
}

func (e *Exporter) Limits() (rows int, size int64) { return e.opt.MaxRows, e.opt.MaxSize }

// Path - путь к готовому файлу по его имени из URL, "" - имя недопустимое.
func (e *Exporter) Path(file string) string {
	if !fileRe.MatchString(file) {
		return ""
	}
	return filepath.Join(e.opt.Dir, file)
}

// Export выгружает найденное по p в файл <fname>.<format>.
func (e *Exporter) Export(ctx context.Context, c *opensearch.Client, p search.Params, format, fname string) (Result, error) {
	res := Result{Format: format}
	if format != "csv" && format != "json" {
		return res, fmt.Errorf("%w: format must be csv or json", ErrInvalid)
	}
	if !nameRe.MatchString(fname) {
		return res, fmt.Errorf("%w: wrong file name %q", ErrInvalid, fname)
	}
	p.SearchAfter = ""
	body, err := search.Body(p, e.opt.Batch)
	if err != nil {
		return res, err
	}
	fields := p.Fields
	if len(fields) == 0 {
		fields = p.Mapping
	}
	tf := p.Timefield()
	useSource := len(p.Fields) == 0

	res.File = fname + "." + format
	path := filepath.Join(e.opt.Dir, res.File)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return res, err
	}
	defer f.Close()

	var size int64
	write := func(s string) error {
		n, err := f.WriteString(s)
		size += int64(n)
		return err
	}
	if format == "csv" {
		header := fields
		if tf != "" {
			header = append([]string{tf}, fields...)
		}
		if err := write(strings.Join(header, ";") + "\n"); err != nil {
			return res, err
		}
	}

	scrollID := ""
	// Освобождаем scroll-контекст при любом выходе, не дожидаясь его таймаута
	defer func() {
		if scrollID != "" {
			if err := c.ClearScroll(context.WithoutCancel(ctx), scrollID); err != nil {
				e.opt.Log.Warn("cannot clear scroll", "error", err)
			}
		}
	}()

	for {
		var resp *opensearch.SearchResponse
		if scrollID == "" {
			resp, err = c.Search(ctx, p.Index, body, scrollKeep)
		} else {
			resp, err = c.Scroll(ctx, scrollID, scrollKeep)
		}
		if err != nil {
			return res, err
		}
		if resp.ScrollID != "" {
			scrollID = resp.ScrollID
		}
		if len(resp.Hits.Hits) == 0 {
			break
		}
		for _, h := range resp.Hits.Hits {
			var line string
			if format == "csv" {
				line = csvRow(h, useSource, tf, fields)
			} else {
				line, err = jsonRow(h, useSource, tf, fields)
				if err != nil {
					return res, err
				}
			}
			if err := write(line); err != nil {
				return res, err
			}
			res.Rows++
		}
		// Лимиты проверяются после страницы: файл обрезается, но остаётся доступным
		if res.Rows >= e.opt.MaxRows || size > e.opt.MaxSize {
			res.Truncated = true
			break
		}
	}
	res.Size = size
	return res, nil
}

func value(h opensearch.Hit, useSource bool, field string) any {
	if useSource {
		if v, ok := h.Source[field]; ok {
			return v
		}
		// вложенные поля в _source: kubernetes.pod_name -> {"kubernetes":{"pod_name":...}}
		var cur any = h.Source
		for _, part := range strings.Split(field, ".") {
			m, ok := cur.(map[string]any)
			if !ok {
				return nil
			}
			cur = m[part]
		}
		return cur
	}
	return h.Fields[field]
}

// csvRow - строка CSV через ";": строки в кавычках без переводов строк, массивы через
// ", ", пустые поля - "--". Формат прежний.
func csvRow(h opensearch.Hit, useSource bool, tf string, fields []string) string {
	var sb strings.Builder
	if tf != "" {
		sb.WriteString(fmt.Sprintf("%v;", value(h, useSource, tf)))
	}
	for _, f := range fields {
		data := value(h, useSource, f)
		if data == nil {
			sb.WriteString("--;")
			continue
		}
		switch reflect.TypeOf(data).Kind() {
		case reflect.Slice:
			s := reflect.ValueOf(data)
			parts := make([]string, s.Len())
			for i := range parts {
				parts[i] = fmt.Sprintf("%v", s.Index(i))
			}
			sb.WriteString(fmt.Sprintf(`"%s";`, quote(strings.Join(parts, ", "))))
		case reflect.String:
			sb.WriteString(fmt.Sprintf(`"%s";`, quote(data.(string))))
		default:
			sb.WriteString(fmt.Sprintf("%v;", data))
		}
	}
	sb.WriteString("\n")
	return sb.String()
}

func quote(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\n", ""), `"`, `""`)
}

func jsonRow(h opensearch.Hit, useSource bool, tf string, fields []string) (string, error) {
	row := map[string]any{}
	if tf != "" {
		row[tf] = value(h, useSource, tf)
	}
	for _, f := range fields {
		v := value(h, useSource, f)
		if v == nil {
			v = "--"
		}
		row[f] = v
	}
	b, err := json.Marshal(row)
	if err != nil {
		return "", err
	}
	return string(b) + "\n", nil
}

// Cleanup раз в 10 минут удаляет файлы старше TTL, пока ctx жив. Ошибки только
// записываются в журнал: очистка не должна останавливаться и ронять сервис.
func (e *Exporter) Cleanup(ctx context.Context) {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for {
		e.clean(time.Now())
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (e *Exporter) clean(now time.Time) {
	entries, err := os.ReadDir(e.opt.Dir)
	if err != nil {
		e.opt.Log.Error("cannot read export dir", "dir", e.opt.Dir, "error", err)
		return
	}
	for _, de := range entries {
		info, err := de.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if age := now.Sub(info.ModTime()); age > e.opt.TTL {
			if err := os.Remove(filepath.Join(e.opt.Dir, de.Name())); err != nil {
				e.opt.Log.Warn("cannot delete an old export", "file", de.Name(), "error", err)
				continue
			}
			e.opt.Log.Info("old export deleted", "file", de.Name(), "age", age.Round(time.Second).String())
		}
	}
}
