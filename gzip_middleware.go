package main

import (
	"compress/gzip"
	"net/http"
	"strings"
)

const xlsxContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

type gzipResponseWriter struct {
	http.ResponseWriter
	gz          *gzip.Writer
	wroteHeader bool
	useGzip     bool
}

func isXLSXRequest(path string) bool {
	if strings.HasSuffix(path, ".xlsx") || strings.HasSuffix(path, "/export/xlsx") {
		return true
	}
	switch path {
	case "/timesheets/export",
		"/groups/stats",
		"/projects/stats",
		"/employees/stats",
		"/employees/stats/individual":
		return true
	}
	return false
}

func (w *gzipResponseWriter) WriteHeader(statusCode int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true

	ct := w.Header().Get("Content-Type")
	if strings.Contains(ct, xlsxContentType) {
		w.ResponseWriter.WriteHeader(statusCode)
		return
	}

	gz, err := gzip.NewWriterLevel(w.ResponseWriter, gzip.BestSpeed)
	if err != nil {
		w.ResponseWriter.WriteHeader(statusCode)
		return
	}

	w.gz = gz
	w.useGzip = true
	w.Header().Set("Content-Encoding", "gzip")
	w.Header().Del("Content-Length")
	w.Header().Set("Vary", "Accept-Encoding")
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", http.DetectContentType(b))
		}
		w.WriteHeader(http.StatusOK)
	}
	if w.useGzip {
		return w.gz.Write(b)
	}
	return w.ResponseWriter.Write(b)
}

func (w *gzipResponseWriter) Close() {
	if w.gz != nil {
		_ = w.gz.Close()
	}
}

func gzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") || isXLSXRequest(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		gzw := &gzipResponseWriter{ResponseWriter: w}
		defer gzw.Close()

		next.ServeHTTP(gzw, r)
	})
}
