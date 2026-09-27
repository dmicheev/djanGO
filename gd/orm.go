package gd

import (
	"database/sql"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

var timeLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02 15:04:05",
	"2006-01-02T15:04",
	"2006-01-02 15:04",
	"2006-01-02",
}

func (db *DB) Create(m any) error {
	mi, rv, err := db.modelOf(m)
	if err != nil {
		return err
	}
	db.autoNow(mi, rv, false)
	var cols []string
	var args []any
	for _, f := range mi.Fields {
		if f.Kind == kindM2M || f.Primary {
			continue
		}
		cols = append(cols, f.Column)
		args = append(args, fieldArg(f, rv.FieldByIndex(f.index)))
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(cols)), ",")
	stmt := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", mi.Table, strings.Join(cols, ", "), ph)
	var id int64
	if db.dialect.NeedsReturning() {
		stmt += " RETURNING " + mi.PK.Column
		err := db.QueryRow(db.dialect.Rebind(stmt), args...).Scan(&id)
		if err != nil {
			return fmt.Errorf("gd: insert %s: %w", mi.Table, err)
		}
		rv.FieldByIndex(mi.PK.index).SetInt(id)
		return nil
	}
	res, err := db.Exec(stmt, args...)
	if err != nil {
		return fmt.Errorf("gd: insert %s: %w", mi.Table, err)
	}
	id, err = res.LastInsertId()
	if err == nil {
		rv.FieldByIndex(mi.PK.index).SetInt(id)
	}
	return nil
}

func (db *DB) Update(m any) error {
	mi, rv, err := db.modelOf(m)
	if err != nil {
		return err
	}
	if rv.FieldByIndex(mi.PK.index).Int() == 0 {
		return fmt.Errorf("gd: update %s: zero primary key", mi.Table)
	}
	db.autoNow(mi, rv, true)
	var sets []string
	var args []any
	for _, f := range mi.Fields {
		if f.Kind == kindM2M || f.Primary {
			continue
		}
		sets = append(sets, f.Column+" = ?")
		args = append(args, fieldArg(f, rv.FieldByIndex(f.index)))
	}
	args = append(args, rv.FieldByIndex(mi.PK.index).Int())
	stmt := fmt.Sprintf("UPDATE %s SET %s WHERE %s = ?", mi.Table, strings.Join(sets, ", "), mi.PK.Column)
	if _, err := db.Exec(db.dialect.Rebind(stmt), args...); err != nil {
		return fmt.Errorf("gd: update %s: %w", mi.Table, err)
	}
	return nil
}

func (db *DB) Save(m any) error {
	mi, rv, err := db.modelOf(m)
	if err != nil {
		return err
	}
	if rv.FieldByIndex(mi.PK.index).Int() == 0 {
		return db.Create(m)
	}
	return db.Update(m)
}

func (db *DB) Delete(m any) error {
	mi, rv, err := db.modelOf(m)
	if err != nil {
		return err
	}
	return db.DeleteByID(mi, rv.FieldByIndex(mi.PK.index).Int())
}

func (db *DB) DeleteByID(mi *ModelInfo, id int64) error {
	for _, f := range mi.Fields {
		if f.Kind == kindM2M {
			rel := db.models[f.RelModel]
			if rel != nil {
				jt := junctionTable(mi, rel)
				stmt := fmt.Sprintf("DELETE FROM %s WHERE %s = ?", jt, singular(mi.Table)+"_id")
				if _, err := db.Exec(db.dialect.Rebind(stmt), id); err != nil {
					return fmt.Errorf("gd: m2m cleanup %s: %w", jt, err)
				}
			}
		}
	}
	stmt := fmt.Sprintf("DELETE FROM %s WHERE %s = ?", mi.Table, mi.PK.Column)
	if _, err := db.Exec(db.dialect.Rebind(stmt), id); err != nil {
		return fmt.Errorf("gd: delete %s: %w", mi.Table, err)
	}
	return nil
}

