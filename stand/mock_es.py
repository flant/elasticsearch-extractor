# Заглушка Elasticsearch для тестового стенда: отвечает на те запросы, которые делает
# extractor, демонстрационными данными. Даты считаются от сегодняшнего дня, состояние
# (восстановленные и удалённые индексы) живёт в памяти и сбрасывается при перезапуске.
#
#   python3 mock_es.py [LOG] [PORT]
import json
import re
import sys
import time
from datetime import datetime, timedelta, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

LOG = open(sys.argv[1], "a") if len(sys.argv) > 1 else sys.stderr
PORT = int(sys.argv[2]) if len(sys.argv) > 2 else 19200

GB = 2**30
TODAY = datetime.now(timezone.utc).date()
NOW_MS = int(time.time() * 1000)


def ymd(d):
    return d.strftime("%Y.%m.%d")


def dmy(d):
    return d.strftime("%d-%m-%Y")


YESTERDAY = TODAY - timedelta(days=1)
DAY_BEFORE = TODAY - timedelta(days=2)

# Восстановленные индексы: имя extracted_<индекс>-DD-MM-YYYY, как их называет extractor
RESTORED = {
    f"extracted_app-nginx-{ymd(DAY_BEFORE)}-{dmy(TODAY)}": {
        "src": f"app-nginx-{ymd(DAY_BEFORE)}", "snap": f"logs-{ymd(YESTERDAY)}",
        "total": int(6.8 * GB), "rec": int(3.1 * GB), "stage": "INDEX", "t": NOW_MS - 600_000},
    f"extracted_app-backend-{ymd(DAY_BEFORE)}-{dmy(TODAY)}": {
        "src": f"app-backend-{ymd(DAY_BEFORE)}", "snap": f"logs-{ymd(YESTERDAY)}",
        "total": int(7.4 * GB), "rec": int(7.4 * GB), "stage": "DONE", "t": NOW_MS - 3_600_000},
    f"extracted_ingress-{ymd(TODAY - timedelta(days=8))}-{dmy(YESTERDAY)}": {
        "src": f"ingress-{ymd(TODAY - timedelta(days=8))}", "snap": f"logs-{ymd(TODAY - timedelta(days=7))}",
        "total": int(2.2 * GB), "rec": int(2.2 * GB), "stage": "DONE", "t": NOW_MS - 90_000_000},
}

# Индексы в каждом снапшоте: имя без даты -> (шарды, размер в ГБ). huge-archive не влезет на узлы.
SNAP_INDICES = {
    "app-nginx": (2, 6.8), "app-backend": (3, 7.4), "app-frontend": (1, 1.2), "ingress": (3, 12.5),
    "kube-events": (1, 0.6), "postgres": (2, 4.3), "huge-archive": (1, 5000),
}

NODES = [
    ("10.4.3.21", "es-data-0", 1220, 780, "61.0"),
    ("10.4.3.22", "es-data-1", 1560, 440, "78.0"),
    ("10.4.3.23", "es-data-2", 1840, 160, "92.0"),
]


def snapshots_of(repo):
    if repo == "s3-logs":
        # сегодняшний ещё создаётся, позавчерашний частичный, ещё через день - с ошибкой
        states = {0: "IN_PROGRESS", 3: "PARTIAL", 5: "FAILED"}
        out = [{"snapshot": f"logs-{ymd(TODAY - timedelta(days=i))}", "uuid": f"l{i}", "state": states.get(i, "SUCCESS")}
               for i in range(13)]
        return out + [{"snapshot": "manual-before-upgrade", "uuid": "m", "state": "SUCCESS"}]
    if repo == "s3-audit":
        return [{"snapshot": f"audit-{ymd(TODAY - timedelta(days=i))}", "uuid": f"a{i}", "state": "SUCCESS"} for i in range(5)]
    return []


# Документы для поиска: 2000 записей за последние сутки с разными уровнями, подами и сообщениями.
MESSAGES = [
    ("ERROR", "request to upstream payments-api failed: context deadline exceeded (timeout {n}s)", 504),
    ("WARN", "retrying upstream call to inventory-api, attempt {n}/3", 200),
    ("INFO", "healthcheck ok in {n} ms", 200),
    ("ERROR", "dial tcp 10.12.4.18:8080: connection refused", 502),
    ("INFO", "POST /api/v1/orders 201 Created in {n}0 ms", 201),
    ("WARN", "slow query to postgres: {n}00 ms", 200),
    ("INFO", "user {n} logged in", 200),
    ("ERROR", "panic: runtime error: index out of range [{n}]", 500),
]
PODS = ["backend-7f9c6d-x2k4p", "backend-7f9c6d-q8m1z", "backend-7f9c6d-lp3wd"]
NAMESPACES = ["production", "production", "staging"]


