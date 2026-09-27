package gd

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"
)

//go:embed admin_templates/*.html
var adminTemplatesFS embed.FS

type AdminAction struct {
	Name    string
	Label   string
	Handler func(c *Ctx, db *DB, ids []int64) error
}

type ModelAdmin struct {
	ListDisplay    []string
	ListFilter     []string
	SearchFields   []string
	Ordering       []string
	ListPerPage    int
	Fields         []string
	Exclude        []string
	ReadOnlyFields []string
	Actions        []AdminAction
	Validators     map[string]func(v any) string
	BeforeSave     func(c *Ctx, m any) error
}

type AdminUser struct {
	ID           int64  `gd:"primary"`
	Username     string `gd:"len:150;unique"`
	PasswordHash string `gd:"len:300;widget:textarea"`
	IsSuperuser  bool
	CreatedAt    time.Time
}

type adminModel struct {
	mi  *ModelInfo
	cfg ModelAdmin
}

func (am *adminModel) label() string    { return plural(am.mi.Name) }
func (am *adminModel) singular() string { return am.mi.Name }

type AdminSite struct {
	app    *App
	db     *DB
	prefix string
	store  *SessionStore
	tpls   *template.Template
	models map[string]*adminModel
}

func NewAdmin(app *App, db *DB) (*AdminSite, error) {
	if err := db.Register(&AdminUser{}); err != nil {
		return nil, err
	}
	tpls, err := template.ParseFS(adminTemplatesFS, "admin_templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("gd: admin templates: %w", err)
	}
	secret := app.Settings.SecretKey
	if secret == "" {
		secret = "gd-admin-secret"
	}
	a := &AdminSite{
		app:    app,
		db:     db,
		prefix: "/admin",
		store:  NewSessionStore(secret),
		tpls:   tpls,
		models: make(map[string]*adminModel),
	}
	if u := db.Model("AdminUser"); u != nil {
		a.models[u.URLName] = &adminModel{
			mi: u,
			cfg: ModelAdmin{
				ListDisplay: []string{"Username", "IsSuperuser", "CreatedAt"},
				Fields:      []string{"Username", "IsSuperuser", "CreatedAt"},
				ListPerPage: 20,
			},
		}
	}
	return a, nil
}

func (a *AdminSite) Register(model any, cfg ModelAdmin) error {
	mi, err := parseModel(model)
	if err != nil {
		return err
	}
	if err := a.db.Register(model); err != nil {
		return err
	}
	if cfg.ListPerPage <= 0 {
		cfg.ListPerPage = 20
	}
	a.models[mi.URLName] = &adminModel{mi: mi, cfg: cfg}
	return nil
}

func (a *AdminSite) CreateUser(username, password string, superuser bool) error {
	u := AdminUser{
		Username:     username,
		PasswordHash: HashPassword(password),
		IsSuperuser:  superuser,
		CreatedAt:    time.Now(),
	}
	return a.db.Create(&u)
}

func (a *AdminSite) Mount(prefix string) {
	a.prefix = strings.TrimSuffix(prefix, "/")
	p := a.prefix

	a.app.GET(p+"/login", a.handleLogin)
	a.app.POST(p+"/login", a.handleLogin)
	a.app.POST(p+"/logout", a.handleLogout)
	a.app.GET(p, a.guard(a.handleIndex))
	a.app.GET(p+"/:m", a.guard(a.handleList))
	a.app.POST(p+"/:m", a.guard(a.handleList))
	a.app.GET(p+"/:m/add", a.guard(func(c *Ctx, s *Session) error { return a.handleForm(c, s, true) }))
	a.app.POST(p+"/:m/add", a.guard(func(c *Ctx, s *Session) error { return a.handleForm(c, s, true) }))
	a.app.GET(p+"/:m/:id", a.guard(func(c *Ctx, s *Session) error { return a.handleForm(c, s, false) }))
	a.app.POST(p+"/:m/:id", a.guard(func(c *Ctx, s *Session) error { return a.handleForm(c, s, false) }))
	a.app.GET(p+"/:m/:id/delete", a.guard(a.handleDelete))
	a.app.POST(p+"/:m/:id/delete", a.guard(a.handleDelete))
}

func (a *AdminSite) model(urlName string) *adminModel {
	return a.models[urlName]
}

