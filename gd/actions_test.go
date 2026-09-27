package gd_test

import (
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	gd "github.com/dmicheev/djanGO/gd"
)

type Product struct {
	ID       int64  `gd:"primary"`
	Name     string `gd:"len:200"`
	Price    float64
	Discount int64
}

func noRedirect(req *http.Request, via []*http.Request) error {
	return http.ErrUseLastResponse
}

func newJarClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, CheckRedirect: noRedirect}
}

func readAll(t *testing.T, resp *http.Response) string {
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

func extractCSRF(t *testing.T, body string) string {
	t.Helper()
	m := regexp.MustCompile(`name="csrf" value="([0-9a-f]+)"`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("csrf token not found in:\n%s", body)
	}
	return m[1]
}

func csrfOf(t *testing.T, client *http.Client, rawurl string) string {
	t.Helper()
	resp, err := client.Get(rawurl)
	if err != nil {
		t.Fatal(err)
	}
	return extractCSRF(t, readAll(t, resp))
}

func loginClient(t *testing.T, ts *httptest.Server, user, pass string) *http.Client {
	t.Helper()
	client := newJarClient()
	csrf := csrfOf(t, client, ts.URL+"/admin/login")
	resp, err := client.PostForm(ts.URL+"/admin/login", url.Values{
		"csrf": {csrf}, "username": {user}, "password": {pass},
	})
	if err != nil {
		t.Fatal(err)
	}
	readAll(t, resp)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("login failed: %d", resp.StatusCode)
	}
	return client
}

func newActionServer(t *testing.T, actionCalls *int) *httptest.Server {
	t.Helper()
	db, err := gd.OpenDB("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Register(&Product{}); err != nil {
		t.Fatal(err)
	}
	app := gd.New(gd.Settings{SecretKey: "s"})
	admin, err := gd.NewAdmin(app, db)
	if err != nil {
		t.Fatal(err)
	}
	err = admin.Register(&Product{}, gd.ModelAdmin{
		ListDisplay: []string{"Name", "Price", "Discount"},
		Validators: map[string]func(v any) string{
			"Price": func(v any) string {
				if v.(float64) < 0 {
					return "Цена не может быть отрицательной"
				}
				return ""
			},
			"Name": func(v any) string {
				if len(strings.TrimSpace(v.(string))) < 2 {
					return "Слишком короткое название"
				}
				return ""
			},
		},
		Actions: []gd.AdminAction{{
			Name:  "discount10",
			Label: "Сделать скидку 10%",
			Handler: func(c *gd.Ctx, db *gd.DB, ids []int64) error {
				*actionCalls += len(ids)
				for _, id := range ids {
					var p Product
					if err := db.FindByID(&p, id); err != nil {
						return err
					}
					p.Discount = 10
					if err := db.Update(&p); err != nil {
						return err
					}
				}
				return nil
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	admin.Mount("/admin")
	if err := admin.CreateUser("admin", "adminpw", true); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(app)
	t.Cleanup(ts.Close)
	return ts
}

func TestAdminValidators(t *testing.T) {
	var calls int
	ts := newActionServer(t, &calls)
	client := loginClient(t, ts, "admin", "adminpw")

	csrf := csrfOf(t, client, ts.URL+"/admin/product/add")
	resp, err := client.PostForm(ts.URL+"/admin/product/add", url.Values{
		"csrf":  {csrf},
		"Name":  {"A"},
		"Price": {"-5"},
	})
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	if !strings.Contains(body, "Слишком короткое название") ||
		!strings.Contains(body, "Цена не может быть отрицательной") {
		t.Fatalf("validator messages missing:\n%s", body)
	}
}

func TestAdminBulkActions(t *testing.T) {
	var calls int
	ts := newActionServer(t, &calls)
	client := loginClient(t, ts, "admin", "adminpw")

	addCSRF := csrfOf(t, client, ts.URL+"/admin/product/add")
	for i, name := range []string{"One", "Two", "Three"} {
		_ = i
		resp, err := client.PostForm(ts.URL+"/admin/product/add", url.Values{
			"csrf":  {addCSRF},
			"Name":  {name},
			"Price": {"100"},
		})
		if err != nil {
			t.Fatal(err)
		}
		readAll(t, resp)
	}

	listCSRF := csrfOf(t, client, ts.URL+"/admin/product")
	resp, err := client.PostForm(ts.URL+"/admin/product", url.Values{
		"csrf":      {listCSRF},
		"action":    {"discount10"},
		"_selected": {"1", "2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	readAll(t, resp)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("custom action expected 302, got %d", resp.StatusCode)
	}
	if calls != 2 {
		t.Fatalf("custom action ran on %d objects, want 2", calls)
	}

	resp, err = client.PostForm(ts.URL+"/admin/product", url.Values{
		"csrf":      {listCSRF},
		"action":    {"delete_selected"},
		"_selected": {"3"},
	})
	if err != nil {
		t.Fatal(err)
	}
	readAll(t, resp)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("delete action expected 302, got %d", resp.StatusCode)
	}

	resp, err = client.Get(ts.URL + "/admin/product")
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, resp)
	if strings.Contains(body, "Three") {
		t.Fatal("deleted object still listed")
	}
	if !strings.Contains(body, "One") || !strings.Contains(body, "Two") {
		t.Fatal("remaining objects lost")
	}
	if !strings.Contains(body, "delete_selected") {
		t.Fatal("delete_selected action missing from dropdown")
	}
}

func TestDialect(t *testing.T) {
	pg := gd.DetectDialect("pgx")
	my := gd.DetectDialect("go-sql-driver/mysql")
	lite := gd.DetectDialect("sqlite")
	unknown := gd.DetectDialect("weirddb")

	if pg.Name != "postgres" || my.Name != "mysql" || lite.Name != "sqlite" || unknown.Name != "sqlite" {
		t.Fatalf("detection: %s %s %s %s", pg.Name, my.Name, lite.Name, unknown.Name)
	}

	if got := pg.Rebind("a = ? AND b = ?"); got != "a = $1 AND b = $2" {
		t.Fatalf("pg rebind: %q", got)
	}
	if got := my.Rebind("a = ?"); got != "a = ?" {
		t.Fatalf("mysql rebind: %q", got)
	}

	price := &gd.Field{Kind: 2, Len: 0}
	_ = price
	fInt := &gd.Field{Name: "ID", Column: "id", Kind: 0, Primary: true}
	if got := pg.PKColumnDef(fInt); got != "id BIGSERIAL PRIMARY KEY" {
		t.Fatalf("pg pk: %q", got)
	}
	if got := my.PKColumnDef(fInt); got != "id BIGINT AUTO_INCREMENT PRIMARY KEY" {
		t.Fatalf("mysql pk: %q", got)
	}
	if got := lite.PKColumnDef(fInt); got != "id INTEGER PRIMARY KEY AUTOINCREMENT" {
		t.Fatalf("sqlite pk: %q", got)
	}
	if !pg.NeedsReturning() || my.NeedsReturning() || lite.NeedsReturning() {
		t.Fatal("NeedsReturning wrong")
	}
	pgIns := pg.InsertIgnore("t", "a, b")
	if !strings.Contains(pgIns, "ON CONFLICT DO NOTHING") {
		t.Fatalf("pg insert ignore: %q", pgIns)
	}
	if !strings.Contains(my.InsertIgnore("t", "a, b"), "INSERT IGNORE") {
		t.Fatal("mysql insert ignore")
	}
	if !strings.Contains(lite.InsertIgnore("t", "a, b"), "INSERT OR IGNORE") {
		t.Fatal("sqlite insert ignore")
	}
}
