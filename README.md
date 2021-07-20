# HTT-Peasy Hook

[![GoDoc](https://godoc.org/github.com/askeladdk/httpsyhook?status.png)](https://godoc.org/github.com/askeladdk/httpsyhook)
[![Go Report Card](https://goreportcard.com/badge/github.com/askeladdk/httpsyhook)](https://goreportcard.com/report/github.com/askeladdk/httpsyhook)

## Overview

Package httpsyhook provides an interface to hook into http.ResponseWriter.
It can be used to capture any HTTP response metrics, on-the-fly compression, hashing, and more.

## Install

```
go get -u github.com/askeladdk/httpsyhook
```

## Quickstart

To hook into `http.ResponseWriter`, define a type that implements `httpsyhook.Interface`. Embed `httpsyhook.Struct` to use passthrough hooks by default and only implements the hooks that you are interested in.

```go
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
```

To attach the hooks, create a middleware that calls the `Wrap` function. `Wrap` returns a new `http.ResponseWriter` that calls the attached `Interface` whenever one of its methods are invoked. It is safe to wrap multiple times.

```go
func logMetrics(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        var m metrics
        next.ServeHTTP(httpsyhook.Wrap(w, &m), r)
        log.Println(r.Method, r.URL, m.StatusCode, m.BytesWritten)
    })
}
```

Finally, apply the middleware to a `http.Handler` to use it.

```go
mux := http.NewServeMux()
mux.Handle(logMetrics(handler))
```

Read the rest of the [documentation on pkg.go.dev](https://pkg.go.dev/github.com/askeladdk/httpsyhook). It's easy-peasy!

## Performance

Unscientific benchmarks on my laptop suggest an overhead of ~400ns.

```
% go test -bench=Handler -benchtime=1000000x
goos: darwin
goarch: amd64
pkg: github.com/askeladdk/httpsyhook
cpu: Intel(R) Core(TM) i5-5287U CPU @ 2.90GHz
BenchmarkHandlerBaseline-4   	 1000000	      1956 ns/op
BenchmarkHandlerHooks-4      	 1000000	      2356 ns/op
```

## License

Package httpsyhook is released under the terms of the ISC license.