func (a *AdminSite) guard(h func(*Ctx, *Session) error) Handler {
	return func(c *Ctx) error {
		s := a.store.Get(c.R)
		if s == nil || s.Get("user") == nil {
			next := url.QueryEscape(c.R.URL.Path)
			c.Redirect(a.prefix + "/login?next=" + next)
			return nil
		}
		return h(c, s)
	}
}

func (a *AdminSite) render(c *Ctx, status int, name string, data any) error {
	c.W.Header().Set("Content-Type", "text/html; charset=utf-8")
	c.W.WriteHeader(status)
	if err := a.tpls.ExecuteTemplate(c.W, name, data); err != nil {
		return fmt.Errorf("gd: admin template %s: %w", name, err)
	}
	return nil
}

type baseData struct {
	Prefix string
	Title  string
	User   string
	Flash  string
	CSRF   string
}

func (a *AdminSite) base(c *Ctx, s *Session, title string) baseData {
	d := baseData{Prefix: a.prefix, Title: title}
	if s != nil {
		if u, ok := s.Get("user").(string); ok {
			d.User = u
		}
		d.CSRF, _ = s.Get("csrf").(string)
		if f, ok := s.Get("flash").(string); ok && f != "" {
			d.Flash = f
			s.Delete("flash")
			a.store.Save(c.W, s)
		}
	}
	return d
}

func csrfOK(c *Ctx, s *Session) bool {
	tok, _ := s.Get("csrf").(string)
	return tok != "" && c.Form("csrf") == tok
}

func (a *AdminSite) flash(c *Ctx, s *Session, msg string) {
	s.Set("flash", msg)
	a.store.Save(c.W, s)
}

func (a *AdminSite) notFound(c *Ctx) error {
	return c.String(http.StatusNotFound, "404 Not Found")
}

func (a *AdminSite) handleLogin(c *Ctx) error {
	s := a.store.Get(c.R)
	if s == nil {
		s = a.store.New()
		if _, ok := s.Get("csrf").(string); !ok {
			s.Set("csrf", randomToken())
		}
		a.store.Save(c.W, s)
	}
	if s.Get("user") != nil {
		c.Redirect(a.prefix + "/")
		return nil
	}
	data := struct {
		baseData
		CSRF  string
		Next  string
		Error string
	}{}
	data.baseData = a.base(c, s, "Вход")
	data.CSRF, _ = s.Get("csrf").(string)
	data.Next = sanitizeNext(c.Query("next"), a.prefix)

	if c.R.Method == http.MethodPost {
		if !csrfOK(c, s) {
			data.Error = "Сессия истекла, попробуйте ещё раз"
			return a.render(c, http.StatusUnauthorized, "login.html", data)
		}
		username := strings.TrimSpace(c.Form("username"))
		password := c.Form("password")
		var user AdminUser
		err := a.db.Q(&AdminUser{}).Filter("Username", username).First(&user)
		if err == nil && CheckPassword(password, user.PasswordHash) {
			s.Set("user", user.Username)
			s.Set("uid", user.ID)
			a.store.Save(c.W, s)
			c.Redirect(data.Next)
			return nil
		}
		data.Error = "Неверное имя пользователя или пароль"
		return a.render(c, http.StatusUnauthorized, "login.html", data)
	}
	return a.render(c, http.StatusOK, "login.html", data)
}

func (a *AdminSite) handleLogout(c *Ctx) error {
	s := a.store.Get(c.R)
	if s != nil && csrfOK(c, s) {
		a.store.Destroy(c.W, s)
	}
	c.Redirect(a.prefix + "/login")
	return nil
}

func sanitizeNext(next, prefix string) string {
	if next == "" || !strings.HasPrefix(next, prefix) || strings.Contains(next, "//") {
		return prefix + "/"
	}
	return next
}

func (a *AdminSite) handleIndex(c *Ctx, s *Session) error {
	type modelLink struct {
		Label string
		URL   string
	}
	var links []modelLink
	for _, mi := range a.db.Models() {
		am := a.models[mi.URLName]
		if am == nil {
			continue
		}
		links = append(links, modelLink{Label: am.label(), URL: a.listURL(am.mi.URLName, "")})
	}
	data := struct {
		baseData
		Models []modelLink
	}{}
	data.baseData = a.base(c, s, "Администрирование сайта")
	data.Models = links
	return a.render(c, http.StatusOK, "index.html", data)
}

func (a *AdminSite) listURL(urlName, query string) string {
	u := a.prefix + "/" + urlName
	if query != "" {
		u += query
	}
	return u
}

