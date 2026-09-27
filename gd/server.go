package gd

import (
	"context"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type App struct {
	Settings Settings
	router   *Router
	mw       []Middleware
	tmpl     *Templates
}

func New(s Settings) *App {
	return &App{Settings: s, router: NewRouter()}
}

func (a *App) Use(mw ...Middleware) {
	a.mw = append(a.mw, mw...)
}

func (a *App) GET(path string, h Handler) {
	a.router.Add(http.MethodGet, path, h)
}

func (a *App) POST(path string, h Handler) {
	a.router.Add(http.MethodPost, path, h)
}

func (a *App) PUT(path string, h Handler) {
	a.router.Add(http.MethodPut, path, h)
}

func (a *App) PATCH(path string, h Handler) {
	a.router.Add(http.MethodPatch, path, h)
}

func (a *App) DELETE(path string, h Handler) {
	a.router.Add(http.MethodDelete, path, h)
}

func (a *App) Handle(method, path string, h Handler) {
	a.router.Add(method, path, h)
}

func (a *App) Mount(prefix string, h http.Handler) {
	handler := func(c *Ctx) error {
		h.ServeHTTP(c.W, c.R)
		return nil
	}
	for _, m := range []string{
		http.MethodGet, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodHead, http.MethodOptions,
	} {
		a.Handle(m, prefix+"/*", handler)
	}
}

func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if a.tmpl == nil {
		t, err := LoadTemplates(a.Settings.TemplatesDir)
		if err != nil {
			log.Printf("[gd] templates: %v", err)
		}
		if t == nil {
			t = &Templates{set: template.New("gd"), names: make(map[string]bool)}
		}
		a.tmpl = t
	}
	c := &Ctx{W: w, R: r, app: a}
	h, params, ok := a.router.Match(r.Method, r.URL.Path)
	if !ok {
		if a.router.HasPathButOtherMethod(r.Method, r.URL.Path) {
			c.String(http.StatusMethodNotAllowed, "405 Method Not Allowed")
			return
		}
		c.String(http.StatusNotFound, "404 Not Found")
		return
	}
	c.params = params
	h = chain(h, a.mw...)
	if err := h(c); err != nil {
		log.Printf("[gd] handler error: %v", err)
		if !headerWritten(w) {
			c.String(http.StatusInternalServerError, "Internal Server Error")
		}
	}
}

func headerWritten(w http.ResponseWriter) bool {
	rw, ok := w.(*statusWriter)
	return ok && rw.status != http.StatusOK
}

func (a *App) Run() error {
	srv := &http.Server{
		Addr:         a.Settings.Addr,
		Handler:      a,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 15 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		log.Printf("[gd] listening on %s", a.Settings.Addr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		log.Printf("[gd] shutting down")
		return srv.Shutdown(shutdownCtx)
	}
}
