package gd

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAppRouting(t *testing.T) {
	app := New(Settings{})
	app.GET("/hello/:name", func(c *Ctx) error {
		return c.String(http.StatusOK, "hi "+c.Param("name"))
	})
	app.GET("/panic", func(c *Ctx) error {
		panic("boom")
	})

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/hello/go", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "hi go" {
		t.Errorf("got %d %q", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/missing", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("404: got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/hello/go", nil)
	app.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("405: got %d", rec.Code)
	}
}

func TestRecoverMiddleware(t *testing.T) {
	app := New(Settings{})
	app.Use(Recover())
	app.GET("/panic", func(c *Ctx) error {
		panic("boom")
	})
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/panic", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("panic recovered: got %d", rec.Code)
	}
}