func (db *DB) FindByID(out any, id int64) error {
	mi, _, err := db.modelOf(out)
	if err != nil {
		return err
	}
	q := db.Q(mi).Filter(mi.PK.Name, id).Limit(1)
	slice := reflect.MakeSlice(reflect.SliceOf(mi.Type), 0, 1)
	dst := reflect.New(slice.Type())
	dst.Elem().Set(slice)
	if err := q.All(dst.Interface()); err != nil {
		return err
	}
	s := dst.Elem()
	if s.Len() == 0 {
		return sql.ErrNoRows
	}
	reflect.ValueOf(out).Elem().Set(s.Index(0))
	return nil
}

func (db *DB) All(dst any) error {
	mi, _, err := db.modelOf(reflect.ValueOf(dst).Elem().Interface())
	if err != nil {
		return err
	}
	return db.Q(mi).All(dst)
}

func (db *DB) autoNow(mi *ModelInfo, rv reflect.Value, isUpdate bool) {
	for _, f := range mi.Fields {
		if f.Kind != kindTime {
			continue
		}
		if f.Name == "CreatedAt" && !isUpdate {
			fv := rv.FieldByIndex(f.index)
			if fv.Type() == reflect.TypeOf(time.Time{}) && fv.Interface().(time.Time).IsZero() {
				fv.Set(reflect.ValueOf(time.Now()))
			}
		}
		if f.Name == "UpdatedAt" {
			fv := rv.FieldByIndex(f.index)
			if fv.Type() == reflect.TypeOf(time.Time{}) {
				fv.Set(reflect.ValueOf(time.Now()))
			}
		}
	}
}

type Query struct {
	db     *DB
	mi     *ModelInfo
	wheres []string
	args   []any
	order  string
	limit  int
	offset int
}

func (db *DB) Q(model any) *Query {
	var mi *ModelInfo
	switch m := model.(type) {
	case *ModelInfo:
		mi = m
	default:
		mi, _, _ = db.modelOf(model)
	}
	return &Query{db: db, mi: mi}
}

func (q *Query) Filter(field string, val any) *Query {
	if f := q.mi.Field(field); f != nil {
		q.wheres = append(q.wheres, f.Column+" = ?")
		q.args = append(q.args, val)
	}
	return q
}

func (q *Query) Search(fields []string, s string) *Query {
	s = strings.NewReplacer("%", "", "_", "").Replace(s)
	if s == "" {
		return q
	}
	var likes []string
	for _, name := range fields {
		f := q.mi.Field(name)
		if f == nil || f.Kind == kindM2M {
			continue
		}
		likes = append(likes, fmt.Sprintf("%s LIKE ?", f.Column))
		q.args = append(q.args, "%"+s+"%")
	}
	if len(likes) > 0 {
		q.wheres = append(q.wheres, "("+strings.Join(likes, " OR ")+")")
	}
	return q
}

func (q *Query) Order(fields ...string) *Query {
	var cols []string
	for _, name := range fields {
		dir := ""
		if strings.HasPrefix(name, "-") {
			dir = " DESC"
			name = name[1:]
		}
		if f := q.mi.Field(name); f != nil && f.Kind != kindM2M {
			cols = append(cols, f.Column+dir)
		}
	}
	if len(cols) > 0 {
		q.order = strings.Join(cols, ", ")
	}
	return q
}

func (q *Query) Limit(n int) *Query {
	q.limit = n
	return q
}

func (q *Query) Offset(n int) *Query {
	q.offset = n
	return q
}

