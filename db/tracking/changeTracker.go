// Package tracking records snapshots of entity graphs and reports how they changed
// between loading and saving.
package tracking

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// ChangeTracker snapshots an object graph and later reports how it changed. Call
// Before on entities as they are loaded, then call Track on the same roots after they
// are modified to obtain the list of changes.
type ChangeTracker struct {
	cache cache
}

// NewChangeTracker returns an empty tracker backed by an in-memory cache.
func NewChangeTracker() *ChangeTracker {
	return &ChangeTracker{cache: newMemoryCache()}
}

// ChangeStatus enumerates the change kinds reported by Track.
var ChangeStatus = struct {
	Added   int
	Removed int
	Updated int
}{
	Added:   1,
	Removed: 2,
	Updated: 3,
}

// Change pairs a detected change kind with the entity it applies to.
type Change struct {
	Status int
	Data   any
}

// Before records a JSON snapshot of the objects reachable from oldObject so Track can
// later compare them. Objects are keyed by type and id, with untracked objects keyed
// as new. It returns an error if marshalling fails.
func (c *ChangeTracker) Before(oldObject any) error {
	objs, err := extractObjects(oldObject)
	if err != nil {
		return err
	}

	for key, obj := range objs {
		b, err := json.Marshal(obj)
		if err != nil {
			return err
		}
		if err := c.cache.Set(key, b); err != nil {
			return err
		}
	}
	return nil
}

// Track compares the current objects reachable from newObject against the snapshots
// stored by Before and returns the detected changes. Entities absent from the snapshot
// are reported as Added, missing entities as Removed, and entities whose fields differ
// (ignoring UpdatedAt and CreatedAt) as Updated. It returns an error if the object
// graph cannot be walked or a snapshot cannot be decoded.
func (c *ChangeTracker) Track(newObject any) ([]Change, error) {
	newMap, err := extractObjects(newObject)
	if err != nil {
		return nil, err
	}

	oldMap := make(map[string]any, len(newMap))
	for key, newObj := range newMap {
		raw, _ := c.cache.Get(key)
		if raw == nil {
			continue
		}
		bs, ok := raw.([]byte)
		if !ok {
			continue
		}

		t := indirectType(reflect.TypeOf(newObj))
		ptr := reflect.New(t).Interface()
		if err := json.Unmarshal(bs, ptr); err != nil {
			return nil, err
		}
		oldMap[key] = ptr
	}

	return compareObjects(oldMap, newMap), nil
}

func extractObjects(root any) (map[string]any, error) {
	out := make(map[string]any)

	v := reflect.ValueOf(root)
	if !v.IsValid() {
		return nil, fmt.Errorf("nil object")
	}
	return extractRec(root, v, "root", out)
}

func extractRec(original any, v reflect.Value, path string, out map[string]any) (map[string]any, error) {
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return out, nil
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return out, nil
	}

	if key, ok := makeKey(v, path); ok {
		out[key] = original
	}

	for i := 0; i < v.NumField(); i++ {
		fv := v.Field(i)
		ft := v.Type().Field(i).Type

		if ft.Kind() == reflect.Slice || ft.Kind() == reflect.Array {
			for j := 0; j < fv.Len(); j++ {
				itemV := fv.Index(j)
				item := itemV.Interface()
				_, _ = extractRec(item, itemV, fmt.Sprintf("%s.%s[%d]", path, v.Type().Field(i).Name, j), out)
			}
		}
	}

	return out, nil
}

// makeKey derives the cache key for a struct value: its type plus the string form of
// its Id field. Values whose Id is the zero value are keyed under a "__new__" marker
// so that not-yet-persisted entities are tracked separately. It reports false when the
// value has no Id field.
func makeKey(v reflect.Value, path string) (string, bool) {
	idField := v.FieldByName("Id")
	if !idField.IsValid() {
		return "", false
	}

	t := v.Type()
	if isZero(idField) {
		return fmt.Sprintf("%s:__new__:%s", t.String(), path), true
	}
	return fmt.Sprintf("%s:%v", t.String(), idField.Interface()), true
}

func isZero(v reflect.Value) bool {
	zero := reflect.Zero(v.Type())
	return reflect.DeepEqual(v.Interface(), zero.Interface())
}

func indirectType(t reflect.Type) reflect.Type {
	if t.Kind() == reflect.Ptr {
		return t.Elem()
	}
	return t
}

func compareObjects(oldMap, newMap map[string]any) []Change {
	var changes []Change

	for key, oldObj := range oldMap {
		if newObj, ok := newMap[key]; ok {
			diff := hasAnyDiff(oldObj, newObj)
			if diff {
				changes = append(changes, Change{Status: ChangeStatus.Updated, Data: newObj})
			}
		} else {
			changes = append(changes, Change{Status: ChangeStatus.Removed, Data: oldObj})
		}
	}

	for key, newObj := range newMap {
		if _, ok := oldMap[key]; !ok {
			changes = append(changes, Change{Status: ChangeStatus.Added, Data: newObj})
		}
	}

	return changes
}

func hasAnyDiff(oldObject, newObject any) bool {
	oldVal := reflect.ValueOf(oldObject)
	newVal := reflect.ValueOf(newObject)

	if oldVal.Kind() == reflect.Ptr {
		oldVal = oldVal.Elem()
	}
	if newVal.Kind() == reflect.Ptr {
		newVal = newVal.Elem()
	}

	if oldVal.Kind() != reflect.Struct || newVal.Kind() != reflect.Struct {
		return !reflect.DeepEqual(oldObject, newObject)
	}
	if oldVal.Type() != newVal.Type() {
		return true
	}

	for i := 0; i < oldVal.NumField(); i++ {
		name := oldVal.Type().Field(i).Name
		if name == "UpdatedAt" || name == "CreatedAt" {
			continue
		}
		if !reflect.DeepEqual(oldVal.Field(i).Interface(), newVal.Field(i).Interface()) {
			return true
		}
	}
	return false
}

// IsSoftDeleted reports whether obj has a true IsDeleted bool field. Non-struct and
// non-pointer values, and types without the field, return false.
func IsSoftDeleted(obj any) bool {
	v := reflect.ValueOf(obj)
	if v.Kind() == reflect.Ptr && !v.IsNil() {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return false
	}
	f := v.FieldByName("IsDeleted")
	return f.IsValid() && f.Kind() == reflect.Bool && f.Bool()
}

// IsHardDeleted reports whether obj has a true HardDeleted bool field. Non-struct and
// non-pointer values, and types without the field, return false.
func IsHardDeleted(obj any) bool {
	v := reflect.ValueOf(obj)
	if v.Kind() == reflect.Ptr && !v.IsNil() {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return false
	}
	f := v.FieldByName("HardDeleted")
	return f.IsValid() && f.Kind() == reflect.Bool && f.Bool()
}