type listColumn struct {
	Field   string
	Label   string
	SortKey string
	Sorted  string
	SortURL string
}

type listRow struct {
	ID        int64
	ChangeURL string
	DeleteURL string
	Cells     []string
}

type listChoice struct {
	Label  string
	URL    string
	Active bool
}

type listFilter struct {
	Title   string
	Choices []listChoice
}

type pageLink struct {
	Label  string
	URL    string
	Active bool
}

type listData struct {
	baseData
	Label       string
	Singular    string
	AddURL      string
	HasSearch   bool
	SearchQ     string
	ClearURL    string
	Columns     []listColumn
	Rows        []listRow
	Filters     []listFilter
	HasFilters  bool
	Total       int
	Page        int
	Pages       []pageLink
	HasPrev     bool
	PrevURL     string
	HasNext     bool
	NextURL     string
	IsPaginated bool
	Actions     []formOption
	HasActions  bool
}

func (a *AdminSite) handleList(c *Ctx, s *Session) error {
	am := a.model(c.Param("m"))
	if am == nil {
		return a.notFound(c)
	}
	if c.R.Method == http.MethodPost {
		return a.runAction(c, s, am)
	}
	mi := am.mi
	query := c.R.URL.Query()

	q := a.db.Q(mi).Search(am.cfg.SearchFields, query.Get("q"))
	for key, vals := range query {
		if !strings.HasPrefix(key, "f_") || len(vals) == 0 || vals[0] == "" {
			continue
		}
		if f := mi.columnField(strings.TrimPrefix(key, "f_")); f != nil {
			q.Filter(f.Name, coerceFilterValue(f, vals[0]))
		}
	}

	orderFields := am.cfg.Ordering
	if o := query.Get("o"); o != "" {
		name := strings.TrimPrefix(o, "-")
		if f := mi.columnField(name); f != nil && f.Kind != kindM2M {
			orderFields = []string{o}
		}
	}
	q.Order(orderFields...)

	total, err := q.Count()
	if err != nil {
		return err
	}
	perPage := am.cfg.ListPerPage
	if perPage <= 0 {
		perPage = 20
	}
	pageCount := (total + perPage - 1) / perPage
	if pageCount == 0 {
		pageCount = 1
	}
	page, _ := strconv.Atoi(query.Get("p"))
	if page < 1 {
		page = 1
	}
	if page > pageCount {
		page = pageCount
	}
	q.Limit(perPage).Offset((page - 1) * perPage)

	rowsVal := reflect.MakeSlice(reflect.SliceOf(mi.Type), 0, perPage)
	rowsPtr := reflect.New(rowsVal.Type())
	rowsPtr.Elem().Set(rowsVal)
	if err := q.All(rowsPtr.Interface()); err != nil {
		return err
	}
	rows := rowsPtr.Elem()

	listFields := am.cfg.ListDisplay
	if len(listFields) == 0 {
		for _, f := range mi.Fields {
			if !f.Primary && f.Kind != kindM2M {
				listFields = append(listFields, f.Name)
			}
		}
	}

	fkMaps := a.fkReprMaps(mi, listFields)

	data := listData{}
	data.baseData = a.base(c, s, "Выберите объект для изменения")
	data.Label = am.label()
	data.Singular = am.singular()
	data.AddURL = a.prefix + "/" + mi.URLName + "/add"
	data.SearchQ = query.Get("q")
	data.HasSearch = len(am.cfg.SearchFields) > 0
	data.Total = total
	data.Actions = append(data.Actions,
		formOption{Value: "", Label: "---------"},
		formOption{Value: "delete_selected", Label: fmt.Sprintf("Удалить выбранные %s", data.Label)},
	)
	for _, act := range am.cfg.Actions {
		if act.Name != "" && act.Handler != nil {
			data.Actions = append(data.Actions, formOption{Value: act.Name, Label: act.Label})
		}
	}
	data.HasActions = len(data.Actions) > 2

	currentOrder := ""
	if o := query.Get("o"); o != "" {
		if f := mi.columnField(strings.TrimPrefix(o, "-")); f != nil {
			currentOrder = o
		}
	}

	for _, name := range listFields {
		col := listColumn{Field: name, Label: humanize(name)}
		if name == "__str__" {
			col.Label = data.Singular
		}
		if f := mi.Field(name); f != nil && f.Kind != kindM2M {
			col.SortKey = f.Column
			dir := ""
			if currentOrder == f.Column {
				dir = "-"
				col.Sorted = "asc"
			} else if currentOrder == "-"+f.Column {
				col.Sorted = "desc"
			}
			col.SortURL = a.withQuery(query, map[string]string{"o": dir + f.Column, "p": ""})
		}
		data.Columns = append(data.Columns, col)
	}

	for i := 0; i < rows.Len(); i++ {
		rv := rows.Index(i)
		id := rv.FieldByIndex(mi.PK.index).Int()
		row := listRow{
			ID:        id,
			ChangeURL: fmt.Sprintf("%s/%s/%d", a.prefix, mi.URLName, id),
			DeleteURL: fmt.Sprintf("%s/%s/%d/delete", a.prefix, mi.URLName, id),
		}
		for _, name := range listFields {
			if name == "__str__" {
				row.Cells = append(row.Cells, mi.Repr(rv))
				continue
			}
			f := mi.Field(name)
			if f == nil {
				row.Cells = append(row.Cells, "")
				continue
			}
			row.Cells = append(row.Cells, a.displayValue(mi, f, rv.FieldByIndex(f.index), fkMaps))
		}
		data.Rows = append(data.Rows, row)
	}

	filters, err := a.buildFilters(am, query)
	if err != nil {
		return err
	}
	data.Filters = filters
	data.HasFilters = len(filters) > 0
	data.IsPaginated = pageCount > 1
	data.Page = page
	for n := 1; n <= pageCount; n++ {
		data.Pages = append(data.Pages, pageLink{
			Label:  strconv.Itoa(n),
			URL:    a.withQuery(query, map[string]string{"p": strconv.Itoa(n)}),
			Active: n == page,
		})
	}
	if page > 1 {
		data.HasPrev = true
		data.PrevURL = a.withQuery(query, map[string]string{"p": strconv.Itoa(page - 1)})
	}
	if page < pageCount {
		data.HasNext = true
		data.NextURL = a.withQuery(query, map[string]string{"p": strconv.Itoa(page + 1)})
	}
	data.ClearURL = a.listURL(mi.URLName, "")

	return a.render(c, http.StatusOK, "list.html", data)
}

