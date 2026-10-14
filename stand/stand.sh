#!/bin/sh
# Тестовый стенд: заглушка Elasticsearch, extractor из текущего кода и UI на dev-сервере Vite.
#
#   stand/stand.sh start | stop | status
#
# Заглушка (127.0.0.1:19200) и extractor (127.0.0.1:19400) слушают только локально,
# UI (0.0.0.0:$STAND_PORT, по умолчанию 5173) доступен по сети и проксирует /api и /data на extractor.
# Node.js не нужен: Vite и npm работают в контейнере node:24-alpine.
set -eu

ROOT=$(cd "$(dirname "$0")/.." && pwd)
RUN="$ROOT/stand/.run"
PORT=${STAND_PORT:-5173}
NODE_IMAGE=${NODE_IMAGE:-mirror.gcr.io/library/node:24-alpine}
CONTAINER=xtractor-stand

node() {
	docker run --rm --user "$(id -u):$(id -g)" -e HOME=/tmp -v "$ROOT/web":/web -w /web "$NODE_IMAGE" "$@"
}

stop() {
	if [ -f "$RUN/pids" ]; then
		kill $(cat "$RUN/pids") 2>/dev/null || true
		rm -f "$RUN/pids"
	fi
	# Если pid-файл потерян, процессы стенда находим по его портам
	for p in 19200 19400; do
		pid=$(ss -ltnpH "sport = :$p" 2>/dev/null | sed -n 's/.*pid=\([0-9]*\).*/\1/p' | head -n 1)
		[ -n "$pid" ] && kill "$pid" 2>/dev/null || true
	done
	docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
}

status() {
	ss -ltn | grep -E ":(19200|19400|$PORT) " || echo "стенд не запущен"
}

start() {
	stop
	mkdir -p "$RUN"

	# extractor собирается из текущего кода; UI отдаёт Vite, правки web/src видны сразу
	(cd "$ROOT" && go build -mod=mod -ldflags "-X main.version=$(git describe --tags --always --dirty 2>/dev/null || echo dev) -X main.vBuild=stand" -o "$RUN/extractor" ./cmd/)

	if [ ! -d "$ROOT/web/node_modules" ] || [ "$ROOT/web/package-lock.json" -nt "$ROOT/web/node_modules" ]; then
		node npm install --no-audit --no-fund
	fi

	: >"$RUN/mock_es.log"
	python3 "$ROOT/stand/mock_es.py" "$RUN/mock_es.log" 19200 >"$RUN/mock_es.out" 2>&1 &
	echo $! >"$RUN/pids"
	# extractor пишет выгрузки в /tmp/data - путь зашит в коде
	(cd "$RUN" && ./extractor -config "$ROOT/stand/config.yml" >"$RUN/extractor.log" 2>&1 & echo $! >>"$RUN/pids")

	docker run -d --name "$CONTAINER" --network host --user "$(id -u):$(id -g)" -e HOME=/tmp \
		-e BACKEND=http://127.0.0.1:19400 -v "$ROOT/web":/web -w /web "$NODE_IMAGE" \
		npx vite --host 0.0.0.0 --port "$PORT" --strictPort >/dev/null

	for _ in 1 2 3 4 5 6 7 8 9 10; do
		curl -fs -o /dev/null "http://127.0.0.1:$PORT/" && break
		sleep 1
	done
	if ! curl -fs -o /dev/null -X POST "http://127.0.0.1:$PORT/api/" -d '{"action":"get_clusters"}'; then
		echo "стенд не отвечает, логи: $RUN/extractor.log, $RUN/mock_es.out, docker logs $CONTAINER" >&2
		exit 1
	fi

	echo "Стенд запущен:"
	for ip in $(hostname -I); do
		case "$ip" in *:*|172.17.*) continue ;; esac
		echo "  http://$ip:$PORT/          снапшоты"
		echo "  http://$ip:$PORT/search/   поиск"
	done
	echo "Остановить: make stand-stop. Логи: $RUN/"
}

case "${1:-start}" in
start) start ;;
stop) stop && echo "стенд остановлен" ;;
status) status ;;
*) echo "usage: $0 start|stop|status" >&2; exit 2 ;;
esac
