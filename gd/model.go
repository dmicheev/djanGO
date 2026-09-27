package gd

import (
	"fmt"
	"reflect"
	"strings"
	"time"
	"unicode"
)

const tagGD = "gd"

type fieldKind int

const (
	kindInt fieldKind = iota
	kindFloat
	kindBool
	kindString
	kindTime
	kindM2M
)

type Field struct {
	Name     string
	Column   string
	Kind     fieldKind
	Primary  bool
	Unique   bool
	Index    bool
	Nullable bool
	Blank    bool
	Len      int
	Widget   string
	RelModel string
	index    []int
}

type ModelInfo struct {
	Type    reflect.Type
	Name    string
	URLName string
	Table   string
	Fields  []*Field
	PK      *Field

	byName map[string]*Field
}

func (m *ModelInfo) Field(name string) *Field {
	return m.byName[name]
}

func (m *ModelInfo) Repr(v reflect.Value) string {
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return ""
		}
		v = v.Elem()
	}
	if s, ok := v.Addr().Interface().(fmt.Stringer); ok {
		return s.String()
	}
	for _, candidate := range []string{"Title", "Name", "Username", "Email", "Slug"} {
		if f := m.Field(candidate); f != nil && f.Kind == kindString {
			if s := v.Field(f.index[0]).String(); s != "" {
				return s
			}
		}
	}
	if m.PK != nil {
		return fmt.Sprintf("%s#%d", m.Name, v.FieldByIndex(m.PK.index).Int())
	}
	return m.Name
}

func parseModel(v any) (*ModelInfo, error) {
	t := reflect.Indirect(reflect.ValueOf(v)).Type()
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("gd: model must be a struct, got %s", t)
	}
	if t.Name() == "" {
		return nil, fmt.Errorf("gd: anonymous structs are not supported")
	}
	mi := &ModelInfo{
		Type:   t,
		Name:   t.Name(),
		byName: make(map[string]*Field),
	}
	mi.URLName = snake(mi.Name)
	mi.Table = plural(mi.URLName)
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if !sf.IsExported() {
			continue
		}
		tag := sf.Tag.Get(tagGD)
		if tag == "-" {
			continue
		}
		f, err := parseField(sf, tag)
		if err != nil {
			return nil, fmt.Errorf("gd: %s.%s: %w", mi.Name, sf.Name, err)
		}
		if f == nil {
			continue
		}
		f.index = []int{i}
		mi.Fields = append(mi.Fields, f)
		mi.byName[f.Name] = f
		if f.Primary {
			if mi.PK != nil {
				return nil, fmt.Errorf("gd: %s: multiple primary keys", mi.Name)
			}
			mi.PK = f
		}
	}
	if mi.PK == nil {
		return nil, fmt.Errorf("gd: %s: no primary key (add ID int64 `gd:\"primary\"`)", mi.Name)
	}
	return mi, nil
}

func parseField(sf reflect.StructField, tag string) (*Field, error) {
	f := &Field{Name: sf.Name, Column: snake(sf.Name)}
	ft := sf.Type
	if ft.Kind() == reflect.Ptr {
		f.Nullable = true
		ft = ft.Elem()
	}
	switch ft.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		f.Kind = kindInt
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		f.Kind = kindInt
	case reflect.Float32, reflect.Float64:
		f.Kind = kindFloat
	case reflect.Bool:
		f.Kind = kindBool
	case reflect.String:
		f.Kind = kindString
	case reflect.Slice:
		if ft.Elem().Kind() == reflect.Int64 {
			f.Kind = kindM2M
		} else {
			return nil, nil
		}
	case reflect.Struct:
		if ft == reflect.TypeOf(time.Time{}) {
			f.Kind = kindTime
		} else {
			return nil, nil
		}
	default:
		return nil, nil
	}
	for _, opt := range strings.Split(tag, ";") {
		if opt == "" {
			continue
		}
		key, val, _ := strings.Cut(opt, ":")
		switch key {
		case "primary":
			f.Primary = true
		case "unique":
			f.Unique = true
		case "index":
			f.Index = true
		case "null":
			f.Nullable = true
		case "blank":
			f.Blank = true
		case "column":
			if val != "" {
				f.Column = val
			}
		case "len":
			fmt.Sscanf(val, "%d", &f.Len)
		case "widget":
			f.Widget = val
		case "fk":
			f.RelModel = val
		case "m2m":
			f.RelModel = val
			if f.Kind != kindM2M {
				return nil, fmt.Errorf("m2m requires []int64 field")
			}
		}
	}
	if f.Kind == kindM2M && f.RelModel == "" {
		return nil, fmt.Errorf("m2m field requires related model: `gd:\"m2m:Tag\"`")
	}
	if f.Kind != kindM2M && f.RelModel != "" && f.Kind != kindInt {
		return nil, fmt.Errorf("fk requires int64 field")
	}
	if f.Name == "ID" && f.Kind == kindInt && tag == "" {
		f.Primary = true
	}
	return f, nil
}

var acronyms = []string{"IDs", "ID", "UUID", "URL", "API", "HTTP", "JSON", "HTML", "CSS"}

func snake(s string) string {
	for _, a := range acronyms {
		s = strings.ReplaceAll(s, a, "|"+strings.ToLower(a)+"|")
	}
	var b strings.Builder
	runes := []rune(s)
	for i, r := range runes {
		switch {
		case r == '|':
			if b.Len() > 0 && !strings.HasSuffix(b.String(), "_") {
				b.WriteRune('_')
			}
		case unicode.IsUpper(r):
			var prev rune
			if i > 0 {
				prev = runes[i-1]
			}
			prevBoundary := unicode.IsLower(prev) || unicode.IsDigit(prev) || prev == '|'
			nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if i > 0 && (prevBoundary || (unicode.IsUpper(prev) && nextLower)) {
				b.WriteRune('_')
			}
			b.WriteRune(unicode.ToLower(r))
		default:
			b.WriteRune(r)
		}
	}
	out := b.String()
	for strings.Contains(out, "__") {
		out = strings.ReplaceAll(out, "__", "_")
	}
	return strings.Trim(out, "_")
}

func plural(s string) string {
	switch {
	case strings.HasSuffix(s, "s"), strings.HasSuffix(s, "x"), strings.HasSuffix(s, "z"),
		strings.HasSuffix(s, "ch"), strings.HasSuffix(s, "sh"):
		return s + "es"
	case strings.HasSuffix(s, "y"):
		return s[:len(s)-1] + "ies"
	default:
		return s + "s"
	}
}

func singular(s string) string {
	switch {
	case strings.HasSuffix(s, "ies"):
		return s[:len(s)-3] + "y"
	case strings.HasSuffix(s, "ses"), strings.HasSuffix(s, "xes"), strings.HasSuffix(s, "zes"),
		strings.HasSuffix(s, "ches"), strings.HasSuffix(s, "shes"):
		return s[:len(s)-2]
	case strings.HasSuffix(s, "es"):
		return s[:len(s)-2]
	case strings.HasSuffix(s, "s"):
		return s[:len(s)-1]
	default:
		return s
	}
}
