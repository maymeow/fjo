package output

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/itchyny/gojq"
	"github.com/spf13/cobra"

	"github.com/maymeow/fjo/internal/fjoerr"
)

// JSONFlags holds --json, --jq and --template for a command (IMPLEMENTATION
// §10.2, BACKLOG B-2).
type JSONFlags struct {
	cmd      *cobra.Command
	allowed  []string
	fields   []string
	jq       string
	template string
	active   bool
}

// AddJSONFlags registers --json, --jq and --template on cmd. fields is the
// complete list of selectable JSON fields; --json without fields, unknown
// fields and --jq/--template without --json are usage errors.
func AddJSONFlags(cmd *cobra.Command, fields []string) *JSONFlags {
	j := &JSONFlags{
		cmd:     cmd,
		allowed: append([]string(nil), fields...),
	}
	fl := cmd.Flags()
	fl.StringSliceVar(&j.fields, "json", nil, "Output JSON with the specified `fields`")
	fl.StringVarP(&j.jq, "jq", "q", "", "Filter JSON output using a jq `expression`")
	fl.StringVarP(&j.template, "template", "t", "", "Format JSON output using a Go template")

	oldPreRun := cmd.PreRunE
	cmd.PreRunE = func(c *cobra.Command, args []string) error {
		if oldPreRun != nil {
			if err := oldPreRun(c, args); err != nil {
				return err
			}
		}
		return j.validate()
	}

	oldFlagError := cmd.FlagErrorFunc()
	cmd.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		if c == cmd && err.Error() == "flag needs an argument: --json" {
			return fjoerr.Usage("%s", j.missingFieldsMessage())
		}
		return oldFlagError(c, err)
	})
	return j
}

// Changed reports whether --json was supplied. Commands should produce JSON
// output only when this is true.
func (j *JSONFlags) Changed() bool { return j.active }

// Fields returns the validated, normalized field selection.
func (j *JSONFlags) Fields() []string { return append([]string(nil), j.fields...) }

func (j *JSONFlags) validate() error {
	fl := j.cmd.Flags()
	if !fl.Changed("json") {
		if fl.Changed("jq") {
			return fjoerr.Usage("--jq cannot be used without --json")
		}
		if fl.Changed("template") {
			return fjoerr.Usage("--template cannot be used without --json")
		}
		return nil
	}
	if web := fl.Lookup("web"); web != nil && web.Changed {
		return fjoerr.Usage("--json cannot be used with --web")
	}

	fields := make([]string, 0, len(j.fields))
	for _, f := range j.fields {
		f = strings.TrimSpace(f)
		if f != "" {
			fields = append(fields, f)
		}
	}
	if len(fields) == 0 {
		return fjoerr.Usage("%s", j.missingFieldsMessage())
	}
	for _, f := range fields {
		if !containsFold(j.allowed, f) {
			return fjoerr.Usage("Unknown JSON field: %q\nAvailable fields:\n  %s",
				f, strings.Join(sortedCopy(j.allowed), "\n  "))
		}
	}
	j.fields = fields
	j.active = true
	return nil
}

func (j *JSONFlags) missingFieldsMessage() string {
	return "Specify one or more comma-separated fields for `--json`:\n  " +
		strings.Join(sortedCopy(j.allowed), "\n  ")
}

// WriteOptions controls JSON/template presentation.
type WriteOptions struct {
	TTY   bool
	Color bool
	Width int
}

