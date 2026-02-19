package systemcontroller

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
)

// sortSlice sorts a slice of structs by the struct field whose json tag (or
// field name when no tag is present) matches sortBy.  sortOrder must be "asc"
// or "desc"; it defaults to "asc" for any other value.  An empty sortBy
// returns the slice unchanged.
func sortSlice[T any](slice []T, sortBy, sortOrder string) []T {
	if len(slice) == 0 || sortBy == "" {
		return slice
	}

	idx, ok := fieldIndex[T](sortBy)
	if !ok {
		return slice
	}

	desc := strings.EqualFold(sortOrder, "desc")

	sort.SliceStable(slice, func(i, j int) bool {
		a := reflect.ValueOf(slice[i]).Field(idx)
		b := reflect.ValueOf(slice[j]).Field(idx)
		cmp := compareValues(a, b)
		if desc {
			return cmp > 0
		}
		return cmp < 0
	})

	return slice
}

// fieldIndex returns the struct field index whose json tag name (or exported
// field name, if no tag) matches key.
func fieldIndex[T any](key string) (int, bool) {
	var zero T
	t := reflect.TypeOf(zero)
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		name := tagName(tag)
		if name == "" {
			name = f.Name
		}
		if name == key {
			return i, true
		}
	}
	return 0, false
}

// tagName extracts the field name from a json struct tag, ignoring options
// like omitempty.  Returns "" for "-" or empty tags.
func tagName(tag string) string {
	if tag == "" || tag == "-" {
		return ""
	}
	name, _, _ := strings.Cut(tag, ",")
	return name
}

// compareValues compares two reflect.Values, returning -1, 0, or 1.
func compareValues(a, b reflect.Value) int {
	switch a.Kind() {
	case reflect.String:
		return strings.Compare(a.String(), b.String())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		ai, bi := a.Int(), b.Int()
		if ai < bi {
			return -1
		}
		if ai > bi {
			return 1
		}
		return 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		au, bu := a.Uint(), b.Uint()
		if au < bu {
			return -1
		}
		if au > bu {
			return 1
		}
		return 0
	case reflect.Bool:
		ab, bb := a.Bool(), b.Bool()
		if ab == bb {
			return 0
		}
		if !ab {
			return -1
		}
		return 1
	case reflect.Struct:
		if a.Type() == reflect.TypeOf(time.Time{}) {
			at := a.Interface().(time.Time)
			bt := b.Interface().(time.Time)
			if at.Before(bt) {
				return -1
			}
			if at.After(bt) {
				return 1
			}
			return 0
		}
		return strings.Compare(fmt.Sprint(a.Interface()), fmt.Sprint(b.Interface()))
	default:
		return strings.Compare(fmt.Sprint(a.Interface()), fmt.Sprint(b.Interface()))
	}
}
