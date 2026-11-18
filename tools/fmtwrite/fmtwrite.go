// Package fmtwrite reports fmt.Fprint, fmt.Fprintf and fmt.Fprintln calls
// whose destination is a file or a network connection.
//
// errcheck is configured to ignore fmt.Fprint* so that ordinary CLI output to
// stdout/stderr stays readable. That exclusion would also hide failed writes
// to files and sockets, so those destinations must use Write, io.Copy or a
// bufio.Writer (with a checked Flush) instead, where errcheck still applies.
package fmtwrite

import (
	"go/ast"
	"go/types"

	"github.com/golangci/plugin-module-register/register"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/types/typeutil"
)

func init() {
	register.Plugin("fmtwrite", New)
}

// New is the golangci-lint module plugin constructor. The linter has no
// settings.
func New(any) (register.LinterPlugin, error) {
	return plugin{}, nil
}

type plugin struct{}

func (plugin) BuildAnalyzers() ([]*analysis.Analyzer, error) {
	return []*analysis.Analyzer{Analyzer}, nil
}

func (plugin) GetLoadMode() string {
	return register.LoadModeTypesInfo
}

// Analyzer is the fmtwrite analysis pass.
var Analyzer = &analysis.Analyzer{
	Name:     "fmtwrite",
	Doc:      "forbids fmt.Fprint* to files and network connections; use Write, io.Copy or bufio.Writer so errcheck sees the error",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

var fprintFuncs = map[string]bool{
	"Fprint":   true,
	"Fprintf":  true,
	"Fprintln": true,
}

func run(pass *analysis.Pass) (any, error) {
	ins, ok := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !ok {
		return nil, nil
	}

	ins.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return
		}
		fn, ok := typeutil.Callee(pass.TypesInfo, call).(*types.Func)
		if !ok || fn.Pkg() == nil || fn.Pkg().Path() != "fmt" || !fprintFuncs[fn.Name()] {
			return
		}
		if dest := forbiddenDest(pass, call.Args[0]); dest != "" {
			pass.Reportf(call.Pos(),
				"fmt.%s writes to %s; use Write, io.Copy or bufio.Writer (with a checked Flush) so errcheck catches write errors",
				fn.Name(), dest)
		}
	})
	return nil, nil
}

// forbiddenDest describes the destination when it is a file or a network
// connection, and returns "" for anything else (buffers, builders,
// bufio.Writer, plain io.Writer interfaces, os.Stdout/os.Stderr).
func forbiddenDest(pass *analysis.Pass, e ast.Expr) string {
	t := pass.TypesInfo.TypeOf(e)
	if t == nil {
		return ""
	}
	switch {
	case isOSFile(t):
		if isStdStream(pass, e) {
			return ""
		}
		return "a file (*os.File)"
	case hasMethods(t, "Write", "RemoteAddr", "SetDeadline"):
		return "a network connection (net.Conn)"
	case hasMethods(t, "Write", "Header", "WriteHeader"):
		return "an HTTP response (http.ResponseWriter)"
	}
	return ""
}

func isOSFile(t types.Type) bool {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj.Pkg() != nil && obj.Pkg().Path() == "os" && obj.Name() == "File"
}

// isStdStream reports whether e is literally os.Stdout or os.Stderr.
func isStdStream(pass *analysis.Pass, e ast.Expr) bool {
	sel, ok := ast.Unparen(e).(*ast.SelectorExpr)
	if !ok {
		return false
	}
	v, ok := pass.TypesInfo.Uses[sel.Sel].(*types.Var)
	if !ok || v.Pkg() == nil || v.Pkg().Path() != "os" {
		return false
	}
	return v.Name() == "Stdout" || v.Name() == "Stderr"
}

// hasMethods reports whether t's method set (including pointer receivers and
// interface methods) contains every named method.
func hasMethods(t types.Type, names ...string) bool {
	for _, name := range names {
		obj, _, _ := types.LookupFieldOrMethod(t, true, nil, name)
		if _, ok := obj.(*types.Func); !ok {
			return false
		}
	}
	return true
}