def make_docs():
    start = datetime.now(timezone.utc).replace(microsecond=0)
    docs = []
    for i in range(2000):
        level, text, status = MESSAGES[(i * 7) % len(MESSAGES)]
        ts = start - timedelta(seconds=43 * i)
        docs.append({
            "_index": f"app-backend-{ymd(ts.date())}", "_id": f"id{i}",
            # значение сортировки по @timestamp, как его отдаёт ES: миллисекунды
            "sort": [int(ts.timestamp() * 1000) + i % 1000],
            "_source": {
                "@timestamp": ts.strftime("%Y-%m-%dT%H:%M:%S") + f".{i % 1000:03d}Z",
                "level": level,
                "kubernetes": {"namespace": NAMESPACES[i % 3], "pod_name": PODS[(i // 3) % 3]},
                "message": text.format(n=i % 9 + 1),
                "http": {"status": status},
            },
        })
    return docs


DOCS = make_docs()


def get_path(src, field):
    cur = src
    for part in field.split("."):
        cur = cur.get(part) if isinstance(cur, dict) else None
    return cur


def strings(v):
    if isinstance(v, dict):
        for x in v.values():
            yield from strings(x)
    elif isinstance(v, list):
        for x in v:
            yield from strings(x)
    elif v is not None:
        yield str(v)


def match_text(src, q):
    """simple_query_string приблизительно: слова и "фразы" через ИЛИ (как default_operator OR),
    -слово исключает документ. Сравнение без учёта регистра по всем полям."""
    text = " ".join(strings(src)).lower()
    positive, negative = [], []
    for m in re.finditer(r'(-?)"([^"]+)"|(\S+)', q):
        neg, phrase, word = m.group(1), m.group(2), m.group(3)
        term = (phrase or word or "").lower()
        if word:
            if word in ("AND", "OR", "NOT", "|", "+"):
                continue
            neg = "-" if word.startswith("-") else ""
            term = word.lstrip("-+").lower().rstrip("*")
        if not term:
            continue
        (negative if neg else positive).append(term)
    if any(t in text for t in negative):
        return False
    return not positive or any(t in text for t in positive)


def match_clause(src, c):
    if "match_all" in c:
        return True
    if "simple_query_string" in c:
        return match_text(src, c["simple_query_string"].get("query", ""))
    if "match_phrase" in c:
        field, value = next(iter(c["match_phrase"].items()))
        v = get_path(src, field)
        return v is not None and str(value).lower() in " ".join(strings(v)).lower()
    if "exists" in c:
        return get_path(src, c["exists"]["field"]) is not None
    if "range" in c:
        field, r = next(iter(c["range"].items()))
        v = get_path(src, field)
        if v is None:
            return False
        if "gte" in r and v < r["gte"].replace(".000Z", ""):
            return False
        if "lte" in r and v[:19] > r["lte"][:19]:
            return False
        return True
    return True


def run_query(q):
    """Документы, подходящие под bool-запрос extractor, новые первыми."""
    b = (q or {}).get("bool", {})
    out = []
    for d in DOCS:
        src = d["_source"]
        if all(match_clause(src, c) for c in b.get("must", []) + b.get("filter", [])) and \
                not any(match_clause(src, c) for c in b.get("must_not", [])):
            out.append(d)
    return out


def only_fields(h, fields):
    out = {}
    for f in fields:
        cur = h["_source"]
        for part in f.split("."):
            cur = cur.get(part) if isinstance(cur, dict) else None
        if cur is not None:
            out[f] = [cur]
    return {"_index": h["_index"], "_id": h["_id"], "fields": out, "sort": h["sort"]}


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *args):
        pass

    def send(self, obj, code=200):
        b = json.dumps(obj).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(b)))
        self.end_headers()
        self.wfile.write(b)

    def body(self):
        n = int(self.headers.get("Content-Length") or 0)
        return self.rfile.read(n).decode() if n else ""

    def handle_any(self):
        b, p = self.body(), self.path
        LOG.write(f"{self.command} {p} {b[:300]}\n")
        LOG.flush()

        if p.startswith("/_cat/repositories"):
            return self.send([{"id": "s3-logs", "type": "s3"}, {"id": "s3-audit", "type": "s3"}, {"id": "local-fs", "type": "fs"}])
        if p.startswith("/_cat/nodes"):
            return self.send([{"ip": ip, "name": n, "dt": str((u + a) * GB), "du": str(u * GB), "dup": pct, "d": str(a * GB)}
                              for ip, n, u, a, pct in NODES])
        if p.startswith("/_snapshot/") and "/*" in p:
            return self.send({"snapshots": snapshots_of(p.split("/")[2])})
        if "/_restore" in p:
            # восстановление: индексы появляются в списке extracted* и восстанавливаются по 0,4 ГБ за опрос
            req = json.loads(b or "{}")
            snap = p.split("/")[3]
            for idx in req.get("indices") or []:
                name = req.get("rename_replacement", "extracted_$1").replace("$1", idx)
                shards, gb = SNAP_INDICES.get(idx.rsplit("-", 1)[0], (1, 1.0))
                RESTORED[name] = {"src": idx, "snap": snap, "total": int(gb * GB), "rec": 0,
                                  "stage": "INDEX", "t": int(time.time() * 1000)}
            return self.send({"accepted": True})
        if p.endswith("/_status"):
            # индексы снапшота - за предыдущий день
            snap = p.split("/")[3]
            try:
                day = datetime.strptime(snap[-10:], "%Y.%m.%d").date() - timedelta(days=1)
            except ValueError:
                day = YESTERDAY
            idx ={f"{n}-{ymd(day)}": {"shards_stats": {"total": s}, "stats": {"total": {"size_in_bytes": int(g * GB)}},
                                       "shards": {str(k): {"stats": {"total": {"size_in_bytes": int(g * GB / s)}}} for k in range(s)}}
                   for n, (s, g) in SNAP_INDICES.items()}
            return self.send({"snapshots": [{"snapshot": snap, "state": "SUCCESS", "indices": idx}]})
        if p.startswith("/_cluster/health"):
            return self.send({"cluster_name": "stand", "status": "green", "initializing_shards": 0, "unassigned_shards": 0})
        if p.startswith("/extracted") and p.endswith("/_recovery"):
            out = {}
            for name, r in RESTORED.items():
                # восстановление идёт: каждый опрос прибавляет 0,4 ГБ
                if r["stage"] != "DONE":
                    r["rec"] = min(r["total"], r["rec"] + int(0.4 * GB))
                    if r["rec"] >= r["total"]:
                        r["stage"] = "DONE"
                out[name] = {"shards": [{
                    "type": "SNAPSHOT", "stage": r["stage"], "start_time_in_millis": r["t"],
                    "source": {"repository": "s3-logs", "snapshot": r["snap"], "index": r["src"]},
                    "index": {"size": {"total_in_bytes": r["total"], "recovered_in_bytes": r["rec"], "percent": "0%"}}}]}
            return self.send(out)
        if self.command == "DELETE" and p.startswith("/extracted_"):
            RESTORED.pop(p[1:], None)
            return self.send({"acknowledged": True})
        if p.startswith("/_cat/indices"):
            return self.send([{"index": f"{n}-{ymd(TODAY)}"} for n in ("app-backend", "app-nginx", "ingress")])
        if "_mapping" in p:
            return self.send({f"app-backend-{ymd(TODAY)}": {"mappings": {"properties": {
                "@timestamp": {"type": "date"}, "level": {"type": "keyword"}, "message": {"type": "text"},
                "kubernetes": {"properties": {"namespace": {"type": "keyword"}, "pod_name": {"type": "keyword"}}},
                "http": {"properties": {"status": {"type": "long"}}}}}}})
        if p.endswith("/_count"):
            return self.send({"count": len(run_query(json.loads(b or "{}").get("query")))})
        if "_search?scroll" in p:
            # выгрузка: страницы по size записей, пока найденное не кончится
            q = json.loads(b or "{}")
            self.server.found = run_query(q.get("query"))
            self.server.size = q.get("size", 2)
            self.server.pos = 0
            self.server.fields = q.get("fields", []) if q.get("_source") is False else None
            return self.send(self.page())
        if p == "/_search/scroll":
            if self.command == "DELETE":
                return self.send({"succeeded": True})
            return self.send(self.page())
        if p.startswith("/_search/scroll/"):
            return self.send({"succeeded": True})
        if p.endswith("/_search"):
            q = json.loads(b or "{}")
            found = run_query(q.get("query"))
            if q.get("search_after"):
                # следующая страница: документы после последнего показанного
                after = q["search_after"][0]
                found = [d for d in found if d["sort"][0] < after]
            hits = found[: min(q.get("size", 500), 500)]
            if q.get("_source") is False:
                hits = [only_fields(h, q.get("fields", [])) for h in hits]
            total = min(len(found), 10000)
            return self.send({"took": 12, "hits": {"total": {"value": total, "relation": "eq" if len(found) <= 10000 else "gte"}, "hits": hits}})
        return self.send({"error": {"reason": "mock: unknown " + p}}, 404)

    def page(self):
        srv = self.server
        hits = srv.found[srv.pos: srv.pos + srv.size]
        srv.pos += len(hits)
        if srv.fields is not None:
            hits = [only_fields(h, srv.fields) for h in hits]
        return {"_scroll_id": "SID", "hits": {"total": {"value": len(srv.found)}, "hits": hits}}

    do_GET = do_POST = do_DELETE = do_HEAD = handle_any


srv = ThreadingHTTPServer(("127.0.0.1", PORT), Handler)
srv.found, srv.size, srv.pos, srv.fields = [], 2, 0, None
print(f"mock elasticsearch on 127.0.0.1:{PORT}", flush=True)
srv.serve_forever()
