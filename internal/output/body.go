package output

import (
	"context"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/maymeow/fjo/internal/fjoerr"
)

// BodyOptions holds the shared body flags of issue and pull request commands.
type BodyOptions struct {
	Body     string
	BodyFile string
	Editor   bool

	// BodySet records that --body was given, even as --body "" (needed to
	// clear a body with `edit`). AddBodyFlags fills it from the flag set;
	// code that builds BodyOptions directly sets it itself.
	BodySet bool

	flags *pflag.FlagSet
}

// AddBodyFlags registers --body, --body-file and --editor.
func AddBodyFlags(cmd *cobra.Command, o *BodyOptions) {
	fl := cmd.Flags()
	fl.StringVar(&o.Body, "body", "", "Body text")
	fl.StringVar(&o.BodyFile, "body-file", "", "Read the body from a file (use `-` for standard input)")
	fl.BoolVar(&o.Editor, "editor", false, "Open the editor to write the body")
	o.flags = fl
}

func (o *BodyOptions) bodySet() bool {
	return o.BodySet || o.Body != "" || (o.flags != nil && o.flags.Changed("body"))
}

func (o *BodyOptions) bodyFileSet() bool {
	return o.BodyFile != "" || (o.flags != nil && o.flags.Changed("body-file"))
}

// Provided reports whether any body source was given (--body, even empty,
// --body-file or --editor). Commands use it to tell "keep the current body"
// from "set the body", e.g. in `issue edit`.
func (o *BodyOptions) Provided() bool {
	return o.bodySet() || o.bodyFileSet() || o.Editor
}

// BodyContext supplies the inputs of BodyOptions.Read.
type BodyContext struct {
	Stdin        io.Reader
	Getenv       func(string) string
	ConfigEditor string
	Editor       EditorOptions
	// CanPrompt must be true for --editor; without a terminal the editor
	// would wait for input that never comes (§11.1).
	CanPrompt bool
}

// Read returns the body from --body, --body-file, the editor, or empty. The
// caller decides whether an empty body is required or should trigger a
// prompt in interactive mode.
func (o *BodyOptions) Read(ctx context.Context, c BodyContext) (string, error) {
	bodySet, fileSet := o.bodySet(), o.bodyFileSet()
	if bodySet && fileSet {
		return "", fjoerr.Usage("--body and --body-file cannot be used together")
	}
	if o.Editor && (bodySet || fileSet) {
		return "", fjoerr.Usage("--editor cannot be combined with --body or --body-file")
	}
	switch {
	case bodySet:
		return o.Body, nil
	case fileSet && o.BodyFile == "":
		return "", fjoerr.Usage("--body-file requires a file name (use `-` for standard input)")
	case o.BodyFile == "-":
		if c.Stdin == nil {
			return "", fjoerr.Usage("--body-file - requires standard input")
		}
		data, err := io.ReadAll(c.Stdin)
		if err != nil {
			return "", fjoerr.Wrap(fjoerr.KindGeneral, err, "read body from standard input")
		}
		return string(data), nil
	case o.BodyFile != "":
		data, err := os.ReadFile(o.BodyFile)
		if err != nil {
			return "", fjoerr.Wrap(fjoerr.KindGeneral, err, "read body file")
		}
		return string(data), nil
	case o.Editor:
		if !c.CanPrompt {
			return "", fjoerr.Usage("--editor requires a terminal; use --body or --body-file")
		}
		editor := ResolveEditor(c.Getenv, c.ConfigEditor)
		return editor.Edit(ctx, "", c.Editor)
	default:
		return "", nil
	}
}
