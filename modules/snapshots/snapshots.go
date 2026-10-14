// Package snapshots - снапшоты репозиториев и восстановление из них индексов под именами
// extracted_<индекс>-DD-MM-YYYY.
package snapshots

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/flant/elasticsearch-extractor/modules/opensearch"
)

// Prefix - начало имени восстановленного индекса. Только такие индексы extractor
// показывает и разрешает удалять.
const Prefix = "extracted_"

// MaxBusyShards - сколько шардов extracted* может восстанавливаться, прежде чем новые
// восстановления откладываются.
const MaxBusyShards = 5

var (
	// ErrBusy - кластер ещё восстанавливает другие индексы.
	ErrBusy = errors.New("indices will not be restored now: the cluster is still recovering other indices, please wait")
	// ErrInvalid - запрос с неверными параметрами.
	ErrInvalid = errors.New("invalid request")
)

// Snapshot - снапшот с датой из имени. JSON-ключи прежние: их читает UI.
type Snapshot struct {
	Snapshot    string `json:"snapshot"`
	UUID        string `json:"uuid,omitempty"`
	State       string `json:"state,omitempty"`
	CreateEpoch int64  `json:"CreateEpoch"`
	CreateDate  string `json:"CreateDate"`
}

// RestoreResult - что восстанавливается и что пропущено из-за нехватки места на узлах.
type RestoreResult struct {
	Restoring []string `json:"restoring"`
	Skipped   []string `json:"skipped"`
}

type Options struct {
	Client        *opensearch.Client
	IncludeSystem bool // показывать снапшоты с именами на "."
	S3            bool // репозиторий в S3: место на узлах не проверяется
	Log           *slog.Logger
	Now           func() time.Time
}

type Service struct {
	opt   Options
	mu    sync.RWMutex
	cache map[string][]Snapshot
}

func New(o Options) *Service {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
	return &Service{opt: o, cache: map[string][]Snapshot{}}
}

var dated = regexp.MustCompile(`^(.*)-(\d{4}\.\d{2}\.\d{2})`)

// List - снапшоты репозитория с датой в имени (name-YYYY.MM.DD), отсортированные.
// Остальные пропускаются: по ним нельзя понять, за какой день логи.
func (s *Service) List(ctx context.Context, repo, otype, odir string) ([]Snapshot, error) {
	if repo == "" {
		return nil, fmt.Errorf("%w: repository is required", ErrInvalid)
	}
	raw, err := s.opt.Client.Snapshots(ctx, repo)
	if err != nil {
		return nil, err
	}
	items := make([]Snapshot, 0, len(raw))
	for _, n := range raw {
		if !s.opt.IncludeSystem && strings.HasPrefix(n.Snapshot, ".") {
			continue
		}
		m := dated.FindStringSubmatch(n.Snapshot)
		if m == nil {
			continue
		}
		d, err := time.Parse("2006.01.02", m[2])
		if err != nil {
			s.opt.Log.Warn("snapshot with a wrong date", "repository", repo, "snapshot", n.Snapshot, "error", err)
			continue
		}
		items = append(items, Snapshot{Snapshot: n.Snapshot, UUID: n.UUID, State: n.State, CreateEpoch: d.Unix(), CreateDate: m[2]})
	}
	s.mu.Lock()
	s.cache[repo] = append([]Snapshot(nil), items...)
	s.mu.Unlock()
	Sort(items, otype, odir)
	return items, nil
}

// Cached - последний полученный список репозитория, без обращения к кластеру; если его
// ещё нет - запрашивается.
func (s *Service) Cached(ctx context.Context, repo, otype, odir string) ([]Snapshot, error) {
	s.mu.RLock()
	items, ok := s.cache[repo]
	s.mu.RUnlock()
	if !ok {
		return s.List(ctx, repo, otype, odir)
	}
	items = append([]Snapshot(nil), items...)
	Sort(items, otype, odir)
	return items, nil
}

// Sort - по времени ("time") или имени ("name"), по возрастанию ("asc") или убыванию.
func Sort(items []Snapshot, otype, odir string) {
	var less func(i, j int) bool
	switch otype {
	case "time":
		less = func(i, j int) bool { return items[i].CreateEpoch < items[j].CreateEpoch }
	case "name":
		less = func(i, j int) bool { return items[i].Snapshot < items[j].Snapshot }
	default:
		return
	}
	if odir == "asc" {
		sort.SliceStable(items, less)
	} else {
		sort.SliceStable(items, func(i, j int) bool { return less(j, i) })
	}
}