func (a *AdminSite) runAction(c *Ctx, s *Session, am *adminModel) error {
	if !csrfOK(c, s) {
		return c.String(http.StatusBadRequest, "CSRF verification failed")
	}
	action := c.Form("action")
	var ids []int64
	for _, v := range c.R.Form["_selected"] {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			ids = append(ids, n)
		}
	}
	if action == "" || len(ids) == 0 {
		a.flash(c, s, "Ничего не выбрано.")
		c.Redirect(a.listURL(am.mi.URLName, ""))
		return nil
	}
	if action == "delete_selected" {
		for _, id := range ids {
			if err := a.db.DeleteByID(am.mi, id); err != nil {
				return err
			}
		}
		a.flash(c, s, fmt.Sprintf("Удалено объектов: %d.", len(ids)))
		c.Redirect(a.listURL(am.mi.URLName, ""))
		return nil
	}
	for _, act := range am.cfg.Actions {
		if act.Name != action {
			continue
		}
		if err := act.Handler(c, a.db, ids); err != nil {
			return err
		}
		a.flash(c, s, fmt.Sprintf("%s: объектов: %d.", act.Label, len(ids)))
		c.Redirect(a.listURL(am.mi.URLName, ""))
		return nil
	}
	return a.notFound(c)
}

func (a *AdminSite) validate(am *adminModel, rv reflect.Value) map[string]string {
	if len(am.cfg.Validators) == 0 {
		return nil
	}
	errs := make(map[string]string)
	for name, fn := range am.cfg.Validators {
		f := am.mi.Field(name)
		if f == nil || fn == nil {
			continue
		}
		if msg := fn(rv.FieldByIndex(f.index).Interface()); msg != "" {
			errs[name] = msg
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return errs
}

func (a *AdminSite) fkReprMaps(mi *ModelInfo, listFields []string) map[string]map[int64]string {
	out := make(map[string]map[int64]string)
	for _, name := range listFields {
		f := mi.Field(name)
		if f == nil || f.Kind != kindInt || f.RelModel == "" {
			continue
		}
		rel := a.db.Model(f.RelModel)
		if rel == nil {
			continue
		}
		out[f.Name] = a.reprMap(rel)
	}
	return out
}

func (a *AdminSite) reprMap(rel *ModelInfo) map[int64]string {
	m := make(map[int64]string)
	slice := reflect.MakeSlice(reflect.SliceOf(rel.Type), 0, 0)
	ptr := reflect.New(slice.Type())
	ptr.Elem().Set(slice)
	if err := a.db.Q(rel).All(ptr.Interface()); err != nil {
		return m
	}
	s := ptr.Elem()
	for i := 0; i < s.Len(); i++ {
		rv := s.Index(i)
		id := rv.FieldByIndex(rel.PK.index).Int()
		m[id] = rel.Repr(rv)
	}
	return m
}

func (a *AdminSite) displayValue(mi *ModelInfo, f *Field, fv reflect.Value, fkMaps map[string]map[int64]string) string {
	if fv.Kind() == reflect.Ptr {
		if fv.IsNil() {
			return "—"
		}
		fv = fv.Elem()
	}
	switch f.Kind {
	case kindInt:
		if f.RelModel != "" {
			if m := fkMaps[f.Name]; m != nil {
				if s, ok := m[fv.Int()]; ok {
					return s
				}
				if fv.Int() == 0 {
					return "—"
				}
			}
		}
		return strconv.FormatInt(fv.Int(), 10)
	case kindFloat:
		return strconv.FormatFloat(fv.Float(), 'f', -1, 64)
	case kindBool:
		if fv.Bool() {
			return "Да"
		}
		return "Нет"
	case kindString:
		return fv.String()
	case kindTime:
		t := fv.Interface().(time.Time)
		if t.IsZero() {
			return "—"
		}
		return t.Format("02.01.2006 15:04")
	}
	return ""
}

func (a *AdminSite) buildFilters(am *adminModel, query url.Values) ([]listFilter, error) {
	var filters []listFilter
	mi := am.mi
	for _, name := range am.cfg.ListFilter {
		f := mi.Field(name)
		if f == nil || f.Kind == kindM2M || f.Primary {
			continue
		}
		lf := listFilter{Title: humanize(f.Name)}
		active := query.Get("f_" + f.Column)
		addChoice := func(label, value string) {
			lf.Choices = append(lf.Choices, listChoice{
				Label:  label,
				URL:    a.withQuery(query, map[string]string{"f_" + f.Column: value, "p": ""}),
				Active: active == value,
			})
		}
		switch {
		case f.Kind == kindInt && f.RelModel != "":
			rel := a.db.Model(f.RelModel)
			if rel == nil {
				continue
			}
			m := a.reprMap(rel)
			ids := make([]int64, 0, len(m))
			for id := range m {
				ids = append(ids, id)
			}
			for i := 0; i < len(ids); i++ {
				for j := i + 1; j < len(ids); j++ {
					if m[ids[j]] < m[ids[i]] {
						ids[i], ids[j] = ids[j], ids[i]
					}
				}
			}
			for _, id := range ids {
				addChoice(m[id], strconv.FormatInt(id, 10))
			}
		case f.Kind == kindBool:
			addChoice("Да", "1")
			addChoice("Нет", "0")
		default:
			rows, err := a.db.DB.Query(
				fmt.Sprintf("SELECT DISTINCT %s FROM %s WHERE %s IS NOT NULL ORDER BY %s LIMIT 200",
					f.Column, mi.Table, f.Column, f.Column),
			)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			for rows.Next() {
				var v any
				if err := rows.Scan(&v); err != nil {
					return nil, err
				}
				s := fmt.Sprint(v)
				addChoice(s, s)
			}
		}
		if len(lf.Choices) > 0 {
			filters = append(filters, lf)
		}
	}
	return filters, nil
}

func coerceFilterValue(f *Field, s string) any {
	switch f.Kind {
	case kindInt:
		n, _ := strconv.ParseInt(s, 10, 64)
		return n
	case kindFloat:
		n, _ := strconv.ParseFloat(s, 64)
		return n
	case kindBool:
		return s == "1" || s == "true"
	default:
		return s
	}
}

func (a *AdminSite) withQuery(base url.Values, set map[string]string) string {
	v := url.Values{}
	for k, vals := range base {
		if len(vals) > 0 {
			v.Set(k, vals[0])
		}
	}
	for k, val := range set {
		if val == "" {
			v.Del(k)
		} else {
			v.Set(k, val)
		}
	}
	if len(v) == 0 {
		return ""
	}
	return "?" + v.Encode()
}

type formOption struct {
	Value string
	Label string
}

type formField struct {
	Label    string
	Name     string
	Widget   string
	Type     string
	Value    string
	Checked  bool
	Options  []formOption
	Selected map[string]bool
	ReadOnly bool
	Required bool
	Error    string
}

type formData struct {
	baseData
	Label     string
	Singular  string
	ListURL   string
	DeleteURL string
	IsEdit    bool
	Fields    []formField
	Errors    bool
	CSRF      string
	ID        int64
}

func (a *AdminSite) editableFields(am *adminModel) []*Field {
	mi := am.mi
	var out []*Field
	if len(am.cfg.Fields) > 0 {
		for _, name := range am.cfg.Fields {
			if f := mi.Field(name); f != nil && !f.Primary && !contains(am.cfg.Exclude, name) {
				out = append(out, f)
			}
		}
		return out
	}
	for _, f := range mi.Fields {
		if f.Primary || contains(am.cfg.Exclude, f.Name) {
			continue
		}
		out = append(out, f)
	}
	return out
}

func (a *AdminSite) handleForm(c *Ctx, s *Session, add bool) error {
	am := a.model(c.Param("m"))
	if am == nil {
		return a.notFound(c)
	}
	mi := am.mi
	id := int64(0)
	if !add {
		id, _ = strconv.ParseInt(c.Param("id"), 10, 64)
		if id <= 0 {
			return a.notFound(c)
		}
	}

	data := formData{}
	data.baseData = a.base(c, s, "")
	data.Label = am.label()
	data.Singular = am.singular()
	data.ListURL = a.listURL(mi.URLName, "")
	data.CSRF, _ = s.Get("csrf").(string)
	data.IsEdit = !add
	data.ID = id

	newObj := reflect.New(mi.Type)
	if !add {
		if err := a.db.FindByID(newObj.Interface(), id); err != nil {
			return a.notFound(c)
		}
	}

	if c.R.Method == http.MethodPost {
		if !csrfOK(c, s) {
			return c.String(http.StatusBadRequest, "CSRF verification failed")
		}
		rv := reflect.New(mi.Type).Elem()
		m2mSets, errs := a.parseForm(c, am, rv)
		if len(errs) == 0 {
			errs = a.validate(am, rv)
		}
		if len(errs) == 0 {
			if !add {
				rv.FieldByIndex(mi.PK.index).SetInt(id)
			}
			var saveErr error
			if am.cfg.BeforeSave != nil {
				saveErr = am.cfg.BeforeSave(c, rv.Addr().Interface())
			}
			if saveErr == nil {
				saveErr = a.db.Save(rv.Addr().Interface())
				if saveErr != nil {
					if field, msg := uniqueViolation(mi, saveErr); field != "" {
						errs[field] = msg
						saveErr = nil
					}
				}
			}
			if saveErr == nil {
				for name, ids := range m2mSets {
					if err := a.db.M2MSet(rv.Addr().Interface(), name, ids); err != nil {
						saveErr = err
					}
				}
			}
			if saveErr == nil {
				savedID := rv.FieldByIndex(mi.PK.index).Int()
				if add {
					a.flash(c, s, fmt.Sprintf("%s «%s» добавлен.", data.Singular, mi.Repr(rv)))
				} else {
					a.flash(c, s, fmt.Sprintf("%s «%s» изменён.", data.Singular, mi.Repr(rv)))
				}
				if c.Form("_continue") != "" {
					c.Redirect(fmt.Sprintf("%s/%s/%d", a.prefix, mi.URLName, savedID))
				} else {
					c.Redirect(a.listURL(mi.URLName, ""))
				}
				return nil
			}
			errs["__all__"] = saveErr.Error()
		}
		data.Errors = true
		data.Title = fmt.Sprintf("Изменить %s", data.Singular)
		data.Fields = a.buildFormFields(c, am, rv, c.R.Form, errs)
		return a.render(c, http.StatusBadRequest, "form.html", data)
	}

	data.Title = fmt.Sprintf("Добавить %s", data.Singular)
	if !add {
		data.Title = fmt.Sprintf("Изменить %s", data.Singular)
		data.DeleteURL = fmt.Sprintf("%s/%s/%d/delete", a.prefix, mi.URLName, id)
	}
	current := reflect.Indirect(newObj)
	data.Fields = a.buildFormFields(c, am, current, nil, nil)
	return a.render(c, http.StatusOK, "form.html", data)
}

func (a *AdminSite) parseForm(c *Ctx, am *adminModel, rv reflect.Value) (map[string][]int64, map[string]string) {
	errs := make(map[string]string)
	m2mSets := make(map[string][]int64)
	for _, f := range a.editableFields(am) {
		if contains(am.cfg.ReadOnlyFields, f.Name) {
			continue
		}
		if f.Kind == kindM2M {
			var ids []int64
			for _, v := range c.R.Form[f.Name] {
				if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
					ids = append(ids, n)
				}
			}
			m2mSets[f.Name] = ids
			continue
		}
		fv := rv.FieldByIndex(f.index)
		raw := c.Form(f.Name)
		switch f.Kind {
		case kindBool:
			fv.SetBool(raw == "on" || raw == "true" || raw == "1")
		case kindInt:
			if raw == "" {
				if f.RelModel != "" && !f.Nullable {
					errs[f.Name] = "Выберите значение"
					continue
				}
				fv.SetInt(0)
				continue
			}
			n, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				errs[f.Name] = "Введите целое число"
				continue
			}
			fv.SetInt(n)
		case kindFloat:
			if raw == "" {
				fv.SetFloat(0)
				continue
			}
			n, err := strconv.ParseFloat(raw, 64)
			if err != nil {
				errs[f.Name] = "Введите число"
				continue
			}
			fv.SetFloat(n)
		case kindString:
			if raw == "" && !f.Nullable && !f.Blank {
				errs[f.Name] = "Обязательное поле"
				continue
			}
			fv.SetString(raw)
		case kindTime:
			if raw == "" {
				continue
			}
			var t time.Time
			var ok bool
			for _, layout := range []string{"2006-01-02T15:04", "2006-01-02T15:04:05", time.RFC3339} {
				if parsed, err := time.Parse(layout, raw); err == nil {
					t, ok = parsed, true
					break
				}
			}
			if !ok {
				errs[f.Name] = "Введите дату и время"
				continue
			}
			fv.Set(reflect.ValueOf(t))
		}
	}
	return m2mSets, errs
}

