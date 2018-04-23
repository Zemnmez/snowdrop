/*
	Snowdrop is an HTTP(s) forward and reverse proxy for go intended for small loads.

	Snowdrop is brittle, and configured more for experimental systems than production
	ones, though with a little tidying I'm sure it would be comfortable in either.
*/
package snowdrop

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"sync"
	"time"
)

type Server struct {
}

type ResponseBufferer struct {
	http.ResponseWriter
	code     int
	buf      bytes.Buffer
	hijacked bool
}

func (f *ResponseBufferer) Write(b []byte) (n int, err error) { return f.buf.Write(b) }
func (f *ResponseBufferer) WriteHeader(statusCode int)        { f.code = statusCode }
func (f *ResponseBufferer) Flush() (n int, err error) {
	if f.hijacked {
		return 0, nil
	}
	f.ResponseWriter.WriteHeader(f.code)
	return f.ResponseWriter.Write(f.buf.Bytes())
}

func (f *ResponseBufferer) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	f.ResponseWriter.WriteHeader(http.StatusOK)
	f.hijacked = true

	hj, ok := f.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("cannot hijack :(")
	}

	return hj.Hijack()
}

func (s Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rb := ResponseBufferer{ResponseWriter: w}
	defer rb.Flush()

	code, err := s.serveHTTP(&rb, r)

	if rb.code == 0 {
		rb.code = code
	}

	if err != nil || !(rb.code > 199 && rb.code < 201) {
		if rb.code == 0 {
			rb.code = 500
		}

		if err == nil {
			err = errors.New(http.StatusText(code))
		}

		log.Printf("ERROR %d %s", rb.code, err)

		http.Error(&rb, err.Error(), rb.code)
	}
}

func printIfError(errfn func() error) {
	if err := errfn(); err != nil {
		log.Println(err)
	}
}

func (s Server) serveHTTP(w http.ResponseWriter, r *http.Request) (code int, err error) {
	requestBytes, err := httputil.DumpRequest(r, true)
	if err != nil {
		return
	}

	if _, err = os.Stderr.Write(requestBytes); err != nil {
		return
	}

	switch r.Method {
	case "CONNECT":
		var destConn net.Conn
		destConn, err = net.DialTimeout("tcp", r.Host, 10*time.Second)
		if err != nil {
			return
		}

		log.Printf("[%s] successful conn", r.Host)

		hijacker, ok := w.(http.Hijacker)
		if !ok {
			return 500, errors.New("could not hijack connection")
		}

		var clientConn net.Conn
		clientConn, _, err = hijacker.Hijack()
		if err != nil {
			return
		}

		var g sync.WaitGroup
		defer printIfError(clientConn.Close)
		defer printIfError(destConn.Close)

		log.Printf("[%s] setting up bilateral conn", r.Host)

		var cp = func(dest io.WriteCloser, src io.ReadCloser) {
			log.Printf("[%s] copying bytes...", r.Host)
			n, err := io.Copy(dest, src)
			log.Printf("[%s] copied %d bytes", r.Host, n)
			if err != nil {
				log.Println(err)
			}

			g.Done()
		}

		g.Add(2)
		go cp(destConn, clientConn)
		go cp(clientConn, destConn)

		log.Printf("[%s] waiting for conn to close...", r.Host)
		g.Wait()
		log.Printf("[%s] conn closed", r.Host)
	default:
		proxy := httputil.NewSingleHostReverseProxy(r.URL)
		r.URL.Path = ""
		proxy.ServeHTTP(w, r)
	}

	return
}
