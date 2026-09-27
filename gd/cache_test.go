package gd_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	gd "github.com/dmicheev/djanGO/gd"
)

func TestCache(t *testing.T) {
	c := gd.NewCache(50 * time.Millisecond)
	defer c.Close()

	c.Set("k", "v")
	if v, ok := c.Get("k"); !ok || v.(string) != "v" {
		t.Fatalf("get: %v %v", v, ok)
	}
	c.Delete("k")
	if _, ok := c.Get("k"); ok {
		t.Fatal("expected miss after delete")
	}

	c.SetTTL("ttl", 1, 10*time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	if _, ok := c.Get("ttl"); ok {
		t.Fatal("expected expiry")
	}

	if v, err := c.GetOrSet("go", func() (any, error) { return 42, nil }); err != nil || v.(int) != 42 {
		t.Fatalf("getorset: %v %v", v, err)
	}
	c.Set("n", 1)
	if v, err := c.GetOrSet("go", func() (any, error) { return 99, nil }); err != nil || v.(int) != 42 {
		t.Fatalf("getorset should hit cache: %v %v", v, err)
	}
}

func TestCachePage(t *testing.T) {
	c := gd.NewCache(time.Minute)
	defer c.Close()
	hits := 0
	h := gd.CachePage(c, time.Minute, func(c2 *gd.Ctx) error {
		hits++
		return c2.String(http.StatusOK, "page")
	})
	app := gd.New(gd.Settings{})
	app.GET("/cached", h)

	req := func() *http.Response {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/cached", nil)
		app.ServeHTTP(w, r)
		return w.Result()
	}
	resp := req()
	if resp.Header.Get("X-Cache") != "MISS" {
		t.Fatalf("first response should be MISS, got %q", resp.Header.Get("X-Cache"))
	}
	resp = req()
	if resp.Header.Get("X-Cache") != "HIT" || hits != 1 {
		t.Fatalf("expected HIT with hits=1, got %q hits=%d", resp.Header.Get("X-Cache"), hits)
	}
}

func TestMountAllMethods(t *testing.T) {
	app := gd.New(gd.Settings{})
	var methods []string
	sub := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		w.WriteHeader(http.StatusOK)
	})
	app.Mount("/sub", sub)
	for _, m := range []string{"GET", "POST", "PUT", "DELETE"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(m, "/sub/echo", nil)
		app.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d", m, w.Code)
		}
	}
	if len(methods) != 4 {
		t.Fatalf("sub-handler called %d times", len(methods))
	}
}