func (q *Query) All(dst any) error {
	dv := reflect.ValueOf(dst)
	if dv.Kind() != reflect.Ptr || dv.Elem().Kind() != reflect.Slice {
		return fmt.Errorf("gd: destination must be *[]%s", q.mi.Name)
	}
	slice := dv.Elem()
	slice.Set(slice.Slice(0, 0))
	stmt, args := q.buildSelect()
	rows, err := q.db.Query(q.db.dialect.Rebind(stmt), args...)
	if err != nil {
		return fmt.Errorf("gd: select %s: %w", q.mi.Table, err)
	}
	defer rows.Close()
	elem := slice.Type().Elem()
	for rows.Next() {
		ev := reflect.New(elem).Elem()
		if err := q.scanRow(rows, ev); err != nil {
			return err
		}
		slice.Set(reflect.Append(slice, ev))
	}
	return rows.Err()
}

func (q *Query) Count() (int, error) {
	stmt := fmt.Sprintf("SELECT COUNT(*) FROM %s", q.mi.Table)
	if len(q.wheres) > 0 {
		stmt += " WHERE " + strings.Join(q.wheres, " AND ")
	}
	var n int
	if err := q.db.QueryRow(q.db.dialect.Rebind(stmt), q.args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("gd: count %s: %w", q.mi.Table, err)
	}
	return n, nil
}

func (q *Query) First(dst any) error {
	q.limit = 1
	dv := reflect.ValueOf(dst)
	slice := reflect.MakeSlice(reflect.SliceOf(dv.Elem().Type()), 0, 1)
	holder := reflect.New(slice.Type())
	if err := q.All(holder.Interface()); err != nil {
		return err
	}
	if holder.Elem().Len() == 0 {
		return sql.ErrNoRows
	}
	dv.Elem().Set(holder.Elem().Index(0))
	return nil
}

func (q *Query) buildSelect() (string, []any) {
	var cols []string
	for _, f := range q.mi.Fields {
		if f.Kind != kindM2M {
			cols = append(cols, f.Column)
		}
	}
	stmt := fmt.Sprintf("SELECT %s FROM %s", strings.Join(cols, ", "), q.mi.Table)
	if len(q.wheres) > 0 {
		stmt += " WHERE " + strings.Join(q.wheres, " AND ")
	}
	if q.order != "" {
		stmt += " ORDER BY " + q.order
	}
	if q.limit > 0 {
		stmt += " LIMIT " + strconv.Itoa(q.limit)
		if q.offset > 0 {
			stmt += " OFFSET " + strconv.Itoa(q.offset)
		}
	}
	return stmt, q.args
}

func (q *Query) scanRow(rows *sql.Rows, ev reflect.Value) error {
	srcs := make([]any, 0, len(q.mi.Fields))
	for _, f := range q.mi.Fields {
		if f.Kind != kindM2M {
			srcs = append(srcs, new(any))
		}
	}
	if err := rows.Scan(srcs...); err != nil {
		return fmt.Errorf("gd: scan %s: %w", q.mi.Table, err)
	}
	i := 0
	for _, f := range q.mi.Fields {
		if f.Kind == kindM2M {
			continue
		}
		fv := ev.FieldByIndex(f.index)
		if err := setFieldValue(f, fv, *srcs[i].(*any)); err != nil {
			return fmt.Errorf("gd: %s.%s: %w", q.mi.Name, f.Name, err)
		}
		i++
	}
	return nil
}

func fieldArg(f *Field, fv reflect.Value) any {
	if f.Nullable && fv.Kind() == reflect.Ptr {
		if fv.IsNil() {
			return nil
		}
		fv = fv.Elem()
	}
	switch f.Kind {
	case kindInt:
		return fv.Int()
	case kindFloat:
		return fv.Float()
	case kindBool:
		return fv.Bool()
	case kindString:
		return fv.String()
	case kindTime:
		return fv.Interface()
	default:
		return fv.Interface()
	}
}

