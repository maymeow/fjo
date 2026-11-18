package apicmd

import (
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/fjoerr"
)

// param is one -f/-F parameter in command-line order.
type param struct {
	key   string // as written, e.g. "labels[]"
	value any    // string, bool, int64 or nil
}

// parseFields parses -f (raw) and -F (typed) flags. Raw fields come first,
// then typed ones, each in command-line order (cobra does not keep the
// interleaving of different flags). stdinUsed guards against reading
// standard input twice.
func parseFields(f *app.Factory, raw, typed []string, stdinUsed *bool) ([]param, error) {
	var out []param
	for _, s := range raw {
		k, v, err := splitField(s)
		if err != nil {
			return nil, err
		}
		out = append(out, param{key: k, value: v})
	}
	for _, s := range typed {
		k, v, err := splitField(s)
		if err != nil {
			return nil, err
		}
		val, err := typedValue(f, v, stdinUsed)
		if err != nil {
			return nil, err
		}
		out = append(out, param{key: k, value: val})
	}
	return out, nil
}

func splitField(s string) (string, string, error) {
	k, v, ok := strings.Cut(s, "=")
	if !ok || k == "" {
		return "", "", fjoerr.Usage("invalid field %q: expected key=value", s)
	}
	if _, err := keyPath(k); err != nil {
		return "", "", err
	}
	return k, v, nil
}

// typedValue converts -F values like gh: true/false/null, integers, and
// "@file" / "@-" to read the value from a file or standard input.
func typedValue(f *app.Factory, v string, stdinUsed *bool) (any, error) {
	switch v {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "null":
		return nil, nil
	}
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		return n, nil
	}
	if name, ok := strings.CutPrefix(v, "@"); ok {
		var (
			b   []byte
			err error
		)
		if name == "-" {
			if *stdinUsed {
				return nil, fjoerr.Usage("standard input can be read only once")
			}
			*stdinUsed = true
			b, err = io.ReadAll(f.IO.In)
		} else {
			b, err = os.ReadFile(name) //nolint:gosec // G304: @file field values name files the user chose
		}
		if err != nil {
			return nil, fjoerr.Wrap(fjoerr.KindUsage, err, "read field value from %s", v)
		}
		return string(b), nil
	}
	return v, nil
}

// keyPath splits "a[b][]" into ["a", "b", ""]. An empty segment means
// "append to array" and is allowed only at the end.
func keyPath(key string) ([]string, error) {
	bad := func() ([]string, error) {
		return nil, fjoerr.Usage("invalid field key %q", key)
	}
	i := strings.IndexByte(key, '[')
	if i == 0 {
		return bad()
	}
	if i < 0 {
		if strings.ContainsRune(key, ']') {
			return bad()
		}
		return []string{key}, nil
	}
	path := []string{key[:i]}
	rest := key[i:]
	for rest != "" {
		if rest[0] != '[' {
			return bad()
		}
		j := strings.IndexByte(rest, ']')
		if j < 0 {
			return bad()
		}
		seg := rest[1:j]
		if strings.ContainsAny(seg, "[") {
			return bad()
		}
		path = append(path, seg)
		rest = rest[j+1:]
	}
	for _, seg := range path[:len(path)-1] {
		if seg == "" {
			return nil, fjoerr.Usage("invalid field key %q: \"[]\" is only allowed at the end", key)
		}
	}
	return path, nil
}

// buildObject turns params into a nested JSON object.
func buildObject(params []param) (map[string]any, error) {
	root := map[string]any{}
	conflict := func(p param) error {
		return fjoerr.Usage("field %q conflicts with another field", p.key)
	}
	for _, p := range params {
		path, err := keyPath(p.key)
		if err != nil {
			return nil, err
		}
		cur := root
		for i, seg := range path {
			existing, exists := cur[seg]
			switch {
			case i == len(path)-1:
				if exists {
					return nil, fjoerr.Usage("field %q is set more than once", p.key)
				}
				cur[seg] = p.value
			case path[i+1] == "": // seg[] (keyPath guarantees it is last)
				arr, ok := existing.([]any)
				if exists && !ok {
					return nil, conflict(p)
				}
				cur[seg] = append(arr, p.value)
			default:
				next, ok := existing.(map[string]any)
				if exists && !ok {
					return nil, conflict(p)
				}
				if !exists {
					next = map[string]any{}
					cur[seg] = next
				}
				cur = next
				continue
			}
			break
		}
	}
	return root, nil
}
