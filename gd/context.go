package gd

import (
	"encoding/json"
	"net/http"
)

type Ctx struct {
	W      http.ResponseWriter
	R      *http.Request
	app    *App
	params map[string]string
}

func (c *Ctx) Param(name string) string {
	return c.params[name]
}

func (c *Ctx) Query(name string) string {
	return c.R.URL.Query().Get(name)
}

func (c *Ctx) Form(name string) string {
	return c.R.FormValue(name)
}

func (c *Ctx) Bind(v any) error {
	defer c.R.Body.Close()
	return json.NewDecoder(c.R.Body).Decode(v)
}

func (c *Ctx) JSON(status int, v any) error {
	c.W.Header().Set("Content-Type", "application/json; charset=utf-8")
	c.W.WriteHeader(status)
	return json.NewEncoder(c.W).Encode(v)
}

func (c *Ctx) String(status int, s string) error {
	c.W.Header().Set("Content-Type", "text/plain; charset=utf-8")
	c.W.WriteHeader(status)
	_, err := c.W.Write([]byte(s))
	return err
}

func (c *Ctx) HTML(status int, name string, data any) error {
	return c.app.Render(c.W, status, name, data)
}

func (c *Ctx) Redirect(url string, status ...int) {
	code := http.StatusFound
	if len(status) > 0 {
		code = status[0]
	}
	http.Redirect(c.W, c.R, url, code)
}

func (c *Ctx) Cookie(name string) (*http.Cookie, error) {
	return c.R.Cookie(name)
}

func (c *Ctx) SetCookie(cookie *http.Cookie) {
	http.SetCookie(c.W, cookie)
}
