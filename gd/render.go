package gd

import (
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type Templates struct {
	set   *template.Template
	names map[string]bool
}

func LoadTemplates(dir string) (*Templates, error) {
	t := &Templates{set: template.New("gd"), names: make(map[string]bool)}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return t, nil
		}
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".html") {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	if len(files) == 0 {
		return t, nil
	}
	t.set, err = t.set.ParseFiles(files...)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		t.names[filepath.Base(f)] = true
	}
	return t, nil
}

func (t *Templates) Has(name string) bool {
	return t.names[name]
}

func (a *App) Render(w http.ResponseWriter, status int, name string, data any) error {
	if a.tmpl == nil || !a.tmpl.Has(name) {
		http.Error(w, "template not found: "+name, http.StatusInternalServerError)
		return nil
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	return a.tmpl.set.ExecuteTemplate(w, name, data)
}
