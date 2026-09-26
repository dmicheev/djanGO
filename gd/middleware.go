package gd

import (
	"log"
	"net/http"
	"time"
)

type Middleware func(Handler) Handler

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func chain(h Handler, mw ...Middleware) Handler {
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](h)
	}
	return h
}

func Recover() Middleware {
	return func(next Handler) Handler {
		return func(c *Ctx) error {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[gd] panic: %v", r)
					c.String(http.StatusInternalServerError, "Internal Server Error")
				}
			}()
			return next(c)
		}
	}
}

func Logger() Middleware {
	return func(next Handler) Handler {
		return func(c *Ctx) error {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: c.W, status: http.StatusOK}
			c.W = sw
			err := next(c)
			log.Printf("%s %s -> %d (%s)", c.R.Method, c.R.URL.Path, sw.status, time.Since(start))
			return err
		}
	}
}
