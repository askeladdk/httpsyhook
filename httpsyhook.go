// Package httpsyhook provides an interface to hook into http.ResponseWriter.
// It can be used to capture any HTTP response metrics, on-the-fly compression, hashing, and more.
package httpsyhook

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
)

// Interface exposes hooks into the ResponseWriter.
type Interface interface {
	// HookHeader is called whenever ResponseWriter.Header is called.
	HookHeader(http.ResponseWriter) http.Header

	// HookWrite is called whenever the ResponseWriter is written to,
	// unless the source Reader is an *os.File and the underlying
	// TCP connection implements the io.ReaderFrom fast path.
	//
	// Wrap the *os.File to bypass the fast path:
	//  io.Copy(w, struct{ io.Reader }{f})
	HookWrite(w io.Writer, p []byte) (int, error)

	// HookWriteHeader is called when ResponseWriter.WriteHeader is first called.
	HookWriteHeader(w http.ResponseWriter, statusCode int)

	// HookFlush is called whenever the http.Flusher interface is invoked.
	HookFlush(flusher http.Flusher)

	// HookHijack is called whenever the http.Hijacker interface is invoked.
	HookHijack(hijacker http.Hijacker) (net.Conn, *bufio.ReadWriter, error)

	// HookPush is called whenever the http.Pusher interface is invoked.
	HookPush(pusher http.Pusher, target string, opts *http.PushOptions) error
}

type responseWriter struct {
	http.ResponseWriter
	iface       Interface
	wroteHeader int32
}

func (w *responseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *responseWriter) Header() http.Header {
	return w.iface.HookHeader(w.ResponseWriter)
}

func (w *responseWriter) WriteHeader(statusCode int) {
	if atomic.CompareAndSwapInt32(&w.wroteHeader, 0, 1) {
		w.iface.HookWriteHeader(w.ResponseWriter, statusCode)
	}
}

func (w *responseWriter) Write(p []byte) (int, error) {
	w.WriteHeader(http.StatusOK)
	return w.iface.HookWrite(w.ResponseWriter, p)
}

func (w *responseWriter) Flush() {
	w.WriteHeader(http.StatusOK)
	w.iface.HookFlush(w.ResponseWriter.(http.Flusher))
}

func (w *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.iface.HookHijack(w.ResponseWriter.(http.Hijacker))
}

func (w *responseWriter) Push(target string, opts *http.PushOptions) error {
	return w.iface.HookPush(w.ResponseWriter.(http.Pusher), target, opts)
}

func (w *responseWriter) CloseNotify() <-chan bool {
	return w.ResponseWriter.(http.CloseNotifier).CloseNotify() //nolint
}