func (a *AdminSite) buildFormFields(c *Ctx, am *adminModel, rv reflect.Value, posted url.Values, errs map[string]string) []formField {
	mi := am.mi
	var out []formField
	for _, f := range a.editableFields(am) {
		ff := formField{
			Label:    humanize(f.Name),
			Name:     f.Name,
			ReadOnly: contains(am.cfg.ReadOnlyFields, f.Name),
			Required: !f.Nullable && !f.Blank && f.Kind != kindBool && f.Kind != kindM2M && f.Kind != kindTime,
		}
		if f.Kind == kindM2M {
			ff.Label = plural(f.RelModel)
		}
		if errs != nil {
			if e, ok := errs[f.Name]; ok {
				ff.Error = e
			}
		}
		switch f.Kind {
		case kindBool:
			ff.Widget = "checkbox"
			if posted != nil {
				ff.Checked = posted.Get(f.Name) == "on"
			} else {
				ff.Checked = rv.FieldByIndex(f.index).Bool()
			}
		case kindInt:
			if f.RelModel != "" {
				ff.Widget = "select"
				rel := a.db.Model(f.RelModel)
				if rel != nil {
					for _, opt := range a.relatedOptions(rel) {
						ff.Options = append(ff.Options, opt)
					}
				}
				ff.Selected = make(map[string]bool)
				cur := ""
				if posted != nil {
					cur = posted.Get(f.Name)
				} else {
					cur = strconv.FormatInt(rv.FieldByIndex(f.index).Int(), 10)
				}
				if cur != "0" {
					ff.Selected[cur] = true
				}
			} else {
				ff.Widget = "input"
				ff.Type = "number"
				if posted != nil {
					ff.Value = posted.Get(f.Name)
				} else {
					ff.Value = strconv.FormatInt(rv.FieldByIndex(f.index).Int(), 10)
				}
			}
		case kindFloat:
			ff.Widget = "input"
			ff.Type = "number"
			if posted != nil {
				ff.Value = posted.Get(f.Name)
			} else {
				ff.Value = strconv.FormatFloat(rv.FieldByIndex(f.index).Float(), 'f', -1, 64)
			}
		case kindString:
			if f.Widget == "textarea" {
				ff.Widget = "textarea"
			} else {
				ff.Widget = "input"
				ff.Type = "text"
			}
			if posted != nil {
				ff.Value = posted.Get(f.Name)
			} else {
				ff.Value = rv.FieldByIndex(f.index).String()
			}
		case kindTime:
			ff.Widget = "input"
			ff.Type = "datetime-local"
			if posted != nil {
				ff.Value = posted.Get(f.Name)
			} else {
				t := rv.FieldByIndex(f.index)
				if t.Kind() == reflect.Ptr {
					if !t.IsNil() {
						ff.Value = t.Elem().Interface().(time.Time).Format("2006-01-02T15:04")
					}
				} else if tt := t.Interface().(time.Time); !tt.IsZero() {
					ff.Value = tt.Format("2006-01-02T15:04")
				}
			}
		case kindM2M:
			ff.Widget = "multiselect"
			rel := a.db.Model(f.RelModel)
			if rel != nil {
				for _, opt := range a.relatedOptions(rel) {
					ff.Options = append(ff.Options, opt)
				}
			}
			ff.Selected = make(map[string]bool)
			if posted != nil {
				for _, v := range posted[f.Name] {
					ff.Selected[v] = true
				}
			} else if rv.FieldByIndex(mi.PK.index).Int() > 0 {
				if ids, err := a.db.M2MIDs(rv.Addr().Interface(), f.Name); err == nil {
					for _, id := range ids {
						ff.Selected[strconv.FormatInt(id, 10)] = true
					}
				}
			}
		}
		out = append(out, ff)
	}
	return out
}

