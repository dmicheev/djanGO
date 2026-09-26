# go_django

Мини-фреймворк для веба на Go, вдохновлённый Django: роутер, middleware,
контекст запроса, шаблоны, настройки и сервер с graceful shutdown — всё в
~400 строк без внешних зависимостей (только стандартная библиотека).

## Возможности

| Django | go_django | Где |
|---|---|---|
| `urls.py` + views | Роутер с параметрами `:id`, wildcard `*`, авт. 404/405 | `gd/router.go` |
| HttpRequest/Response | `*gd.Ctx`: JSON, HTML, Bind, Redirect, Cookie | `gd/context.go` |
| middleware | Цепочка Middleware + `Logger`, `Recover` из коробки | `gd/middleware.go` |
| templates | Рендер `html/template` из каталога по имени | `gd/render.go` |
| `settings.py` | `gd.Settings` | `gd/settings.go` |
| `runserver` | HTTP-сервер с таймаутами и graceful shutdown | `gd/server.go` |

## Быстрый старт

Требуется Go 1.22+.

```bash
git clone https://github.com/dmicheev/go_django.git
cd go_django
go run .
# открой http://localhost:8000
```

## Использование

### Приложение и маршруты

```go
app := gd.New(gd.Settings{
	Addr:         ":8000",
	TemplatesDir: "templates",
})

app.Use(gd.Logger(), gd.Recover())

app.GET("/", func(c *gd.Ctx) error {
	return c.HTML(http.StatusOK, "index.html", map[string]any{"Title": "go_django"})
})

app.GET("/hello/:name", func(c *gd.Ctx) error {
	return c.JSON(http.StatusOK, map[string]string{"hello": c.Param("name")})
})

app.POST("/echo", func(c *gd.Ctx) error {
	var body map[string]any
	if err := c.Bind(&body); err != nil {
		return c.String(http.StatusBadRequest, "invalid json")
	}
	return c.JSON(http.StatusOK, body)
})

log.Fatal(app.Run())
```

Поддерживаемые методы: `GET`, `POST`, `PUT`, `PATCH`, `DELETE`, а также
`Handle(method, path, handler)` для произвольных.

### Параметры пути

- `/hello/:name` — сегмент `:name` доступен через `c.Param("name")`
- `/files/*` — остаток пути через `c.Param("*")`

### Middleware

```go
type Middleware func(Handler) Handler

func RequestID() gd.Middleware {
	return func(next gd.Handler) gd.Handler {
		return func(c *gd.Ctx) error {
			id := uuid.NewString()
			c.W.Header().Set("X-Request-ID", id)
			return next(c)
		}
	}
}
```

Middleware применяются в порядке добавления (`app.Use(...)`): каждый
оборачивает следующий, как луковица.

### Шаблоны

Все `*.html` из `TemplatesDir` парсятся при первом запросе. Рендер:

```go
return c.HTML(http.StatusOK, "index.html", data)
```

Внутри шаблонов доступен полный синтаксис `html/template`. Для «наследования»
шаблонов используйте `{{define}}` / `{{template}}`.

## Структура

```
gd/                 пакет фреймворка
  context.go        *Ctx: запрос/ответ, хелперы
  middleware.go     цепочка, Logger, Recover
  render.go         шаблоны
  router.go         маршрутизация с параметрами
  server.go         App, ServeHTTP, Run (graceful shutdown)
  settings.go       настройки
main.go             демо-приложение
templates/          HTML-шаблоны демо
```

## Дорожная карта

- [ ] Мини-ORM: модели через теги, миграции
- [ ] Сессии и аутентификация (cookie + bcrypt)
- [ ] Админ-панель с автогенерацией CRUD
- [ ] Валидация форм
- [ ] Кэширование

## Тесты

```bash
go test ./...
```

## Лицензия

MIT
