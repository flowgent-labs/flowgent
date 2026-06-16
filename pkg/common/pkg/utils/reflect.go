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
// Embedded structs are flattened; outer fields shadow inner ones with the same column name.
// Column name priority: db tag > json tag > snake_case(field name).
func StructFields(entity any) (cols []string, args []any) {
	v := reflect.ValueOf(entity)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return
	}
	seen := map[string]bool{}
	collectFields(v, &cols, &args, seen, false)
	return
}

func collectFields(v reflect.Value, cols *[]string, args *[]any, seen map[string]bool, embedded bool) {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		if f.Anonymous && f.Type.Kind() == reflect.Struct {
			collectFields(v.Field(i), cols, args, seen, true)
			continue
		}
		col := ColName(f)
		if col == "" || col == "-" || seen[col] {
			continue
		}
		seen[col] = true
		*cols = append(*cols, col)
		*args = append(*args, v.Field(i).Interface())
	}
}

// ColName returns the SQL column name for a struct field.
func ColName(f reflect.StructField) string {
	if tag := f.Tag.Get("db"); tag != "" {
		return strings.Split(tag, ",")[0]
	}
	if tag := f.Tag.Get("json"); tag != "" {
		n := strings.Split(tag, ",")[0]
		if n != "" && n != "-" {
			return n
		}
	}
	return ToSnakeCase(f.Name)
}

// ToSnakeCase converts CamelCase to snake_case.
func ToSnakeCase(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
		}
		b.WriteRune(r)
	}
	return strings.ToLower(b.String())
}

// IsJSONType returns true if the field type should be scanned as JSON ([]byte→Unmarshal).
func IsJSONType(ft reflect.Type) bool {
	if ft == reflect.TypeOf(json.RawMessage{}) {
		return false
	}
	if ft == reflect.TypeOf([]byte{}) {
		return false
	}
	switch ft.Kind() {
	case reflect.Map:
		return true
	case reflect.Slice:
		return ft.Elem().Kind() != reflect.Uint8 // exclude []byte
	case reflect.Struct:
		return ft != reflect.TypeOf(time.Time{})
	case reflect.Ptr:
		if ft.Elem().Kind() == reflect.Struct && ft.Elem() != reflect.TypeOf(time.Time{}) {
			return true
		}
	}
	return false
}

type scanEntry struct {
	ptr  any
	idx  int // index into ev for the owning struct
	json bool
}

// ScanStruct scans a row into a struct, handling JSON types automatically.
// Embedded structs are flattened; outer fields shadow inner ones with the same column name.
func ScanStruct(scanner interface{ Scan(dest ...any) error }, dest any) error {
	v := reflect.ValueOf(dest)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("dest must be *struct")
	}
	ev := v.Elem()

	var entries []scanEntry
	seen := map[string]bool{}
	collectScanFields(ev, &entries, seen)

	var ptrs []any
	jsonIdxs := make(map[int]int) // ptrsIndex → entriesIndex
	for ei, e := range entries {
		if e.json {
			jsonIdxs[len(ptrs)] = ei
			ptrs = append(ptrs, reflect.New(reflect.TypeOf([]byte{})).Interface())
		} else {
			ptrs = append(ptrs, e.ptr)
		}
	}
	if err := scanner.Scan(ptrs...); err != nil {
		return err
	}
	for pi, ei := range jsonIdxs {
		b := ptrs[pi].(*[]byte)
		if b == nil || len(*b) == 0 {
			continue
		}
		ent := entries[ei]
		fv := ev.Field(ent.idx)
		json.Unmarshal(*b, fv.Addr().Interface())
	}
	return nil
}

func collectScanFields(v reflect.Value, entries *[]scanEntry, seen map[string]bool) {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		if f.Anonymous && f.Type.Kind() == reflect.Struct {
			collectScanFields(v.Field(i), entries, seen)
			continue
		}
		col := ColName(f)
		if col == "" || col == "-" || seen[col] {
			continue
		}
		seen[col] = true
		fv := v.Field(i)
		if IsJSONType(fv.Type()) {
			*entries = append(*entries, scanEntry{ptr: nil, idx: i, json: true})
		} else {
			*entries = append(*entries, scanEntry{ptr: fv.Addr().Interface(), idx: i, json: false})
		}
	}
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