func (a *AdminSite) relatedOptions(rel *ModelInfo) []formOption {
	m := a.reprMap(rel)
	ids := make([]int64, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			if m[ids[j]] < m[ids[i]] {
				ids[i], ids[j] = ids[j], ids[i]
			}
		}
	}
	out := make([]formOption, 0, len(ids))
	for _, id := range ids {
		out = append(out, formOption{Value: strconv.FormatInt(id, 10), Label: m[id]})
	}
	return out
}

func (a *AdminSite) handleDelete(c *Ctx, s *Session) error {
	am := a.model(c.Param("m"))
	if am == nil {
		return a.notFound(c)
	}
	mi := am.mi
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if id <= 0 {
		return a.notFound(c)
	}
	obj := reflect.New(mi.Type)
	if err := a.db.FindByID(obj.Interface(), id); err != nil {
		return a.notFound(c)
	}
	if c.R.Method == http.MethodPost {
		if !csrfOK(c, s) {
			return c.String(http.StatusBadRequest, "CSRF verification failed")
		}
		if err := a.db.DeleteByID(mi, id); err != nil {
			return err
		}
		a.flash(c, s, fmt.Sprintf("%s «%s» удалён.", mi.Name, mi.Repr(obj.Elem())))
		c.Redirect(a.listURL(mi.URLName, ""))
		return nil
	}
	data := struct {
		baseData
		Label      string
		Singular   string
		ObjectRepr string
		ListURL    string
		DeleteURL  string
		CSRF       string
	}{}
	data.baseData = a.base(c, s, "Удаление")
	data.Label = am.label()
	data.Singular = am.singular()
	data.ObjectRepr = mi.Repr(obj.Elem())
	data.ListURL = a.listURL(mi.URLName, "")
	data.DeleteURL = fmt.Sprintf("%s/%s/%d/delete", a.prefix, mi.URLName, id)
	data.CSRF, _ = s.Get("csrf").(string)
	return a.render(c, http.StatusOK, "delete.html", data)
}

func uniqueViolation(mi *ModelInfo, err error) (string, string) {
	if err == nil || !strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return "", ""
	}
	for _, f := range mi.Fields {
		if strings.Contains(err.Error(), mi.Table+"."+f.Column) {
			return f.Name, "Такое значение уже существует"
		}
	}
	return "", ""
}

func humanize(name string) string {
	name = strings.TrimSuffix(name, "ID")
	s := snake(name)
	words := strings.Split(s, "_")
	for i, w := range words {
		if w == "" {
			continue
		}
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func randomToken() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		panic("gd: crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(buf)
}

func (mi *ModelInfo) columnField(column string) *Field {
	for _, f := range mi.Fields {
		if f.Column == column {
			return f
		}
	}
	return nil
}
