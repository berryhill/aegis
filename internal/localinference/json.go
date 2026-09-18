package localinference

import (
	"encoding/json"
	"errors"
)

func uniqueJSON(d *json.Decoder, depth int) error {
	if depth > 32 {
		return errors.New("JSON nesting exceeds bound")
	}
	token, e := d.Token()
	if e != nil {
		return e
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	if delim != '{' && delim != '[' {
		return errors.New("invalid JSON")
	}
	seen := map[string]bool{}
	for d.More() {
		if delim == '{' {
			key, e := d.Token()
			if e != nil {
				return e
			}
			s, ok := key.(string)
			if !ok || seen[s] {
				return errors.New("duplicate JSON key")
			}
			seen[s] = true
		}
		if e := uniqueJSON(d, depth+1); e != nil {
			return e
		}
	}
	_, e = d.Token()
	return e
}
