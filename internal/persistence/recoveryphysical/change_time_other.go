//go:build !linux

package recoveryphysical

import (
	"os"
	"reflect"
)

// BSD Stat_t names change time Ctimespec; retain the same guard there.
func SameChangeTime(a, b os.FileInfo) bool {
	x, y := reflect.ValueOf(a.Sys()).Elem(), reflect.ValueOf(b.Sys()).Elem()
	for _, name := range []string{"Ctim", "Ctimespec"} {
		u, v := x.FieldByName(name), y.FieldByName(name)
		if u.IsValid() && v.IsValid() {
			return reflect.DeepEqual(u.Interface(), v.Interface())
		}
	}
	return false
}
