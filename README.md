# djanGO

Мини-фреймворк для веба на Go, вдохновлённый Django: роутер, middleware,
контекст запроса, шаблоны, настройки, сервер с graceful shutdown,
мини-ORM с авто-миграциями и **автоматическая админ-панель** по моделям.

Подключается как обычная Go-библиотека:

```bash
go get github.com/dmicheev/djanGO/gd
```

```go
import gd "github.com/dmicheev/djanGO/gd"
```

Ядро (`gd`) использует только стандартную библиотеку; СУБД подключается
через `database/sql` — возьмите любой драйвер (демо использует pure-Go
`modernc.org/sqlite` без CGO).

## Возможности

| Django | djanGO | Где |
|---|---|---|
| `urls.py` + views | Роутер с параметрами `:id`, wildcard `*`, авт. 404/405 | `gd/router.go` |
| HttpRequest/Response | `*gd.Ctx`: JSON, HTML, Bind, Redirect, Cookie | `gd/context.go` |
| middleware | Цепочка Middleware + `Logger`, `Recover` из коробки | `gd/middleware.go` |
| templates | Рендер `html/template` из каталога по имени | `gd/render.go` |
| `settings.py` | `gd.Settings` (включая `Database`) | `gd/settings.go` |
| `runserver` | HTTP-сервер с таймаутами и graceful shutdown | `gd/server.go` |
| models + migrations | Модели-структуры с тегами, `CREATE TABLE`, `ALTER TABLE ADD COLUMN` | `gd/model.go`, `gd/db.go` |
| ORM (QuerySet) | `Create/Update/Save/Delete/FindByID/All`, `Q().Filter().Search().Order().Limit().Count()` | `gd/orm.go` |
| ForeignKey / M2M | `gd:"fk:Model"`, `gd:"m2m:Model"` (+ junction-таблица), `M2MIDs/M2MSet` | `gd/db.go`, `gd/orm.go` |
| **django.contrib.admin** | `gd.NewAdmin` + `Register(model, ModelAdmin{...})` | `gd/admin.go` |
| auth | Логин, сессии (HMAC-cookie), PBKDF2-пароли, CSRF | `gd/session.go`, `gd/password.go` |

## Быстрый старт

Требуется Go 1.22+.

В своём проекте:

```bash
go get github.com/dmicheev/djanGO/gd
go get modernc.org/sqlite        # или другой driver для database/sql
```

```go
package main

import (
    _ "modernc.org/sqlite"

    gd "github.com/dmicheev/djanGO/gd"
)

type Note struct {
    ID   int64  `gd:"primary"`
    Text string `gd:"len:1000"`
}

func main() {
    db, _ := gd.OpenDB("sqlite", "app.db")
    db.Register(&Note{})

    app := gd.New(gd.Settings{Addr: ":8000", SecretKey: "secret"})
    admin, _ := gd.NewAdmin(app, db)
    admin.Register(&Note{}, gd.ModelAdmin{})
    admin.Mount("/admin")
    admin.CreateUser("admin", "admin", true)

    app.Run()
}
```

Или запустите демо из этого репозитория:

```bash
git clone https://github.com/dmicheev/djanGO.git
cd djanGO
go run ./example
# сайт: http://localhost:8000
# админка: http://localhost:8000/admin (admin/admin)
```

## Модели и ORM

Модель — структура с первичным ключом и тегами `gd:"..."`:

```go
type Author struct {
    ID    int64  `gd:"primary"`
    Name  string `gd:"len:200"`
    Email string `gd:"len:254;unique;blank"` // blank = не обязательна в формах
    Bio   string `gd:"widget:textarea;blank"`
}

type Book struct {
    ID        int64     `gd:"primary"`
    Title     string    `gd:"len:300"`
    AuthorID  int64     `gd:"fk:Author"`   // FOREIGN KEY -> authors
    TagIDs    []int64   `gd:"m2m:Tag"`     // junction-таблица book_tags
    Price     float64
    Published bool
    CreatedAt time.Time                   // auto_now_add
    UpdatedAt time.Time                   // auto_now
}
```

Теги полей: `primary`, `unique`, `index`, `null`, `blank`,
`column:имя`, `len:N`, `widget:textarea`, `fk:Модель`, `m2m:Модель`, `-` (пропустить).
Имена таблиц/колонок — snake_case, таблица во множественном числе
(`Book` -> `books`, `AuthorID` -> `author_id`).

