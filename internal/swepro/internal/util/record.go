// Record predicate — port of src/util/record.ts:1-2 (swe-pro 3b25a1a).
package util

import "reflect"

func IsRecord(value any) bool {
	if value == nil {
		return false
	}
	v := reflect.ValueOf(value)
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return false
		}
		v = v.Elem()
	}
	return v.Kind() == reflect.Map || v.Kind() == reflect.Struct
}
