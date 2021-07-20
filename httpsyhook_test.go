package httpsyhook

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

type metrics struct {
	Struct
	BytesWritten int64
	StatusCode   int
}

func (m *metrics) HookWriteHeader(w http.ResponseWriter, statusCode int) {
	m.StatusCode = statusCode
	w.WriteHeader(statusCode)
}

func (m *metrics) HookWrite(w io.Writer, p []byte) (int, error) {
	m.BytesWritten += int64(len(p))
	return w.Write(p)
}

func metricsMiddleware(status, written *int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var m metrics
			next.ServeHTTP(Wrap(w, &m), r)
			fmt.Printf("%s %s %d %d\n", r.Method, r.URL, m.StatusCode, m.BytesWritten)
			*status = m.StatusCode
			*written = int(m.BytesWritten)
		})
	}
}

func TestMetricsTrace(t *testing.T) {
	endpoint := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
		b := bytes.NewBuffer(make([]byte, 4096))
		_, _ = io.Copy(w, b)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	var status, written int
	x := metricsMiddleware(&status, &written)(http.HandlerFunc(endpoint))
	x.ServeHTTP(w, r)
	if status != 201 || status != w.Code || written != w.Body.Len() {
		t.Fatal()
	}
}

type writeTracer struct {
	Struct
	w io.Writer
}

func (wt *writeTracer) HookWrite(w io.Writer, p []byte) (int, error) {
	_, _ = wt.w.Write(p)
	return w.Write(p)
}

type mockReadFromRecorder struct {
	*httptest.ResponseRecorder
}

func (w *mockReadFromRecorder) ReadFrom(r io.Reader) (int64, error) {
	return io.Copy(w.ResponseRecorder, r)
}

func TestReadFrom(t *testing.T) {
	var body []byte
	for i := 0; i < 10000; i++ {
		body = append(body, []byte("gopher")...)
	}

	sum := md5.Sum(body)
	expected := hex.EncodeToString(sum[:])
	var got string

	tracer := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := md5.New()
			next.ServeHTTP(Wrap(w, &writeTracer{w: h}), r)
			got = hex.EncodeToString(h.Sum(nil))
		})
	}

	endpoint := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(w, struct{ io.Reader }{bytes.NewReader(body)})
	})

	w := mockReadFromRecorder{httptest.NewRecorder()}
	r := httptest.NewRequest("GET", "/", nil)
	tracer(endpoint).ServeHTTP(&w, r)

	bodysum := md5.Sum(w.Body.Bytes())
	bodygot := hex.EncodeToString(bodysum[:])

	if expected != got || expected != bodygot || len(w.Body.Bytes()) != len(body) {
		t.Fatal()
	}
}

func TestUnwrap(t *testing.T) {
	w := httptest.NewRecorder()
	x := Wrap(w, &Struct{})
	if y := Unwrap(x); y != w {
		t.Fatal()
	}
}

func TestDoubleWrap(t *testing.T) {
	endpoint := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "Hello world")
	})

	middleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(Wrap(Wrap(w, &Struct{}), &Struct{}), r)
		})
	}

	h := middleware(endpoint)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	h.ServeHTTP(w, r)
	if w.Body.String() != "Hello world\n" {
		t.Fatal()
	}
}
