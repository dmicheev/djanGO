package gd

import (
	"reflect"
	"testing"
	"time"
)

func TestSnake(t *testing.T) {
	cases := map[string]string{
		"ID":          "id",
		"Name":        "name",
		"AuthorID":    "author_id",
		"TagIDs":      "tag_ids",
		"CategoryIDs": "category_ids",
		"URLName":     "url_name",
		"APIKey":      "api_key",
		"CreatedAt":   "created_at",
		"IsSuperuser": "is_superuser",
		"MyURLField":  "my_url_field",
	}
	for in, want := range cases {
		if got := snake(in); got != want {
			t.Errorf("snake(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPluralSingular(t *testing.T) {
	cases := map[string]string{
		"book":       "books",
		"author":     "authors",
		"category":   "categories",
		"box":        "boxes",
		"tag":        "tags",
		"admin_user": "admin_users",
	}
	for sing, plur := range cases {
		if got := plural(sing); got != plur {
			t.Errorf("plural(%q) = %q, want %q", sing, got, plur)
		}
		if got := singular(plur); got != sing {
			t.Errorf("singular(%q) = %q, want %q", plur, got, sing)
		}
	}
}

func TestParseModel(t *testing.T) {
	type Post struct {
		ID       int64   `gd:"primary"`
		Title    string  `gd:"len:200;unique"`
		AuthorID int64   `gd:"fk:Author"`
		TagIDs   []int64 `gd:"m2m:Tag"`
		Hidden   string  `gd:"-"`
		Score    float64
		Draft    bool
		DueAt    *time.Time `gd:"null"`
	}
	mi, err := parseModel(&Post{})
	if err != nil {
		t.Fatal(err)
	}
	if mi.Table != "posts" || mi.URLName != "post" {
		t.Fatalf("naming: %s %s", mi.Table, mi.URLName)
	}
	if mi.PK == nil || mi.PK.Name != "ID" {
		t.Fatal("PK not detected")
	}
	if f := mi.Field("Title"); f == nil || f.Len != 200 || !f.Unique {
		t.Fatalf("Title tags not parsed: %+v", f)
	}
	if f := mi.Field("AuthorID"); f == nil || f.RelModel != "Author" || f.Column != "author_id" {
		t.Fatalf("fk not parsed: %+v", f)
	}
	if f := mi.Field("TagIDs"); f == nil || f.Kind != kindM2M || f.Column != "tag_ids" {
		t.Fatalf("m2m not parsed: %+v", f)
	}
	if mi.Field("Hidden") != nil {
		t.Fatal("Hidden should be skipped")
	}
	if f := mi.Field("DueAt"); f == nil || !f.Nullable {
		t.Fatalf("nullable pointer field: %+v", f)
	}
	wantKinds := map[string]fieldKind{"Score": kindFloat, "Draft": kindBool, "DueAt": kindTime}
	for name, kind := range wantKinds {
		if f := mi.Field(name); f == nil || f.Kind != kind {
			t.Fatalf("%s kind = %v, want %v", name, f, kind)
		}
	}
	if !reflect.DeepEqual(mi.Field("Draft").Column, "draft") {
		t.Fatal("unexpected column")
	}
}

func TestParseModelErrors(t *testing.T) {
	type NoPK struct {
		Name string
	}
	if _, err := parseModel(&NoPK{}); err == nil {
		t.Fatal("expected error for missing PK")
	}
	type BadM2M struct {
		ID   int64   `gd:"primary"`
		Tags []int64 `gd:"m2m:"`
	}
	if _, err := parseModel(&BadM2M{}); err == nil {
		t.Fatal("expected error for m2m without model")
	}
}
