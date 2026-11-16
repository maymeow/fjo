package output

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/maymeow/fjo/internal/api"
	"github.com/maymeow/fjo/internal/fjoerr"
)

func jsonCmd(fields []string) (*cobra.Command, *JSONFlags) {
	cmd := &cobra.Command{
		Use:           "test",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE:          func(*cobra.Command, []string) error { return nil },
	}
	j := AddJSONFlags(cmd, fields)
	return cmd, j
}

func executeJSON(cmd *cobra.Command, args ...string) error {
	cmd.SetArgs(args)
	return cmd.Execute()
}

func TestAddJSONFlagsSelection(t *testing.T) {
	cmd, j := jsonCmd([]string{"title", "number"})
	if err := executeJSON(cmd, "--json", "title,number"); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !j.Changed() {
		t.Fatal("--json should be active")
	}
	if got := strings.Join(j.Fields(), ","); got != "title,number" {
		t.Errorf("fields = %q", got)
	}
}

func TestAddJSONFlagsBareJSON(t *testing.T) {
	cmd, _ := jsonCmd([]string{"title", "number"})
	err := executeJSON(cmd, "--json")
	if fjoerr.KindOf(err) != fjoerr.KindUsage {
		t.Fatalf("kind = %v, err = %v", fjoerr.KindOf(err), err)
	}
	for _, want := range []string{"Specify one or more", "number", "title"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
}

func TestAddJSONFlagsErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"unknown field", []string{"--json", "nope"}, "Unknown JSON field"},
		{"empty field", []string{"--json="}, "Specify one or more"},
		{"jq without json", []string{"--jq", "."}, "without --json"},
		{"template without json", []string{"--template", "{{.}}"}, "without --json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, _ := jsonCmd([]string{"title"})
			err := executeJSON(cmd, tt.args...)
			if fjoerr.KindOf(err) != fjoerr.KindUsage || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want usage containing %q", err, tt.want)
			}
		})
	}
}

func TestAddJSONFlagsWebConflict(t *testing.T) {
	cmd, _ := jsonCmd([]string{"title"})
	var web bool
	cmd.Flags().BoolVar(&web, "web", false, "")
	err := executeJSON(cmd, "--json", "title", "--web")
	if fjoerr.KindOf(err) != fjoerr.KindUsage || !strings.Contains(err.Error(), "--web") {
		t.Fatalf("err = %v", err)
	}
}

type jsonItem struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Hidden string `json:"hidden"`
}

