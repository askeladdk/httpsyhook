package httpsyhook

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func BenchmarkHandlerBaseline(b *testing.B) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", nil)
		h.ServeHTTP(w, r)
	}
	b.StopTimer()
}

func BenchmarkHandlerHooks(b *testing.B) {
	endpoint := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	middleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(Wrap(w, &Struct{}), r)
		})
	}

	h := middleware(endpoint)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", nil)
		h.ServeHTTP(w, r)
	}
	b.StopTimer()
}

func BenchmarkReaderFromBaseline(b *testing.B) {
	bs := make([]byte, 32*1024*1024)

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buffer := bytes.NewReader(bs)
		_, _ = io.Copy(w, buffer)
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := mockReadFromRecorder{httptest.NewRecorder()}
		r := httptest.NewRequest("GET", "/", nil)
		h.ServeHTTP(&w, r)
	}
	b.StopTimer()
}

func BenchmarkReaderFromHooks(b *testing.B) {
	bs := make([]byte, 32*1024*1024)

	endpoint := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buffer := bytes.NewReader(bs)
		_, _ = io.Copy(w, buffer)
	})

	middleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(Wrap(w, &Struct{}), r)
		})
	}

	h := middleware(endpoint)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := mockReadFromRecorder{httptest.NewRecorder()}
		r := httptest.NewRequest("GET", "/", nil)
		h.ServeHTTP(&w, r)
	}
	b.StopTimer()
}