// Status - состав снапшота: индексы, их шарды и размеры.
func (s *Service) Status(ctx context.Context, repo, snapshot string) (*opensearch.SnapshotStatus, error) {
	if repo == "" || snapshot == "" {
		return nil, fmt.Errorf("%w: repository and snapshot are required", ErrInvalid)
	}
	return s.opt.Client.SnapshotStatus(ctx, repo, snapshot)
}

// RestoredName - имя, под которым индекс будет восстановлен сегодня.
func RestoredName(index string, now time.Time) string {
	return Prefix + index + "-" + now.Format("02-01-2006")
}

// Restore восстанавливает индексы снапшота, для которых хватает места на узлах.
// Если места не хватает ни одному, кластер не вызывается вовсе.
func (s *Service) Restore(ctx context.Context, repo, snapshot string, indices []string) (RestoreResult, error) {
	res := RestoreResult{Restoring: []string{}, Skipped: []string{}}
	if repo == "" || snapshot == "" || len(indices) == 0 {
		return res, fmt.Errorf("%w: repository, snapshot and indices are required", ErrInvalid)
	}
	st, err := s.opt.Client.SnapshotStatus(ctx, repo, snapshot)
	if err != nil {
		return res, err
	}
	if len(st.Snapshots) == 0 {
		return res, fmt.Errorf("%w: snapshot %s not found", ErrInvalid, snapshot)
	}
	shards := map[string][]int64{}
	for _, name := range indices {
		ind, ok := st.Snapshots[0].Indices[name]
		if !ok {
			return res, fmt.Errorf("%w: index %s is not in snapshot %s", ErrInvalid, name, snapshot)
		}
		shards[name] = ind.ShardSizes()
	}

	h, err := s.opt.Client.Health(ctx, Prefix+"*")
	if err != nil {
		return res, err
	}
	if h.InitializingShards > MaxBusyShards || h.UnassignedShards > MaxBusyShards {
		return res, ErrBusy
	}

	var free []int64
	if !s.opt.S3 {
		nodes, err := s.opt.Client.Nodes(ctx)
		if err != nil {
			return res, err
		}
		for _, n := range nodes {
			free = append(free, n.Avail)
		}
	}
	res.Restoring, res.Skipped = Plan(shards, free, s.opt.S3)
	if len(res.Restoring) == 0 {
		return res, nil
	}

	err = s.opt.Client.Restore(ctx, repo, snapshot, opensearch.RestoreRequest{
		Indices:           res.Restoring,
		RenamePattern:     "(.+)",
		RenameReplacement: RestoredName("$1", s.opt.Now()),
		IndexSettings:     map[string]any{"index.number_of_replicas": 0},
	})
	return res, err
}

// Plan делит индексы на те, для которых хватает места на узлах, и остальные.
// Оценка грубая, как и прежде: сколько раз шарды индекса помещаются в свободное место
// узлов; индекс восстанавливается, если это число больше числа его шардов.
// Пустые шарды места не требуют. Для S3 место не проверяется.
func Plan(shards map[string][]int64, free []int64, s3 bool) (restore, skip []string) {
	restore, skip = []string{}, []string{}
	for name, sizes := range shards {
		if s3 {
			restore = append(restore, name)
			continue
		}
		fits, nonEmpty := int64(0), int64(0)
		for _, size := range sizes {
			if size == 0 {
				continue
			}
			nonEmpty++
			for _, f := range free {
				fits += f / size
			}
		}
		if nonEmpty == 0 || fits > nonEmpty {
			restore = append(restore, name)
		} else {
			skip = append(skip, name)
		}
	}
	sort.Strings(restore)
	sort.Strings(skip)
	return restore, skip
}

// Restored - восстановленные индексы extracted* с ходом восстановления.
func (s *Service) Restored(ctx context.Context) (map[string]opensearch.IndexRecovery, error) {
	return s.opt.Client.Recovery(ctx, Prefix+"*")
}

// Delete удаляет восстановленный индекс. Другие индексы, маски и списки запрещены:
// раньше имя уходило в DELETE как есть, и "*" удалял бы все индексы кластера.
func (s *Service) Delete(ctx context.Context, index string) error {
	if err := ValidRestored(index); err != nil {
		return err
	}
	return s.opt.Client.DeleteIndex(ctx, index)
}

func ValidRestored(index string) error {
	if !strings.HasPrefix(index, Prefix) || len(index) == len(Prefix) || strings.ContainsAny(index, "*?,/\\ #") {
		return fmt.Errorf("%w: only one restored index %s<name> can be deleted, got %q", ErrInvalid, Prefix, index)
	}
	return nil
}
