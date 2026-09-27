package gd

import "testing"

func TestSplitPath(t *testing.T) {
	cases := []struct {
		path string
		want int
	}{
		{"/", 0},
		{"", 0},
		{"/users", 1},
		{"/users/42/posts", 3},
	}
	for _, tc := range cases {
		if got := len(splitPath(tc.path)); got != tc.want {
			t.Errorf("splitPath(%q) len = %d, want %d", tc.path, got, tc.want)
		}
	}
}

func TestRouterMatch(t *testing.T) {
	rt := NewRouter()
	rt.Add("GET", "/hello/:name", func(c *Ctx) error { return nil })
	rt.Add("GET", "/files/*", func(c *Ctx) error { return nil })
	rt.Add("GET", "/exact", func(c *Ctx) error { return nil })

	cases := []struct {
		path   string
		ok     bool
		params map[string]string
	}{
		{"/hello/go", true, map[string]string{"name": "go"}},
		{"/hello", false, nil},
		{"/files/a/b/c.txt", true, map[string]string{"*": "a/b/c.txt"}},
		{"/exact", true, map[string]string{}},
		{"/exact/extra", false, nil},
	}
	for _, tc := range cases {
		_, params, ok := rt.Match("GET", tc.path)
		if ok != tc.ok {
			t.Errorf("Match(%q) ok = %v, want %v", tc.path, ok, tc.ok)
			continue
		}
		if !ok {
			continue
		}
		for k, want := range tc.params {
			if params[k] != want {
				t.Errorf("Match(%q) param[%q] = %q, want %q", tc.path, k, params[k], want)
			}
		}
	}
}

func TestRouterMethodMismatch(t *testing.T) {
	rt := NewRouter()
	rt.Add("GET", "/only-get", func(c *Ctx) error { return nil })
	if _, _, ok := rt.Match("POST", "/only-get"); ok {
		t.Error("POST should not match GET route")
	}
	if !rt.HasPathButOtherMethod("POST", "/only-get") {
		t.Error("HasPathButOtherMethod should detect 405 case")
	}
}
