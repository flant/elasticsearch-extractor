package server

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/uzhinskiy/lib.go/helpers"
)

type ctxKey struct{}

// reqInfo - то, что обработчики добавляют к записи журнала о запросе.
type reqInfo struct {
	action string
}

func info(r *http.Request) *reqInfo {
	if i, ok := r.Context().Value(ctxKey{}).(*reqInfo); ok {
		return i
	}
	return &reqInfo{}
}

// user - кто прошёл аутентификацию на ingress (oauth2-proxy, dex).
func user(r *http.Request) string {
	for _, h := range []string{"X-Auth-Request-Email", "X-Forwarded-Email", "X-Auth-Request-User", "X-Forwarded-User"} {
		if v := r.Header.Get(h); v != "" {
			return v
		}
	}
	return ""
}

// who - пользователь для журнала событий, "-" - неизвестен.
func who(r *http.Request) string {
	if u := user(r); u != "" {
		return u
	}
	return "-"
}

type recorder struct {
	http.ResponseWriter
	code  int
	bytes int64
}

func (r *recorder) WriteHeader(code int) {
	r.code = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	if r.code == 0 {
		r.code = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += int64(n)
	return n, err
}

// accessLog пишет запись о каждом запросе; проверки и метрики - только в debug.
func (s *Server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ri := &reqInfo{}
		rec := &recorder{ResponseWriter: w}
		next.ServeHTTP(rec, r.WithContext(context.WithValue(r.Context(), ctxKey{}, ri)))
		if rec.code == 0 {
			rec.code = http.StatusOK
		}
		level := slog.LevelInfo
		switch {
		case strings.HasPrefix(r.URL.Path, "/assets/"):
			level = slog.LevelDebug
		case rec.code >= 500:
			level = slog.LevelError
		case rec.code >= 400:
			level = slog.LevelWarn
		}
		attrs := []any{
			"method", r.Method, "path", r.URL.Path, "status", rec.code, "bytes", rec.bytes,
			"duration", time.Since(start).Round(time.Millisecond).String(),
			"remote", helpers.GetIP(r.RemoteAddr, r.Header.Get("X-Real-IP"), r.Header.Get("X-Forwarded-For")),
		}
		if ri.action != "" {
			attrs = append(attrs, "action", ri.action)
		}
		if u := user(r); u != "" {
			attrs = append(attrs, "user", u)
		}
		s.opt.Log.Log(r.Context(), level, "http", attrs...)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}
