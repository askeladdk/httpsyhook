package httpsyhook_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"

	"github.com/askeladdk/httpsyhook"
)

type metrics struct {
	httpsyhook.Struct
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

// Create a logger middleware to log all requests and their metrics.
func Example_logging() {
	logger := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var m metrics
			next.ServeHTTP(httpsyhook.Wrap(w, &m), r)
			fmt.Println(r.Method, r.URL, m.StatusCode, m.BytesWritten)
		})
	}

	endpoint := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Hello, world!")
	})

	mux := http.NewServeMux()
	mux.Handle("/", logger(endpoint))

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	mux.ServeHTTP(w, r)
	// Output: GET / 200 13
}
