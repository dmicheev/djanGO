package gd

import (
	"database/sql"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

type DB struct {
	*sql.DB
	driver  string
	dialect *Dialect
	models  map[string]*ModelInfo
	byType  map[reflect.Type]*ModelInfo
	byTable map[string]*ModelInfo
}

func OpenDB(driver, dsn string) (*DB, error) {
	sdb, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, err
	}
	dialect := DetectDialect(driver)
	if dialect.Name == "sqlite" {
		for _, pragma := range []string{"PRAGMA foreign_keys=ON", "PRAGMA busy_timeout=5000"} {
			if _, err := sdb.Exec(pragma); err != nil {
				sdb.Close()
				return nil, err
			}
		}
	}
	if err := sdb.Ping(); err != nil {
		sdb.Close()
		return nil, err
	}
	return &DB{
		DB:      sdb,
		driver:  driver,
		dialect: dialect,
		models:  make(map[string]*ModelInfo),
		byType:  make(map[reflect.Type]*ModelInfo),
		byTable: make(map[string]*ModelInfo),
	}, nil
}

func OpenDatabase(cfg Database) (*DB, error) {
	driver := cfg.Driver
	if driver == "" {
		driver = "sqlite"
	}
	return OpenDB(driver, cfg.DSN)
}

func (db *DB) Register(models ...any) error {
	var infos []*ModelInfo
	for _, m := range models {
		mi, err := parseModel(m)
		if err != nil {
			return err
		}
		if _, dup := db.byType[mi.Type]; dup {
			continue
		}
		db.models[mi.Name] = mi
		db.byType[mi.Type] = mi
		db.byTable[mi.Table] = mi
		infos = append(infos, mi)
	}
	ordered, err := topoSort(db.models, infos)
	if err != nil {
		return err
	}
	for _, mi := range ordered {
		if err := db.migrateTable(mi); err != nil {
			return err
		}
	}
	for _, mi := range ordered {
		for _, f := range mi.Fields {
			if f.Kind != kindM2M {
				continue
			}
			if err := db.migrateJunction(mi, f); err != nil {
				return err
			}
		}
	}
	return nil
}

func (db *DB) Model(name string) *ModelInfo {
	return db.models[name]
}

