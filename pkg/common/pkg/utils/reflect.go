package utils

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"time"
)

// StructFields extracts column names and values from a struct's tags.
// Column name priority: db tag > json tag > snake_case(field name).
func StructFields(entity any) (cols []string, args []any) {
	v := reflect.ValueOf(entity)
	if v.Kind() == reflect.Ptr { v = v.Elem() }
	if v.Kind() != reflect.Struct { return }
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() { continue }
		col := ColName(f)
		if col == "" || col == "-" { continue }
		cols = append(cols, col)
		args = append(args, v.Field(i).Interface())
	}
	return
}

// ColName returns the SQL column name for a struct field.
func ColName(f reflect.StructField) string {
	if tag := f.Tag.Get("db"); tag != "" { return strings.Split(tag, ",")[0] }
	if tag := f.Tag.Get("json"); tag != "" {
		n := strings.Split(tag, ",")[0]
		if n != "" && n != "-" { return n }
	}
	return ToSnakeCase(f.Name)
}

// ToSnakeCase converts CamelCase to snake_case.
func ToSnakeCase(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' { b.WriteByte('_') }
		b.WriteRune(r)
	}
	return strings.ToLower(b.String())
}

// IsJSONType returns true if the field type should be scanned as JSON ([]byte→Unmarshal).
func IsJSONType(ft reflect.Type) bool {
	if ft == reflect.TypeOf(json.RawMessage{}) { return false }
	if ft == reflect.TypeOf([]byte{}) { return false }
	switch ft.Kind() {
	case reflect.Map:
		return true
	case reflect.Slice:
		return ft.Elem().Kind() != reflect.Uint8 // exclude []byte
	case reflect.Struct:
		return ft != reflect.TypeOf(time.Time{})
	case reflect.Ptr:
		if ft.Elem().Kind() == reflect.Struct && ft.Elem() != reflect.TypeOf(time.Time{}) { return true }
	}
	return false
}

// ScanStruct scans a row into a struct, handling JSON types automatically.
// Uses ColName to filter fields, matching StructFields output.
func ScanStruct(scanner interface{ Scan(dest ...any) error }, dest any) error {
	v := reflect.ValueOf(dest)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("dest must be *struct")
	}
	ev := v.Elem()
	t := ev.Type()

	var ptrs []any
	jsonIdxs := make(map[int]int) // ptrsIndex → fieldIndex
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() { continue }
		if ColName(f) == "" || ColName(f) == "-" { continue }
		fv := ev.Field(i)
		ft := fv.Type()
		if IsJSONType(ft) {
			jsonIdxs[len(ptrs)] = i
			ptrs = append(ptrs, reflect.New(reflect.TypeOf([]byte{})).Interface())
		} else {
			ptrs = append(ptrs, fv.Addr().Interface())
		}
	}
	if err := scanner.Scan(ptrs...); err != nil { return err }
	for pi, fi := range jsonIdxs {
		b := ptrs[pi].(*[]byte)
		if b == nil || len(*b) == 0 { continue }
		json.Unmarshal(*b, ev.Field(fi).Addr().Interface())
	}
	return nil
}

var sqlIdentRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// ValidateIdent returns an error if any identifier contains unsafe characters.
func ValidateIdent(names ...string) error {
	for _, n := range names {
		if !sqlIdentRe.MatchString(n) {
			return fmt.Errorf("invalid SQL identifier: %q", n)
		}
	}
	return nil
}

// Columns returns comma-separated column names for a struct type.
func Columns[T any]() string {
	var entity T
	cols, _ := StructFields(&entity)
	return strings.Join(cols, ",")
}