func setFieldValue(f *Field, fv reflect.Value, src any) error {
	if src == nil {
		return nil
	}
	if fv.Kind() == reflect.Ptr {
		pv := reflect.New(fv.Type().Elem())
		if err := setFieldValue(f, pv.Elem(), src); err != nil {
			return err
		}
		fv.Set(pv)
		return nil
	}
	switch f.Kind {
	case kindInt:
		switch v := src.(type) {
		case int64:
			fv.SetInt(v)
		case float64:
			fv.SetInt(int64(v))
		case bool:
			if v {
				fv.SetInt(1)
			}
		case string:
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return fmt.Errorf("bad int %q", v)
			}
			fv.SetInt(n)
		case []byte:
			return setFieldValue(f, fv, string(v))
		default:
			return fmt.Errorf("cannot scan %T into int", src)
		}
	case kindFloat:
		switch v := src.(type) {
		case float64:
			fv.SetFloat(v)
		case int64:
			fv.SetFloat(float64(v))
		case string:
			n, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return fmt.Errorf("bad float %q", v)
			}
			fv.SetFloat(n)
		case []byte:
			return setFieldValue(f, fv, string(v))
		default:
			return fmt.Errorf("cannot scan %T into float", src)
		}
	case kindBool:
		switch v := src.(type) {
		case bool:
			fv.SetBool(v)
		case int64:
			fv.SetBool(v != 0)
		case float64:
			fv.SetBool(v != 0)
		case string:
			fv.SetBool(v == "1" || v == "true" || v == "t")
		case []byte:
			return setFieldValue(f, fv, string(v))
		default:
			return fmt.Errorf("cannot scan %T into bool", src)
		}
	case kindString:
		switch v := src.(type) {
		case string:
			fv.SetString(v)
		case []byte:
			fv.SetString(string(v))
		default:
			fv.SetString(fmt.Sprint(v))
		}
	case kindTime:
		switch v := src.(type) {
		case time.Time:
			fv.Set(reflect.ValueOf(v))
		case string:
			return setTimeField(fv, v)
		case []byte:
			return setTimeField(fv, string(v))
		case int64:
			fv.Set(reflect.ValueOf(time.Unix(v, 0)))
		default:
			return fmt.Errorf("cannot scan %T into time", src)
		}
	}
	return nil
}

func setTimeField(fv reflect.Value, s string) error {
	s = strings.TrimSpace(s)
	for _, layout := range timeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			fv.Set(reflect.ValueOf(t))
			return nil
		}
	}
	return fmt.Errorf("bad time %q", s)
}

func (db *DB) M2MIDs(owner any, field string) ([]int64, error) {
	mi, rv, err := db.modelOf(owner)
	if err != nil {
		return nil, err
	}
	f := mi.Field(field)
	rel := db.models[f.RelModel]
	if f == nil || f.Kind != kindM2M || rel == nil {
		return nil, fmt.Errorf("gd: %s.%s is not a m2m field", mi.Name, field)
	}
	jt := junctionTable(mi, rel)
	stmt := fmt.Sprintf("SELECT %s FROM %s WHERE %s = ? ORDER BY %s",
		singular(rel.Table)+"_id", jt, singular(mi.Table)+"_id", singular(rel.Table)+"_id")
	rows, err := db.Query(db.dialect.Rebind(stmt), rv.FieldByIndex(mi.PK.index).Int())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (db *DB) M2MSet(owner any, field string, ids []int64) error {
	mi, rv, err := db.modelOf(owner)
	if err != nil {
		return err
	}
	f := mi.Field(field)
	rel := db.models[f.RelModel]
	if f == nil || f.Kind != kindM2M || rel == nil {
		return fmt.Errorf("gd: %s.%s is not a m2m field", mi.Name, field)
	}
	jt := junctionTable(mi, rel)
	srcID := rv.FieldByIndex(mi.PK.index).Int()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	delStmt := fmt.Sprintf("DELETE FROM %s WHERE %s = ?", jt, singular(mi.Table)+"_id")
	if _, err := tx.Exec(db.dialect.Rebind(delStmt), srcID); err != nil {
		return err
	}
	insStmt := db.dialect.InsertIgnore(jt, singular(mi.Table)+"_id, "+singular(rel.Table)+"_id")
	for _, id := range ids {
		if _, err := tx.Exec(db.dialect.Rebind(insStmt), srcID, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