// Write exports v with the selected fields and writes JSON, a jq result or a
// template rendering.
func (j *JSONFlags) Write(ctx context.Context, w io.Writer, v any, o WriteOptions) error {
	if !j.active {
		return fjoerr.Usage("internal error: JSON output requested without --json")
	}
	data, err := ExportData(v, j.fields)
	if err != nil {
		return fjoerr.Usage("%s", err)
	}
	switch {
	case j.jq != "":
		return j.writeJQ(ctx, w, data, o)
	case j.template != "":
		return j.writeTemplate(w, data, o)
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if o.TTY {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(data); err != nil {
		return fjoerr.Wrap(fjoerr.KindGeneral, err, "encode JSON")
	}
	return nil
}

func (j *JSONFlags) writeJQ(ctx context.Context, w io.Writer, data any, o WriteOptions) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return fjoerr.Wrap(fjoerr.KindGeneral, err, "encode JSON")
	}
	var input any
	if err := json.Unmarshal(raw, &input); err != nil {
		return fjoerr.Wrap(fjoerr.KindGeneral, err, "decode JSON for jq")
	}
	query, err := gojq.Parse(j.jq)
	if err != nil {
		return fjoerr.Usage("invalid --jq expression: %v", err)
	}
	code, err := gojq.Compile(query)
	if err != nil {
		return fjoerr.Usage("invalid --jq expression: %v", err)
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if o.TTY {
		enc.SetIndent("", "  ")
	}
	iter := code.RunWithContext(ctx, input)
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if err, ok := v.(error); ok {
			return fjoerr.Usage("jq: %v", err)
		}
		// Like gh, string results are printed raw (no JSON quotes), so
		// `--jq '.[].title'` yields plain lines. On a terminal they are
		// sanitized because they are server-provided text.
		if s, ok := v.(string); ok {
			if o.TTY {
				s = Sanitize(s)
			}
			if _, err := io.WriteString(w, s+"\n"); err != nil {
				return fjoerr.Wrap(fjoerr.KindGeneral, err, "write jq output")
			}
			continue
		}
		if err := enc.Encode(v); err != nil {
			return fjoerr.Wrap(fjoerr.KindGeneral, err, "write jq output")
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func (j *JSONFlags) writeTemplate(w io.Writer, data any, o WriteOptions) error {
	var table Table
	rendered := false
	funcs := template.FuncMap{
		"tablerow": func(values ...any) string {
			row := make([]Cell, len(values))
			for i, v := range values {
				row[i] = Text(fmt.Sprint(v))
			}
			table.Rows = append(table.Rows, row)
			return ""
		},
		"tablerender": func() (string, error) {
			if len(table.Rows) == 0 {
				return "", nil
			}
			rendered = true
			err := table.Print(w, TableOptions{TTY: o.TTY, Width: o.Width, Color: o.Color})
			return "", err
		},
		"timeago":  timeAgoValue,
		"color":    func(spec, text string) string { return Colorize(spec, text, o.Color) },
		"join":     joinValues,
		"pluck":    pluckField,
		"truncate": func(n int, s string) string { return truncateDisplay(s, n) },
	}
	t, err := template.New("fjo").Funcs(funcs).Parse(j.template)
	if err != nil {
		return fjoerr.Usage("invalid --template: %v", err)
	}
	if err := t.Execute(w, data); err != nil {
		return fjoerr.Usage("execute --template: %v", err)
	}
	if len(table.Rows) > 0 && !rendered {
		// gh requires an explicit tablerender; don't silently swallow rows.
		return fjoerr.Usage("--template: tablerow used without tablerender")
	}
	return nil
}

func timeAgoValue(v any) string {
	switch t := v.(type) {
	case time.Time:
		return TimeAgo(t)
	case *time.Time:
		if t == nil {
			return ""
		}
		return TimeAgo(*t)
	case string:
		if parsed, err := time.Parse(time.RFC3339, t); err == nil {
			return TimeAgo(parsed)
		}
		return ""
	default:
		return ""
	}
}

func joinValues(sep string, v any) string {
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return ""
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return fmt.Sprint(v)
	}
	parts := make([]string, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		parts[i] = fmt.Sprint(rv.Index(i).Interface())
	}
	return strings.Join(parts, sep)
}

func pluckField(field string, v any) []any {
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil
	}
	out := make([]any, 0, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		item := rv.Index(i)
		for item.Kind() == reflect.Pointer || item.Kind() == reflect.Interface {
			if item.IsNil() {
				break
			}
			item = item.Elem()
		}
		switch item.Kind() {
		case reflect.Struct:
			if _, idx, ok := fieldByJSONName(item.Type(), field); ok {
				out = append(out, item.Field(idx).Interface())
			}
		case reflect.Map:
			for _, key := range item.MapKeys() {
				if strings.EqualFold(fmt.Sprint(key.Interface()), field) {
					out = append(out, item.MapIndex(key).Interface())
					break
				}
			}
		}
	}
	return out
}

func containsFold(list []string, s string) bool {
	for _, v := range list {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
