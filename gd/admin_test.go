package gd_test

import (
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	gd "github.com/dmicheev/djanGO/gd"
)

type Tag struct {
	ID   int64  `gd:"primary"`
	Name string `gd:"len:100"`
}

func (t Tag) String() string { return t.Name }

type Author struct {
	ID    int64  `gd:"primary"`
	Name  string `gd:"len:200"`
	Email string `gd:"len:254;unique;blank"`
	Bio   string `gd:"widget:textarea;blank"`
}

func (a Author) String() string { return a.Name }

type Book struct {
	ID        int64   `gd:"primary"`
	Title     string  `gd:"len:300"`
	AuthorID  int64   `gd:"fk:Author"`
	TagIDs    []int64 `gd:"m2m:Tag"`
	Price     float64 `gd:"blank"`
	Published bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (b Book) String() string { return b.Title }

func setupDB(t *testing.T) *gd.DB {
	t.Helper()
	db, err := gd.OpenDB("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Register(&Tag{}, &Author{}, &Book{}); err != nil {
		t.Fatalf("register: %v", err)
	}
	return db
}

func TestORMCRUD(t *testing.T) {
	db := setupDB(t)

	author := Author{Name: "Толкин", Email: "tolkien@example.com", Bio: "писатель"}
	if err := db.Create(&author); err != nil {
		t.Fatalf("create author: %v", err)
	}
	if author.ID == 0 {
		t.Fatal("expected backfilled ID")
	}

	tag1 := Tag{Name: "фэнтези"}
	tag2 := Tag{Name: "классика"}
	db.Create(&tag1)
	db.Create(&tag2)

	book := Book{Title: "Хоббит", AuthorID: author.ID, Price: 899.9, Published: true}
	if err := db.Create(&book); err != nil {
		t.Fatalf("create book: %v", err)
	}
	if book.CreatedAt.IsZero() {
		t.Fatal("expected CreatedAt to be auto-set")
	}
	if err := db.M2MSet(&book, "TagIDs", []int64{tag1.ID, tag2.ID}); err != nil {
		t.Fatalf("m2m set: %v", err)
	}

	var got Book
	if err := db.FindByID(&got, book.ID); err != nil {
		t.Fatalf("find: %v", err)
	}
	if got.Title != "Хоббит" || got.AuthorID != author.ID || got.Price != 899.9 {
		t.Fatalf("wrong book: %+v", got)
	}

	ids, err := db.M2MIDs(&got, "TagIDs")
	if err != nil || len(ids) != 2 {
		t.Fatalf("m2m ids: %v %v", ids, err)
	}

	got.Price = 999
	got.Title = "Хоббит (2-е издание)"
	if err := db.Update(&got); err != nil {
		t.Fatalf("update: %v", err)
	}
	var got2 Book
	db.FindByID(&got2, got.ID)
	if got2.Price != 999 {
		t.Fatalf("update not applied: %v", got2.Price)
	}

	n, err := db.Q(&Book{}).Filter("Published", true).Count()
	if err != nil || n != 1 {
		t.Fatalf("filter count: %d %v", n, err)
	}

	var found []Book
	if err := db.Q(&Book{}).Search([]string{"Title"}, "Хобб").All(&found); err != nil || len(found) != 1 {
		t.Fatalf("search: %d %v", len(found), err)
	}

	var ordered []Book
	db.Q(&Book{}).Order("-ID").Limit(1).All(&ordered)
	if len(ordered) != 1 || ordered[0].ID != got.ID {
		t.Fatalf("order/limit failed")
	}

	if err := db.Delete(&got); err != nil {
		t.Fatalf("delete: %v", err)
	}
	n, _ = db.Q(&Book{}).Count()
	if n != 0 {
		t.Fatalf("expected 0 books after delete, got %d", n)
	}
}

func TestUniqueViolation(t *testing.T) {
	db := setupDB(t)
	db.Create(&Author{Name: "A", Email: "dup@example.com"})
	err := db.Create(&Author{Name: "B", Email: "dup@example.com"})
	if err == nil || !strings.Contains(err.Error(), "UNIQUE") {
		t.Fatalf("expected UNIQUE violation, got: %v", err)
	}
}

func TestPassword(t *testing.T) {
	h := gd.HashPassword("secret")
	if !gd.CheckPassword("secret", h) {
		t.Fatal("correct password rejected")
	}
	if gd.CheckPassword("wrong", h) {
		t.Fatal("wrong password accepted")
	}
}

type adminEnv struct {
	ts   *httptest.Server
	db   *gd.DB
	csrf string
}

func login(t *testing.T, ts *httptest.Server) (*http.Client, string) {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	resp, err := client.Get(ts.URL + "/admin/login")
	if err != nil {
		t.Fatal(err)
	}
	body := readBody(t, resp)
	m := regexp.MustCompile(`name="csrf" value="([0-9a-f]+)"`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no csrf token in login page:\n%s", body)
	}
	csrf := m[1]

	resp, err = client.PostForm(ts.URL+"/admin/login", url.Values{
		"csrf":     {csrf},
		"username": {"admin"},
		"password": {"adminpw"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("login expected 302, got %d", resp.StatusCode)
	}
	return client, csrf
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return sb.String()
}

func newAdminServer(t *testing.T) *httptest.Server {
	t.Helper()
	db, err := gd.OpenDB("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Register(&Tag{}, &Author{}, &Book{}); err != nil {
		t.Fatal(err)
	}
	app := gd.New(gd.Settings{Addr: ":0", TemplatesDir: "templates", SecretKey: "test-secret"})
	admin, err := gd.NewAdmin(app, db)
	if err != nil {
		t.Fatal(err)
	}
	admin.Register(&Author{}, gd.ModelAdmin{ListDisplay: []string{"Name", "Email"}, SearchFields: []string{"Name"}})
	admin.Register(&Book{}, gd.ModelAdmin{
		ListDisplay:  []string{"Title", "AuthorID", "Published"},
		ListFilter:   []string{"Published"},
		SearchFields: []string{"Title"},
	})
	admin.Mount("/admin")
	if err := admin.CreateUser("admin", "adminpw", true); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(app)
	t.Cleanup(ts.Close)
	return ts
}

func TestAdminAuth(t *testing.T) {
	ts := newAdminServer(t)

	noRedirect := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := noRedirect.Get(ts.URL + "/admin/")
	if err != nil {
		t.Fatal(err)
	}
	readBody(t, resp)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected redirect to login, got %d", resp.StatusCode)
	}

	client, _ := login(t, ts)
	resp, err = client.Get(ts.URL + "/admin/")
	if err != nil {
		t.Fatal(err)
	}
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "Authors") || !strings.Contains(body, "Books") {
		t.Fatalf("admin index unexpected: %d", resp.StatusCode)
	}
}

func TestAdminFullFlow(t *testing.T) {
	ts := newAdminServer(t)
	client, csrf := login(t, ts)

	getCSRF := func(path string) string {
		resp, err := client.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body := readBody(t, resp)
		m := regexp.MustCompile(`name="csrf" value="([0-9a-f]+)"`).FindStringSubmatch(body)
		if m == nil {
			t.Fatalf("no csrf on %s", path)
		}
		return m[1]
	}

	resp, _ := client.PostForm(ts.URL+"/admin/author/add", url.Values{
		"csrf":  {csrf},
		"Name":  {"Толкин"},
		"Email": {"tolkien@example.com"},
		"Bio":   {"писатель"},
	})
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("add author expected 302, got %d: %s", resp.StatusCode, readBody(t, resp))
	}
	readBody(t, resp)

	resp, _ = client.PostForm(ts.URL+"/admin/book/add", url.Values{
		"csrf":      {csrf},
		"Title":     {"Хоббит"},
		"AuthorID":  {"1"},
		"Price":     {"899.9"},
		"Published": {"on"},
		"CreatedAt": {""},
		"UpdatedAt": {""},
		"TagIDs":    {},
	})
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("add book expected 302, got %d: %s", resp.StatusCode, readBody(t, resp))
	}
	readBody(t, resp)

	resp, err := client.Get(ts.URL + "/admin/book/")
	if err != nil {
		t.Fatal(err)
	}
	body := readBody(t, resp)
	for _, want := range []string{"Хоббит", "Толкин", "Да"} {
		if !strings.Contains(body, want) {
			t.Fatalf("changelist missing %q", want)
		}
	}

	resp, err = client.Get(ts.URL + "/admin/book/?q=" + url.QueryEscape("Хобб"))
	if err != nil {
		t.Fatal(err)
	}
	if body := readBody(t, resp); !strings.Contains(body, "Хоббит") {
		t.Fatal("search failed")
	}

	editCSRF := getCSRF("/admin/book/1/")
	resp, _ = client.PostForm(ts.URL+"/admin/book/1/", url.Values{
		"csrf":      {editCSRF},
		"Title":     {"Хоббит (изд. 2)"},
		"AuthorID":  {"1"},
		"Price":     {"999"},
		"Published": {},
	})
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("edit book expected 302, got %d: %s", resp.StatusCode, readBody(t, resp))
	}
	readBody(t, resp)

	resp, err = client.Get(ts.URL + "/admin/book/1/")
	if err != nil {
		t.Fatal(err)
	}
	body = readBody(t, resp)
	if !strings.Contains(body, "Хоббит (изд. 2)") {
		t.Fatal("edit not applied")
	}

	delCSRF := getCSRF("/admin/book/1/delete")
	resp, _ = client.PostForm(ts.URL+"/admin/book/1/delete", url.Values{"csrf": {delCSRF}})
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("delete expected 302, got %d", resp.StatusCode)
	}
	readBody(t, resp)

	resp, _ = client.Get(ts.URL + "/admin/book/")
	body = readBody(t, resp)
	if strings.Contains(body, "/admin/book/1") {
		t.Fatal("book still in changelist after delete")
	}
}

func TestAdminValidation(t *testing.T) {
	ts := newAdminServer(t)
	client, csrf := login(t, ts)

	resp, _ := client.PostForm(ts.URL+"/admin/book/add", url.Values{
		"csrf":     {csrf},
		"Title":    {""},
		"AuthorID": {""},
	})
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	if !strings.Contains(body, "Обязательное поле") || !strings.Contains(body, "Выберите значение") {
		t.Fatalf("validation errors missing:\n%s", body)
	}
}