func (db *DB) Models() []*ModelInfo {
	var out []*ModelInfo
	for _, mi := range db.models {
		out = append(out, mi)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (db *DB) modelOf(v any) (*ModelInfo, reflect.Value, error) {
	rv := reflect.Indirect(reflect.ValueOf(v))
	if rv.Kind() != reflect.Struct {
		return nil, rv, fmt.Errorf("gd: expected struct, got %s", rv.Kind())
	}
	mi, ok := db.byType[rv.Type()]
	if !ok {
		return nil, rv, fmt.Errorf("gd: model %s is not registered", rv.Type().Name())
	}
	if reflect.ValueOf(v).Kind() != reflect.Ptr {
		return nil, rv, fmt.Errorf("gd: model must be a pointer")
	}
	return mi, rv, nil
}

func topoSort(all map[string]*ModelInfo, batch []*ModelInfo) ([]*ModelInfo, error) {
	deps := func(mi *ModelInfo) []*ModelInfo {
		var out []*ModelInfo
		for _, f := range mi.Fields {
			if f.Kind == kindInt && f.RelModel != "" {
				if dep, ok := all[f.RelModel]; ok && dep != mi {
					out = append(out, dep)
				}
			}
		}
		return out
	}
	visited := make(map[*ModelInfo]int)
	var order []*ModelInfo
	var visit func(mi *ModelInfo) error
	visit = func(mi *ModelInfo) error {
		switch visited[mi] {
		case 1:
			return fmt.Errorf("gd: circular FK dependency involving %s", mi.Name)
		case 2:
			return nil
		}
		visited[mi] = 1
		for _, dep := range deps(mi) {
			if err := visit(dep); err != nil {
				return err
			}
		}
		visited[mi] = 2
		order = append(order, mi)
		return nil
	}
	for _, mi := range batch {
		if err := visit(mi); err != nil {
			return nil, err
		}
	}
	return order, nil
}

func (db *DB) migrateTable(mi *ModelInfo) error {
	existing, err := db.tableColumns(mi.Table)
	if err != nil {
		return err
	}
	if len(existing) == 0 {
		var parts []string
		for _, f := range mi.Fields {
			if f.Kind == kindM2M {
				continue
			}
			var col string
			if f.Primary {
				col = db.dialect.PKColumnDef(f)
			} else {
				col = fmt.Sprintf("%s %s", f.Column, db.dialect.SQLType(f))
				if !f.Nullable {
					col += " NOT NULL"
				}
				if f.Unique {
					col += " UNIQUE"
				}
			}
			parts = append(parts, col)
		}
		for _, f := range mi.Fields {
			if f.Kind == kindInt && f.RelModel != "" {
				rel, ok := db.models[f.RelModel]
				if !ok {
					return fmt.Errorf("gd: %s.%s: unknown related model %q", mi.Name, f.Name, f.RelModel)
				}
				parts = append(parts, fmt.Sprintf(
					"FOREIGN KEY (%s) REFERENCES %s(%s)",
					f.Column, rel.Table, rel.PK.Column,
				))
			}
		}
		if _, err := db.Exec(fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (%s)", mi.Table, strings.Join(parts, ", "))); err != nil {
			return fmt.Errorf("gd: create %s: %w", mi.Table, err)
		}
	} else {
		for _, f := range mi.Fields {
			if f.Kind == kindM2M || existing[f.Column] {
				continue
			}
			alter := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", mi.Table, f.Column, db.dialect.SQLType(f))
			if _, err := db.Exec(db.dialect.Rebind(alter)); err != nil {
				return fmt.Errorf("gd: alter %s.%s: %w", mi.Table, f.Column, err)
			}
		}
	}
	for _, f := range mi.Fields {
		needIndex := f.Index || (f.Kind == kindInt && f.RelModel != "")
		if f.Kind == kindM2M || f.Primary || !needIndex {
			continue
		}
		idx := fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_%s ON %s(%s)", mi.Table, f.Column, mi.Table, f.Column)
		if _, err := db.Exec(idx); err != nil {
			return fmt.Errorf("gd: index %s.%s: %w", mi.Table, f.Column, err)
		}
	}
	return nil
}

func (db *DB) migrateJunction(mi *ModelInfo, f *Field) error {
	rel, ok := db.models[f.RelModel]
	if !ok {
		return fmt.Errorf("gd: %s.%s: unknown related model %q", mi.Name, f.Name, f.RelModel)
	}
	jt := junctionTable(mi, rel)
	srcCol := singular(mi.Table) + "_id"
	dstCol := singular(rel.Table) + "_id"
	stmt := fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS %s (%s INTEGER NOT NULL REFERENCES %s(%s), %s INTEGER NOT NULL REFERENCES %s(%s), PRIMARY KEY (%s, %s))",
		jt, srcCol, mi.Table, mi.PK.Column, dstCol, rel.Table, rel.PK.Column, srcCol, dstCol,
	)
	if _, err := db.Exec(stmt); err != nil {
		return fmt.Errorf("gd: create %s: %w", jt, err)
	}
	idx := fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_%s ON %s(%s)", jt, dstCol, jt, dstCol)
	if _, err := db.Exec(idx); err != nil {
		return fmt.Errorf("gd: index %s: %w", jt, err)
	}
	return nil
}

func junctionTable(owner, rel *ModelInfo) string {
	return singular(owner.Table) + "_" + singular(rel.Table)
}

func (db *DB) tableColumns(table string) (map[string]bool, error) {
	query := db.dialect.TableColumnsQuery()
	rows, err := db.Query(db.dialect.Rebind(query), table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		cols[name] = true
	}
	return cols, rows.Err()
}
