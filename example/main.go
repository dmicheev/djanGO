package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	_ "modernc.org/sqlite"

	gd "github.com/dmicheev/djanGO/gd"
)

type Author struct {
	ID    int64  `gd:"primary"`
	Name  string `gd:"len:200"`
	Email string `gd:"len:254;unique"`
	Bio   string `gd:"widget:textarea;blank"`
}

func (a Author) String() string { return a.Name }

type Tag struct {
	ID   int64  `gd:"primary"`
	Name string `gd:"len:100"`
}

func (t Tag) String() string { return t.Name }

type Book struct {
	ID          int64   `gd:"primary"`
	Title       string  `gd:"len:300"`
	AuthorID    int64   `gd:"fk:Author"`
	TagIDs      []int64 `gd:"m2m:Tag"`
	Price       float64
	Published   bool
	Description string `gd:"widget:textarea;blank"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (b Book) String() string { return b.Title }

func main() {
	db, err := gd.OpenDB("sqlite", "app.db")
	if err != nil {
		log.Fatalf("db: %v", err)
	}

	if err := db.Register(&Author{}, &Tag{}, &Book{}); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	app := gd.New(gd.Settings{
		Addr:         ":8000",
		TemplatesDir: "example/templates",
		SecretKey:    "change-me-in-production",
		Database:     gd.Database{Driver: "sqlite", DSN: "app.db"},
	})

	app.Use(gd.Logger(), gd.Recover())

	admin, err := gd.NewAdmin(app, db)
	if err != nil {
		log.Fatalf("admin: %v", err)
	}
	if err := admin.Register(&Author{}, gd.ModelAdmin{
		ListDisplay:  []string{"Name", "Email"},
		SearchFields: []string{"Name", "Email"},
	}); err != nil {
		log.Fatal(err)
	}
	if err := admin.Register(&Tag{}, gd.ModelAdmin{
		ListDisplay:  []string{"Name"},
		SearchFields: []string{"Name"},
	}); err != nil {
		log.Fatal(err)
	}
	if err := admin.Register(&Book{}, gd.ModelAdmin{
		ListDisplay:  []string{"Title", "AuthorID", "Price", "Published", "CreatedAt"},
		ListFilter:   []string{"AuthorID", "Published"},
		SearchFields: []string{"Title", "Description"},
		Ordering:     []string{"-CreatedAt"},
		ListPerPage:  10,
		BeforeSave: func(c *gd.Ctx, m any) error {
			book, ok := m.(*Book)
			if !ok {
				return nil
			}
			if book.Price > 10000 {
				return fmt.Errorf("слишком дорогая книга: %.2f", book.Price)
			}
			return nil
		},
	}); err != nil {
		log.Fatal(err)
	}
	admin.Mount("/admin")

	seed(db, admin)

	app.GET("/", func(c *gd.Ctx) error {
		return c.HTML(http.StatusOK, "index.html", map[string]any{
			"Title": "go_django",
		})
	})

	app.GET("/hello/:name", func(c *gd.Ctx) error {
		return c.JSON(http.StatusOK, map[string]any{
			"hello": c.Param("name"),
			"now":   time.Now(),
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

func seed(db *gd.DB, admin *gd.AdminSite) {
	if users, _ := db.Q(&gd.AdminUser{}).Count(); users == 0 {
		if err := admin.CreateUser("admin", "admin", true); err != nil {
			log.Printf("seed admin user: %v", err)
		} else {
			log.Printf("[seed] admin user created: admin/admin")
		}
	}
	if n, _ := db.Q(&Author{}).Count(); n == 0 {
		tolkien := Author{Name: "Дж. Р. Р. Толкин", Email: "tolkien@example.com", Bio: "Английский писатель и филолог."}
		clarke := Author{Name: "Артур Кларк", Email: "clarke@example.com", Bio: "Английский писатель-фантаст."}
		db.Create(&tolkien)
		db.Create(&clarke)

		fantasy := Tag{Name: "фэнтези"}
		scifi := Tag{Name: "фантастика"}
		classic := Tag{Name: "классика"}
		db.Create(&fantasy)
		db.Create(&scifi)
		db.Create(&classic)

		hobbit := Book{Title: "Хоббит, или Туда и обратно", AuthorID: tolkien.ID, Price: 899.90, Published: true, Description: "Повесть-сказка про хоббита Бильбо."}
		rings := Book{Title: "Властелин колец", AuthorID: tolkien.ID, Price: 2499.00, Published: true, Description: "Роман-эпопея в трёх томах."}
		odyssey := Book{Title: "2001: Космическая одиссея", AuthorID: clarke.ID, Price: 750.50, Published: false, Description: "Научно-фантастический роман."}
		db.Create(&hobbit)
		db.Create(&rings)
		db.Create(&odyssey)
		db.M2MSet(&hobbit, "TagIDs", []int64{fantasy.ID, classic.ID})
		db.M2MSet(&rings, "TagIDs", []int64{fantasy.ID})
		db.M2MSet(&odyssey, "TagIDs", []int64{scifi.ID})
		log.Printf("[seed] demo data created")
	}
}