func srcIsRegularFile(src io.Reader) (isRegular bool, err error) {
	// copied from the go source code:
	// https://golang.org/src/net/http/server.go?s=3003:5866#L564
	switch v := src.(type) {
	case *os.File:
		fi, err := v.Stat()
		if err != nil {
			return false, err
		}
		return fi.Mode().IsRegular(), nil
	case *io.LimitedReader:
		return srcIsRegularFile(v.R)
	default:
		return
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

var byteSlicePool = &sync.Pool{New: func() interface{} { return new([]byte) }}

func (w *responseWriter) ReadFrom(r io.Reader) (int64, error) {
	regular, err := srcIsRegularFile(r)
	if err != nil {
		return 0, err
	}

	w.WriteHeader(http.StatusOK)

	// fast path for regular files
	if regular {
		if readerFrom, ok := w.ResponseWriter.(io.ReaderFrom); ok {
			return readerFrom.ReadFrom(r)
		}
	}

	wf := writerFunc(func(p []byte) (int, error) {
		return w.iface.HookWrite(w.ResponseWriter, p)
	})

	if writerTo, ok := r.(io.WriterTo); ok {
		return writerTo.WriteTo(wf)
	}
	buf := byteSlicePool.Get().(*[]byte)
	defer byteSlicePool.Put(buf)
	return io.CopyBuffer(wf, r, *buf)
}

// Wrap returns a new http.ResponseWriter that calls the attached
// Interface whenever one of its methods are invoked.
// Any calls to the ResponseWriter or its optional interfaces
// CloseNotifier, Flusher, Hijacker, Pusher, and ReaderFrom
// will be intercepted.
//
// CloseNotifier is not exposed because it is deprecated.
// ReaderFrom is not exposed because it transparently calls the Write method
// in order to provide a single interface for intercepting the data stream.
func Wrap(w http.ResponseWriter, iface Interface) http.ResponseWriter {
	const (
		ifaceCloseNotifier = 1 << iota
		ifaceFlusher
		ifaceHijacker
		ifacePusher
		ifaceReaderFrom
	)

	var ifaces int

	rw := &responseWriter{w, iface, 0}

	if _, ok := w.(http.CloseNotifier); ok { //nolint
		ifaces |= ifaceCloseNotifier // 00001
	}
	if _, ok := w.(http.Flusher); ok {
		ifaces |= ifaceFlusher // 00010
	}
	if _, ok := w.(http.Hijacker); ok {
		ifaces |= ifaceHijacker // 00100
	}
	if _, ok := w.(http.Pusher); ok {
		ifaces |= ifacePusher // 01000
	}
	if _, ok := w.(io.ReaderFrom); ok {
		ifaces |= ifaceReaderFrom // 10000
	}

	switch ifaces {
	default:
		return w
	case ifaceCloseNotifier: // 00001
		return struct {
			unwrapper
			http.ResponseWriter
			http.CloseNotifier
		}{rw, rw, rw}
	case ifaceFlusher: // 00010
		return struct {
			unwrapper
			http.ResponseWriter
			http.Flusher
		}{rw, rw, rw}
	case ifaceCloseNotifier + ifaceFlusher: // 00011
		return struct {
			unwrapper
			http.ResponseWriter
			http.CloseNotifier
			http.Flusher
		}{rw, rw, rw, rw}
	case ifaceHijacker: // 00100
		return struct {
			unwrapper
			http.ResponseWriter
			http.Hijacker
		}{rw, rw, rw}
	case ifaceCloseNotifier + ifaceHijacker: // 00101
		return struct {
			unwrapper
			http.ResponseWriter
			http.CloseNotifier
			http.Hijacker
		}{rw, rw, rw, rw}
	case ifaceFlusher + ifaceHijacker: // 00110
		return struct {
			unwrapper
			http.ResponseWriter
			http.Flusher
			http.Hijacker
		}{rw, rw, rw, rw}
	case ifaceCloseNotifier + ifaceFlusher + ifaceHijacker: // 00111
		return struct {
			unwrapper
			http.ResponseWriter
			http.CloseNotifier
			http.Flusher
			http.Hijacker
		}{rw, rw, rw, rw, rw}
	case ifacePusher: // 01000
		return struct {
			unwrapper
			http.ResponseWriter
			http.Pusher
		}{rw, rw, rw}
	case ifaceCloseNotifier + ifacePusher: // 01001
		return struct {
			unwrapper
			http.ResponseWriter
			http.CloseNotifier
			http.Pusher
		}{rw, rw, rw, rw}
	case ifaceFlusher + ifacePusher: // 01010
		return struct {
			unwrapper
			http.ResponseWriter
			http.Flusher
			http.Pusher
		}{rw, rw, rw, rw}
	case ifaceCloseNotifier + ifaceFlusher + ifacePusher: // 01011
		return struct {
			unwrapper
			http.ResponseWriter
			http.CloseNotifier
			http.Flusher
			http.Pusher
		}{rw, rw, rw, rw, rw}
	case ifaceHijacker + ifacePusher: // 01100
		return struct {
			unwrapper
			http.ResponseWriter
			http.Hijacker
			http.Pusher
		}{rw, rw, rw, rw}
	case ifaceCloseNotifier + ifaceHijacker + ifacePusher: // 01101
		return struct {
			unwrapper
			http.ResponseWriter
			http.CloseNotifier
			http.Hijacker
			http.Pusher
		}{rw, rw, rw, rw, rw}
	case ifaceFlusher + ifaceHijacker + ifacePusher: // 01110
		return struct {
			unwrapper
			http.ResponseWriter
			http.Flusher
			http.Hijacker
			http.Pusher
		}{rw, rw, rw, rw, rw}
	case ifaceCloseNotifier + ifaceFlusher + ifaceHijacker + ifacePusher: // 01111
		return struct {
			unwrapper
			http.ResponseWriter
			http.CloseNotifier
			http.Flusher
			http.Hijacker
			http.Pusher
		}{rw, rw, rw, rw, rw, rw}
	case ifaceReaderFrom: // 10000
		return struct {
			unwrapper
			http.ResponseWriter
			io.ReaderFrom
		}{rw, rw, rw}
	case ifaceCloseNotifier + ifaceReaderFrom: // 10001
		return struct {
			unwrapper
			http.ResponseWriter
			http.CloseNotifier
			io.ReaderFrom
		}{rw, rw, rw, rw}
	case ifaceFlusher + ifaceReaderFrom: // 10010
		return struct {
			unwrapper
			http.ResponseWriter
			http.Flusher
			io.ReaderFrom
		}{rw, rw, rw, rw}
	case ifaceCloseNotifier + ifaceFlusher + ifaceReaderFrom: // 10011
		return struct {
			unwrapper
			http.ResponseWriter
			http.CloseNotifier
			http.Flusher
			io.ReaderFrom
		}{rw, rw, rw, rw, rw}
	case ifaceHijacker + ifaceReaderFrom: // 10100
		return struct {
			unwrapper
			http.ResponseWriter
			http.Hijacker
			io.ReaderFrom
		}{rw, rw, rw, rw}
	case ifaceCloseNotifier + ifaceHijacker + ifaceReaderFrom: // 10101
		return struct {
			unwrapper
			http.ResponseWriter
			http.CloseNotifier
			http.Hijacker
			io.ReaderFrom
		}{rw, rw, rw, rw, rw}
	case ifaceFlusher + ifaceHijacker + ifaceReaderFrom: // 10110
		return struct {
			unwrapper
			http.ResponseWriter
			http.Flusher
			http.Hijacker
			io.ReaderFrom
		}{rw, rw, rw, rw, rw}
	case ifaceCloseNotifier + ifaceFlusher + ifaceHijacker + ifaceReaderFrom: // 10111
		return struct {
			unwrapper
			http.ResponseWriter
			http.CloseNotifier
			http.Flusher
			http.Hijacker
			io.ReaderFrom
		}{rw, rw, rw, rw, rw, rw}
	case ifacePusher + ifaceReaderFrom: // 11000
		return struct {
			unwrapper
			http.ResponseWriter
			http.Pusher
			io.ReaderFrom
		}{rw, rw, rw, rw}
	case ifaceCloseNotifier + ifacePusher + ifaceReaderFrom: // 11001
		return struct {
			unwrapper
			http.ResponseWriter
			http.CloseNotifier
			http.Pusher
			io.ReaderFrom
		}{rw, rw, rw, rw, rw}
	case ifaceFlusher + ifacePusher + ifaceReaderFrom: // 11010
		return struct {
			unwrapper
			http.ResponseWriter
			http.Flusher
			http.Pusher
			io.ReaderFrom
		}{rw, rw, rw, rw, rw}
	case ifaceCloseNotifier + ifaceFlusher + ifacePusher + ifaceReaderFrom: // 11011
		return struct {
			unwrapper
			http.ResponseWriter
			http.CloseNotifier
			http.Flusher
			http.Pusher
			io.ReaderFrom
		}{rw, rw, rw, rw, rw, rw}
	case ifaceHijacker + ifacePusher + ifaceReaderFrom: // 11100
		return struct {
			unwrapper
			http.ResponseWriter
			http.Hijacker
			http.Pusher
			io.ReaderFrom
		}{rw, rw, rw, rw, rw}
	case ifaceCloseNotifier + ifaceHijacker + ifacePusher + ifaceReaderFrom: // 11101
		return struct {
			unwrapper
			http.ResponseWriter
			http.CloseNotifier
			http.Hijacker
			http.Pusher
			io.ReaderFrom
		}{rw, rw, rw, rw, rw, rw}
	case ifaceFlusher + ifaceHijacker + ifacePusher + ifaceReaderFrom: // 11110
		return struct {
			unwrapper
			http.ResponseWriter
			http.Flusher
			http.Hijacker
			http.Pusher
			io.ReaderFrom
		}{rw, rw, rw, rw, rw, rw}
	case ifaceCloseNotifier + ifaceFlusher + ifaceHijacker + ifacePusher + ifaceReaderFrom: // 11111
		return struct {
			unwrapper
			http.ResponseWriter
			http.CloseNotifier
			http.Flusher
			http.Hijacker
			http.Pusher
			io.ReaderFrom
		}{rw, rw, rw, rw, rw, rw, rw}
	}
}

type unwrapper interface{ Unwrap() http.ResponseWriter }

// Unwrap returns the http.ResponseHandler that was wrapped by Wrap or nil otherwise.
func Unwrap(w http.ResponseWriter) http.ResponseWriter {
	if x, ok := w.(unwrapper); ok {
		return x.Unwrap()
	}
	return nil
}

// Struct implements Hooks by passing through all calls.
// Embed it in a struct to avoid having to implements methods
// for hooks that you are not interested in.
type Struct struct{}

// HookHeader implements Hooks.
func (st *Struct) HookHeader(w http.ResponseWriter) http.Header {
	return w.Header()
}

// HookWriteHeader implements Hooks.
func (st *Struct) HookWriteHeader(w http.ResponseWriter, statusCode int) {
	w.WriteHeader(statusCode)
}

// HookWrite implements Hooks.
func (st *Struct) HookWrite(w io.Writer, p []byte) (int, error) {
	return w.Write(p)
}

// HookFlush implements Hooks.
func (st *Struct) HookFlush(flusher http.Flusher) {
	flusher.Flush()
}

// HookHijack implements Hooks.
func (st *Struct) HookHijack(hijacker http.Hijacker) (net.Conn, *bufio.ReadWriter, error) {
	return hijacker.Hijack()
}

// HookPush implements Hooks.
func (st *Struct) HookPush(pusher http.Pusher, target string, opts *http.PushOptions) error {
	return pusher.Push(target, opts)
}
