package cli

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// allowedDuplicates lists functions ("package.name") that may share a body
// with a function in another package because they must stay separate.
// Every entry needs a comment with the reason.
var allowedDuplicates = map[string]bool{}

func TestNoDuplicateFunctions(t *testing.T) {
	root := ".."
	fset := token.NewFileSet()
	bodies := map[string][]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Body == nil {
				continue
			}
			name := file.Name.Name + "." + fn.Name.Name
			if allowedDuplicates[name] {
				continue
			}
			var b bytes.Buffer
			if err := printer.Fprint(&b, token.NewFileSet(), fn.Type); err != nil {
				return err
			}
			if err := printer.Fprint(&b, token.NewFileSet(), fn.Body); err != nil {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			bodies[b.String()] = append(bodies[b.String()], name+" ("+filepath.ToSlash(rel)+")")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, sites := range bodies {
		if len(sites) > 1 {
			sort.Strings(sites)
			t.Errorf("identical functions: %s\nmove the shared helper to internal/cli/cmdutil (commands) or internal/output (formatting), or add it to allowedDuplicates with a reason",
				strings.Join(sites, ", "))
		}
	}
}