func TestJSONWrite(t *testing.T) {
	j := &JSONFlags{active: true, fields: []string{"number", "title"}}
	var buf bytes.Buffer
	if err := j.Write(context.Background(), &buf, jsonItem{1, "one", "x"}, WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	if want := "{\"number\":1,\"title\":\"one\"}\n"; buf.String() != want {
		t.Errorf("got %q, want %q", buf.String(), want)
	}
}

func TestJSONWriteIndentedOnTTY(t *testing.T) {
	j := &JSONFlags{active: true, fields: []string{"number"}}
	var buf bytes.Buffer
	if err := j.Write(context.Background(), &buf, jsonItem{1, "one", "x"}, WriteOptions{TTY: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "\n  \"number\": 1\n") {
		t.Errorf("expected indented JSON, got %q", buf.String())
	}
}

func TestJSONWriteJQ(t *testing.T) {
	j := &JSONFlags{active: true, fields: []string{"number", "title"}, jq: ".[].title"}
	var buf bytes.Buffer
	data := []jsonItem{{1, "one", ""}, {2, "two", ""}}
	if err := j.Write(context.Background(), &buf, data, WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	if want := "one\ntwo\n"; buf.String() != want {
		t.Errorf("got %q, want %q", buf.String(), want)
	}
}

func TestJSONWriteJQErrors(t *testing.T) {
	for _, expr := range []string{".[", ".[] | .foo"} {
		j := &JSONFlags{active: true, fields: []string{"number"}, jq: expr}
		err := j.Write(context.Background(), &bytes.Buffer{}, jsonItem{1, "one", ""}, WriteOptions{})
		if fjoerr.KindOf(err) != fjoerr.KindUsage {
			t.Errorf("jq %q: kind = %v, err = %v", expr, fjoerr.KindOf(err), err)
		}
	}
}

func TestJSONWriteTemplate(t *testing.T) {
	j := &JSONFlags{
		active:   true,
		fields:   []string{"number", "title"},
		template: "{{range .}}{{.title}}\n{{end}}",
	}
	var buf bytes.Buffer
	data := []jsonItem{{1, "one", ""}, {2, "two", ""}}
	if err := j.Write(context.Background(), &buf, data, WriteOptions{TTY: true}); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "one\ntwo\n" {
		t.Errorf("got %q", buf.String())
	}
}

func TestJSONWriteTemplateTable(t *testing.T) {
	j := &JSONFlags{
		active:   true,
		fields:   []string{"number", "title"},
		template: "{{range .}}{{tablerow .number .title}}{{end}}{{tablerender}}",
	}
	var buf bytes.Buffer
	data := []jsonItem{{1, "one", ""}, {2, "two", ""}}
	if err := j.Write(context.Background(), &buf, data, WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	if want := "1\tone\n2\ttwo\n"; buf.String() != want {
		t.Errorf("got %q, want %q", buf.String(), want)
	}

	j.template = "{{range .}}{{tablerow .number .title}}{{end}}"
	err := j.Write(context.Background(), &bytes.Buffer{}, data, WriteOptions{})
	if fjoerr.KindOf(err) != fjoerr.KindUsage {
		t.Errorf("missing tablerender: err = %v", err)
	}
}

func TestJSONWriteTemplateHelpers(t *testing.T) {
	j := &JSONFlags{
		active:   true,
		fields:   []string{"topics", "authors", "when"},
		template: `{{join "," .topics}}|{{range pluck "login" .authors}}{{.}}{{end}}|{{truncate 3 "abcdef"}}|{{timeago .when}}|{{color "red" "x"}}`,
	}
	var buf bytes.Buffer
	data := struct {
		Topics  []string  `json:"topics"`
		Authors []User    `json:"authors"`
		When    time.Time `json:"when"`
	}{[]string{"a", "b"}, []User{{"alice"}, {"bob"}}, time.Now().Add(-2 * time.Hour)}
	if err := j.Write(context.Background(), &buf, data, WriteOptions{Color: false}); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	for _, want := range []string{"a,b", "alicebob", "ab…", "ago", "|x"} {
		if !strings.Contains(got, want) {
			t.Errorf("output %q missing %q", got, want)
		}
	}
	if strings.Contains(got, "\x1b[") {
		t.Errorf("color must be off: %q", got)
	}
}

func TestExportData(t *testing.T) {
	ts := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	issue := Issue{
		Number:    42,
		Title:     "Fix login",
		State:     "open",
		Author:    User{Login: "alice"},
		URL:       "https://forge/org/project/issues/42",
		Labels:    []Label{},
		Assignees: []User{},
		CreatedAt: ts,
		UpdatedAt: ts,
	}
	got, err := ExportData(issue, []string{"number", "title", "closedAt", "labels", "milestone"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"closedAt":null,"labels":[],"milestone":null,"number":42,"title":"Fix login"}`
	if string(raw) != want {
		t.Errorf("got %s, want %s", raw, want)
	}
}

func TestIssueJSONGolden(t *testing.T) {
	ts := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	issue := Issue{
		Number:    42,
		Title:     "Fix login",
		State:     "open",
		Author:    User{Login: "alice"},
		URL:       "https://forge/org/project/issues/42",
		Labels:    []Label{{Name: "bug", Color: "ff0000"}},
		Assignees: []User{{Login: "bob"}},
		Comments:  2,
		CreatedAt: ts,
		UpdatedAt: ts,
	}
	raw, err := json.Marshal(issue)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"number":42,"title":"Fix login","state":"open","author":{"login":"alice"},"url":"https://forge/org/project/issues/42","body":"","labels":[{"name":"bug","color":"ff0000"}],"milestone":null,"assignees":[{"login":"bob"}],"comments":2,"createdAt":"2026-11-16T12:00:00Z","updatedAt":"2026-11-16T12:00:00Z","closedAt":null}`
	if string(raw) != want {
		t.Errorf("issue JSON:\n got %s\nwant %s", raw, want)
	}
}

func TestPullRequestJSONGolden(t *testing.T) {
	ts := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	pr := PullRequest{
		Number:         7,
		Title:          "Add feature",
		State:          "open",
		Author:         User{Login: "alice"},
		URL:            "https://forge/org/project/pulls/7",
		BaseRefName:    "main",
		HeadRefName:    "feature",
		HeadRepository: "org/project",
		Mergeable:      "MERGEABLE",
		Labels:         []Label{},
		Assignees:      []User{},
		Reviewers:      []User{},
		CreatedAt:      ts,
		UpdatedAt:      ts,
	}
	raw, err := json.Marshal(pr)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"number":7,"title":"Add feature","state":"open","isDraft":false,"author":{"login":"alice"},"url":"https://forge/org/project/pulls/7","body":"","baseRefName":"main","headRefName":"feature","headRepository":"org/project","isCrossRepository":false,"mergeable":"MERGEABLE","labels":[],"milestone":null,"assignees":[],"reviewers":[],"comments":0,"additions":0,"deletions":0,"changedFiles":0,"createdAt":"2026-11-16T12:00:00Z","updatedAt":"2026-11-16T12:00:00Z","closedAt":null,"mergedAt":null}`
	if string(raw) != want {
		t.Errorf("pull request JSON:\n got %s\nwant %s", raw, want)
	}
}

func TestSchemaModelsGolden(t *testing.T) {
	ts := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		v    any
		want string
	}{
		{
			"comment",
			Comment{ID: 3, Body: "hi", Author: User{"alice"}, URL: "u", CreatedAt: ts, UpdatedAt: ts},
			`{"id":3,"body":"hi","author":{"login":"alice"},"url":"u","createdAt":"2026-11-16T12:00:00Z","updatedAt":"2026-11-16T12:00:00Z"}`,
		},
		{
			"review",
			Review{ID: 4, State: "APPROVED", Body: "lgtm", Author: User{"bob"}, URL: "u", SubmittedAt: &ts},
			`{"id":4,"state":"APPROVED","body":"lgtm","author":{"login":"bob"},"url":"u","submittedAt":"2026-11-16T12:00:00Z"}`,
		},
		{
			"check",
			Check{Name: "build", State: "SUCCESS", Bucket: "pass", Description: "ok", URL: "u", StartedAt: &ts, CompletedAt: &ts},
			`{"name":"build","state":"SUCCESS","bucket":"pass","description":"ok","url":"u","startedAt":"2026-11-16T12:00:00Z","completedAt":"2026-11-16T12:00:00Z"}`,
		},
		{
			"label-milestone-user",
			struct {
				Label     Label     `json:"label"`
				Milestone Milestone `json:"milestone"`
				User      User      `json:"user"`
			}{Label{"bug", "ff0000"}, Milestone{"v1", 1}, User{"alice"}},
			`{"label":{"name":"bug","color":"ff0000"},"milestone":{"title":"v1","number":1},"user":{"login":"alice"}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := json.Marshal(tt.v)
			if err != nil {
				t.Fatal(err)
			}
			if string(raw) != tt.want {
				t.Errorf("got %s\nwant %s", raw, tt.want)
			}
		})
	}
}

func TestNewRepository(t *testing.T) {
	r := NewRepository(&api.Repository{
		Name:          "project",
		FullName:      "org/project",
		Private:       true,
		Fork:          true,
		Parent:        &api.Repository{FullName: "up/project"},
		Topics:        []string{"go"},
		Stars:         3,
		OpenPulls:     4,
		HTMLURL:       "https://forge/org/project",
		DefaultBranch: "main",
	})
	if r.Name != "project" || r.NameWithOwner != "org/project" || !r.IsPrivate || !r.IsFork {
		t.Errorf("unexpected repository: %+v", r)
	}
	if r.Parent == nil || r.Parent.NameWithOwner != "up/project" {
		t.Errorf("parent = %+v", r.Parent)
	}
	if len(r.Topics) != 1 || r.Topics[0] != "go" {
		t.Errorf("topics = %v", r.Topics)
	}
}

func TestExportDataSlice(t *testing.T) {
	data := []jsonItem{{1, "one", ""}, {2, "two", ""}}
	got, err := ExportData(data, []string{"number"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(got)
	if string(raw) != `[{"number":1},{"number":2}]` {
		t.Errorf("got %s", raw)
	}
}

func TestTableTSV(t *testing.T) {
	tbl := &Table{Headers: []string{"NAME", "DESC"}, Rows: [][]Cell{
		{Text("a"), Text("hello\nworld")},
		{Text("b"), Text("x")},
	}}
	var buf bytes.Buffer
	if err := tbl.Print(&buf, TableOptions{TTY: false}); err != nil {
		t.Fatal(err)
	}
	if want := "a\thello world\nb\tx\n"; buf.String() != want {
		t.Errorf("got %q, want %q", buf.String(), want)
	}
}

func TestTableTTY(t *testing.T) {
	tbl := &Table{Headers: []string{"NAME", "DESC"}, Rows: [][]Cell{
		{Text("a"), Text("hello world")},
		{Text("b"), Text("x")},
	}}
	var buf bytes.Buffer
	if err := tbl.Print(&buf, TableOptions{TTY: true, Width: 80}); err != nil {
		t.Fatal(err)
	}
	want := "NAME  DESC\n" +
		"a     hello world\n" +
		"b     x\n"
	if buf.String() != want {
		t.Errorf("got %q, want %q", buf.String(), want)
	}
}

func TestTableUnicodeAndTruncation(t *testing.T) {
	if got := DisplayWidth("你好"); got != 4 {
		t.Errorf("DisplayWidth(你好) = %d", got)
	}
	if got := DisplayWidth("a\u0301"); got != 1 {
		t.Errorf("combining mark width = %d", got)
	}
	if got := truncateDisplay("abcdef", 4); got != "abc…" {
		t.Errorf("truncate = %q", got)
	}
	if got := truncateDisplay("你好世界", 5); got != "你好…" {
		t.Errorf("wide truncate = %q", got)
	}
	tbl := &Table{Rows: [][]Cell{{Text("你好"), Styled(ColorRed, "世界")}, {Text("a"), Text("b")}}}
	var buf bytes.Buffer
	if err := tbl.Print(&buf, TableOptions{TTY: true, Width: 80, Color: false}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "\x1b[") {
		t.Errorf("unexpected color: %q", buf.String())
	}
	if !strings.Contains(buf.String(), "你好  世界") {
		t.Errorf("alignment: %q", buf.String())
	}
	tbl = &Table{Rows: [][]Cell{{Styled(ColorRed, "x")}}}
	buf.Reset()
	if err := tbl.Print(&buf, TableOptions{TTY: true, Width: 80, Color: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "\x1b[31mx\x1b[0m") {
		t.Errorf("expected ANSI color, got %q", buf.String())
	}
	if got := Colorize("red+bold", "x", true); got != "\x1b[31m\x1b[1mx\x1b[0m" {
		t.Errorf("Colorize = %q", got)
	}
	if got := Colorize("red", "x", false); got != "x" {
		t.Errorf("Colorize disabled = %q", got)
	}
}

func TestTableFitsWidth(t *testing.T) {
	tbl := &Table{Rows: [][]Cell{{Text("1234567890"), Text("abcdefghij")}}}
	var buf bytes.Buffer
	if err := tbl.Print(&buf, TableOptions{TTY: true, Width: 12}); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n") {
		if w := DisplayWidth(line); w > 12 {
			t.Errorf("line %q is %d wide", line, w)
		}
	}
}

func TestResolvePager(t *testing.T) {
	env := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}
	tests := []struct {
		name string
		env  map[string]string
		cfg  string
		want string
	}{
		{"default", nil, "", "less -FRX"},
		{"FJO_PAGER", map[string]string{"FJO_PAGER": "cat", "PAGER": "more"}, "", "cat"},
		{"config beats PAGER", map[string]string{"PAGER": "more"}, "most", "most"},
		{"PAGER", map[string]string{"PAGER": "more"}, "", "more"},
		{"auto means default", nil, "auto", "less -FRX"},
		{"args", map[string]string{"FJO_PAGER": "less -R"}, "", "less -R"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolvePager(env(tt.env), tt.cfg).String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPagerDirectWithoutTTY(t *testing.T) {
	var buf bytes.Buffer
	called := false
	err := (Pager{Command: "cat"}).Run(context.Background(), &buf, &buf, PagerOptions{}, func(w io.Writer) error {
		called = true
		_, err := io.WriteString(w, "hello\n")
		return err
	})
	if err != nil || !called || buf.String() != "hello\n" {
		t.Fatalf("err=%v called=%v out=%q", err, called, buf.String())
	}
}

func TestResolveEditor(t *testing.T) {
	env := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}
	tests := []struct {
		name string
		env  map[string]string
		cfg  string
		want string
	}{
		{"FJO_EDITOR", map[string]string{"FJO_EDITOR": "fjo", "VISUAL": "visual", "EDITOR": "editor"}, "", "fjo"},
		{"config", map[string]string{"VISUAL": "visual", "EDITOR": "editor"}, "cfg", "cfg"},
		{"VISUAL", map[string]string{"VISUAL": "visual", "EDITOR": "editor"}, "", "visual"},
		{"EDITOR", map[string]string{"EDITOR": "editor"}, "", "editor"},
		{"args", map[string]string{"FJO_EDITOR": "code --wait"}, "", "code --wait"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveEditor(env(tt.env), tt.cfg).String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
	wantDefault := "nano"
	if runtime.GOOS == "windows" {
		wantDefault = "notepad"
	}
	if got := ResolveEditor(env(nil), "").String(); got != wantDefault {
		t.Errorf("default = %q, want %q", got, wantDefault)
	}
}

func TestEditorEdit(t *testing.T) {
	var path string
	var mode os.FileMode
	e := Editor{Command: "fake"}
	got, err := e.Edit(context.Background(), "initial", EditorOptions{
		Run: func(_ context.Context, command string, args ...string) error {
			if command != "fake" {
				t.Errorf("command = %q", command)
			}
			path = args[len(args)-1]
			info, err := os.Stat(path)
			if err != nil {
				return err
			}
			mode = info.Mode().Perm()
			return os.WriteFile(path, []byte("edited body\n"), 0o600)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "edited body\n" {
		t.Errorf("body = %q", got)
	}
	if mode != 0o600 {
		t.Errorf("mode = %o, want 600", mode)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("temporary file was not removed: %v", err)
	}
}

func TestEditorEmptyCancels(t *testing.T) {
	e := Editor{Command: "fake"}
	_, err := e.Edit(context.Background(), "", EditorOptions{
		Run: func(_ context.Context, _ string, args ...string) error {
			return os.WriteFile(args[len(args)-1], []byte("  \n"), 0o600)
		},
	})
	if fjoerr.KindOf(err) != fjoerr.KindCancelled {
		t.Fatalf("err = %v", err)
	}
}

func TestBodyOptions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "body.md")
	if err := os.WriteFile(path, []byte("from file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	c := BodyContext{Stdin: strings.NewReader("from stdin\n"), Getenv: func(string) string { return "" }}

	body, err := (&BodyOptions{Body: "inline"}).Read(ctx, c)
	if err != nil || body != "inline" {
		t.Errorf("body: %q, %v", body, err)
	}
	body, err = (&BodyOptions{BodyFile: path}).Read(ctx, c)
	if err != nil || body != "from file\n" {
		t.Errorf("body file: %q, %v", body, err)
	}
	body, err = (&BodyOptions{BodyFile: "-"}).Read(ctx, c)
	if err != nil || body != "from stdin\n" {
		t.Errorf("body stdin: %q, %v", body, err)
	}
	if _, err := (&BodyOptions{Body: "x", BodyFile: "y"}).Read(ctx, c); fjoerr.KindOf(err) != fjoerr.KindUsage {
		t.Errorf("body conflict: %v", err)
	}
	if _, err := (&BodyOptions{Body: "x", Editor: true}).Read(ctx, c); fjoerr.KindOf(err) != fjoerr.KindUsage {
		t.Errorf("editor conflict: %v", err)
	}

	var edited string
	body, err = (&BodyOptions{Editor: true}).Read(ctx, BodyContext{
		Getenv:    func(string) string { return "" },
		CanPrompt: true,
		Editor: EditorOptions{Run: func(_ context.Context, _ string, args ...string) error {
			edited = args[len(args)-1]
			return os.WriteFile(edited, []byte("editor body\n"), 0o600)
		}},
	})
	if err != nil || body != "editor body\n" {
		t.Errorf("editor: %q, %v", body, err)
	}
}

func TestBodyEmptyFlagIsSet(t *testing.T) {
	ctx := context.Background()
	newCmd := func(args ...string) *BodyOptions {
		t.Helper()
		o := &BodyOptions{}
		cmd := &cobra.Command{Use: "x", RunE: func(*cobra.Command, []string) error { return nil }}
		AddBodyFlags(cmd, o)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		return o
	}

	o := newCmd("--body", "")
	if !o.Provided() {
		t.Error(`--body "" must count as provided (clears the body in edit)`)
	}
	if body, err := o.Read(ctx, BodyContext{}); err != nil || body != "" {
		t.Errorf(`--body "": %q, %v`, body, err)
	}
	if newCmd().Provided() {
		t.Error("no body flags must not count as provided")
	}
	if _, err := newCmd("--body", "", "--body-file", "x").Read(ctx, BodyContext{}); fjoerr.KindOf(err) != fjoerr.KindUsage {
		t.Errorf(`--body "" --body-file: %v`, err)
	}
	if _, err := newCmd("--body-file", "").Read(ctx, BodyContext{}); fjoerr.KindOf(err) != fjoerr.KindUsage {
		t.Errorf(`--body-file "": %v`, err)
	}
}

func TestBodyEditorRequiresTerminal(t *testing.T) {
	ran := false
	_, err := (&BodyOptions{Editor: true}).Read(context.Background(), BodyContext{
		Getenv: func(string) string { return "" },
		Editor: EditorOptions{Run: func(context.Context, string, ...string) error {
			ran = true
			return nil
		}},
	})
	if fjoerr.KindOf(err) != fjoerr.KindUsage || ran {
		t.Errorf("err = %v, editor ran = %v", err, ran)
	}
}

func TestSanitize(t *testing.T) {
	for in, want := range map[string]string{
		"plain":                    "plain",
		"tab\tand\nnewline":        "tab\tand\nnewline",
		"x\x1b]52;c;cHduZWQ=\x07y": "x^[]52;c;cHduZWQ=^Gy",
		"cr\rdel\x7f":              "cr^Mdel^?",
		"c1\u009bcsi":              "c1\uFFFDcsi",
		"unicode ž 漢字 🌸":           "unicode ž 漢字 🌸",
	} {
		if got := Sanitize(in); got != want {
			t.Errorf("Sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTableSanitizesCells(t *testing.T) {
	tb := &Table{Headers: []string{"A"}, Rows: [][]Cell{{Text("x\x1b[2Jy")}}}
	for _, tty := range []bool{true, false} {
		var buf bytes.Buffer
		if err := tb.Print(&buf, TableOptions{TTY: tty, Width: 80}); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(buf.String(), "\x1b") || !strings.Contains(buf.String(), "x^[[2Jy") {
			t.Errorf("tty=%v: escape not neutralized: %q", tty, buf.String())
		}
	}
}

func TestJSONWriteJQRawStringSanitizedOnTTY(t *testing.T) {
	j := &JSONFlags{active: true, fields: []string{"title"}, jq: ".[].title"}
	data := []jsonItem{{1, "a\x1b]0;pwn\x07b", ""}}
	var tty, pipe bytes.Buffer
	if err := j.Write(context.Background(), &tty, data, WriteOptions{TTY: true}); err != nil {
		t.Fatal(err)
	}
	if tty.String() != "a^[]0;pwn^Gb\n" {
		t.Errorf("tty: %q", tty.String())
	}
	if err := j.Write(context.Background(), &pipe, data, WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	if pipe.String() != "a\x1b]0;pwn\x07b\n" {
		t.Errorf("pipe must keep the raw value: %q", pipe.String())
	}
}

func TestJSONWriteJQNonStringStaysJSON(t *testing.T) {
	j := &JSONFlags{active: true, fields: []string{"number", "title"}, jq: ".[0]"}
	var buf bytes.Buffer
	if err := j.Write(context.Background(), &buf, []jsonItem{{1, "one", ""}}, WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "{\"number\":1,\"title\":\"one\"}\n" {
		t.Errorf("got %q", buf.String())
	}
}