```go
db, err := gd.OpenDB("sqlite", "app.db")
db.Register(&Author{}, &Tag{}, &Book{})   // + авто-миграция

db.Create(&book)                          // INSERT, ID заполняется
db.FindByID(&book, 42)                    // SELECT ... WHERE id=42
db.Update(&book)
db.Delete(&book)

db.Q(&Book{}).
    Filter("Published", true).            // WHERE published = ?
    Search([]string{"Title"}, "хобб").    // AND (title LIKE ?)
    Order("-CreatedAt").                  // ORDER BY created_at DESC
    Limit(20).Offset(20).
    All(&books)                           // []Book

n, _ := db.Q(&Book{}).Filter("AuthorID", 1).Count()

db.M2MSet(&book, "TagIDs", []int64{1, 2}) // связь M2M
ids, _ := db.M2MIDs(&book, "TagIDs")
```

Ядро фреймворка работает через `database/sql`; для SQLite демо использует
pure-Go драйвер `modernc.org/sqlite` (без CGO).

## Админка

```go
admin, _ := gd.NewAdmin(app, db)

admin.Register(&Book{}, gd.ModelAdmin{
    ListDisplay:   []string{"Title", "AuthorID", "Price", "Published", "CreatedAt"},
    ListFilter:    []string{"AuthorID", "Published"},   // сайдбар с фильтрами
    SearchFields:  []string{"Title", "Description"},    // поиск по LIKE
    Ordering:      []string{"-CreatedAt"},              // сортировка по умолчанию
    ListPerPage:   10,
    Fields:        nil,          // порядок полей формы (nil = все)
    Exclude:       nil,          // исключить поля из формы
    ReadOnlyFields: nil,         // только для чтения
    BeforeSave:    func(c *gd.Ctx, m any) error { return nil }, // хук
})

admin.Mount("/admin")
admin.CreateUser("admin", "пароль", true) // суперюзер
```

Что даёт админка «из коробки»:

- **индекс** — список зарегистрированных моделей;
- **changelist** — таблица с сортировкой по любому столбцу (клик по заголовку),
  поиском, фильтрами (FK — по связанным объектам, bool — Да/Нет,
  прочие — по DISTINCT-значениям), пагинацией;
- **формы** добавления/изменения, сгенерированные по типам полей:
  select для FK, multiselect для M2M, checkbox для bool,
  `datetime-local` для времени, textarea по тегу `widget:textarea`;
- **валидация** (обязательные поля, типы, UNIQUE с человекочитаемой ошибкой);
- **удаление** с confirm-страницей;
- **auth**: страница логина, сессии в подписанной HMAC-cookie,
  пароли PBKDF2-SHA256, CSRF-токены во всех формах.

Действие BeforeSave может отклонить сохранение, вернув ошибку — она
покажется в форме (как `clean()` в Django).

## Остальное

### Приложение и маршруты

```go
app := gd.New(gd.Settings{
    Addr:         ":8000",
    TemplatesDir: "templates",
    SecretKey:    "change-me-in-production",
    Database:     gd.Database{Driver: "sqlite", DSN: "app.db"},
})

app.Use(gd.Logger(), gd.Recover())

app.GET("/", func(c *gd.Ctx) error {
    return c.HTML(http.StatusOK, "index.html", map[string]any{"Title": "djanGO"})
})

log.Fatal(app.Run())
```

Поддерживаемые методы: `GET`, `POST`, `PUT`, `PATCH`, `DELETE`, а также
`Handle(method, path, handler)` для произвольных.

### Параметры пути

- `/hello/:name` — сегмент `:name` доступен через `c.Param("name")`
- `/files/*` — остаток пути через `c.Param("*")`

## Структура

```
gd/                 пакет фреймворка (только stdlib, подключаемый)
  context.go        *Ctx: запрос/ответ, хелперы
  middleware.go     цепочка, Logger, Recover
  render.go         шаблоны
  router.go         маршрутизация с параметрами
  server.go         App, ServeHTTP, Run (graceful shutdown)
  settings.go       настройки (включая Database)
  model.go          метаданные моделей: теги, имена таблиц, связи
  db.go             OpenDB, Register (авто-миграции, FK, M2M junction)
  orm.go            CRUD, query builder, M2M
  admin.go          AdminSite, ModelAdmin, генерация UI
  session.go        сессии (HMAC-cookie)
  password.go       PBKDF2-SHA256
  admin_templates/  встроенные шаблоны админки (embed)
example/            демо-приложение (Author/Tag/Book + админка)
  main.go
  templates/
docs/               скриншоты
```

## Дорожная карта

- [x] Мини-ORM: модели через теги, миграции
- [x] Сессии и аутентификация (cookie + PBKDF2)
- [x] Админ-панель с автогенерацией CRUD
- [ ] Валидация форм (кастомные clean-методы полей)
- [x] Кэширование — пока нет; вместо него busy_timeout и индексы
- [ ] Кастомные действия в changelist (bulk delete и т.п.)
- [ ] Драйверы PostgreSQL/MySQL (диалектная прослойка)

## Тесты

```bash
go test ./...
```

## Лицензия

MIT
