package main

import (
	"net/http"
	"time"

	"godjango/gd"
)

type helloResponse struct {
	Hello string    `json:"hello"`
	Now   time.Time `json:"now"`
}

func main() {
	app := gd.New(gd.Settings{
		Addr:         ":8000",
		TemplatesDir: "templates",
	})

	app.Use(gd.Logger(), gd.Recover())

	app.GET("/", func(c *gd.Ctx) error {
		return c.HTML(http.StatusOK, "index.html", map[string]any{
			"Title": "go_django",
		})
	})

	app.GET("/hello/:name", func(c *gd.Ctx) error {
		return c.JSON(http.StatusOK, helloResponse{
			Hello: c.Param("name"),
			Now:   time.Now(),
		})
	})

	app.POST("/echo", func(c *gd.Ctx) error {
		var body map[string]any
		if err := c.Bind(&body); err != nil {
			return c.String(http.StatusBadRequest, "invalid json")
		}
		return c.JSON(http.StatusOK, body)
	})

	if err := app.Run(); err != nil {
		panic(err)
	}
}
