package gd

import (
	"fmt"
	"strconv"
	"strings"
)

type Dialect struct {
	Name string
}

func DetectDialect(driver string) *Dialect {
	switch {
	case strings.Contains(driver, "postgres"), strings.Contains(driver, "pgx"):
		return &Dialect{Name: "postgres"}
	case strings.Contains(driver, "mysql"):
		return &Dialect{Name: "mysql"}
	default:
		return &Dialect{Name: "sqlite"}
	}
}

func (d *Dialect) SQLType(f *Field) string {
	switch d.Name {
	case "postgres":
		switch f.Kind {
		case kindInt, kindBool:
			return "BIGINT"
		case kindFloat:
			return "DOUBLE PRECISION"
		case kindTime:
			return "TIMESTAMP"
		}
	case "mysql":
		switch f.Kind {
		case kindInt:
			return "BIGINT"
		case kindBool:
			return "BOOLEAN"
		case kindFloat:
			return "DOUBLE"
		case kindTime:
			return "DATETIME"
		}
	}
	switch f.Kind {
	case kindInt, kindBool:
		return "INTEGER"
	case kindFloat:
		return "REAL"
	case kindTime:
		return "TIMESTAMP"
	default:
		if f.Kind == kindString && f.Len > 0 {
			return fmt.Sprintf("VARCHAR(%d)", f.Len)
		}
		return "TEXT"
	}
}

func (d *Dialect) PKColumnDef(f *Field) string {
	switch d.Name {
	case "postgres":
		return fmt.Sprintf("%s BIGSERIAL PRIMARY KEY", f.Column)
	case "mysql":
		return fmt.Sprintf("%s BIGINT AUTO_INCREMENT PRIMARY KEY", f.Column)
	default:
		return fmt.Sprintf("%s INTEGER PRIMARY KEY AUTOINCREMENT", f.Column)
	}
}

func (d *Dialect) Rebind(q string) string {
	if d.Name != "postgres" {
		return q
	}
	var b strings.Builder
	n := 0
	for _, r := range q {
		if r == '?' {
			n++
			b.WriteString("$" + strconv.Itoa(n))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (d *Dialect) TableColumnsQuery() string {
	switch d.Name {
	case "postgres", "mysql":
		return "SELECT column_name FROM information_schema.columns WHERE table_name = ?"
	default:
		return "SELECT name FROM pragma_table_info(?)"
	}
}

func (d *Dialect) NeedsReturning() bool {
	return d.Name == "postgres"
}

func (d *Dialect) InsertIgnore(table, columns string) string {
	switch d.Name {
	case "postgres":
		return fmt.Sprintf("INSERT INTO %s (%s) VALUES (?, ?) ON CONFLICT DO NOTHING", table, columns)
	case "mysql":
		return fmt.Sprintf("INSERT IGNORE INTO %s (%s) VALUES (?, ?)", table, columns)
	default:
		return fmt.Sprintf("INSERT OR IGNORE INTO %s (%s) VALUES (?, ?)", table, columns)
	}
}
