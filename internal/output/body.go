package output

import (
	"context"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/maymeow/fjo/internal/fjoerr"
)

// BodyOptions holds the shared body flags of issue and pull request commands.
type BodyOptions struct {
	Body     string
	BodyFile string
	Editor   bool
}

// AddBodyFlags registers --body, --body-file and --editor.
func AddBodyFlags(cmd *cobra.Command, o *BodyOptions) {
	fl := cmd.Flags()
	fl.StringVar(&o.Body, "body", "", "Body text")
	fl.StringVar(&o.BodyFile, "body-file", "", "Read the body from a file (use `-` for standard input)")
	fl.BoolVar(&o.Editor, "editor", false, "Open the editor to write the body")
}

// BodyContext supplies the inputs of BodyOptions.Read.
type BodyContext struct {
	Stdin        io.Reader
	Getenv       func(string) string
	ConfigEditor string
	Editor       EditorOptions
}

// Read returns the body from --body, --body-file, the editor, or empty. The
// caller decides whether an empty body is required or should trigger a
// prompt in interactive mode.
func (o *BodyOptions) Read(ctx context.Context, c BodyContext) (string, error) {
	if o.Body != "" && o.BodyFile != "" {
		return "", fjoerr.Usage("--body and --body-file cannot be used together")
	}
	if o.Editor && (o.Body != "" || o.BodyFile != "") {
		return "", fjoerr.Usage("--editor cannot be combined with --body or --body-file")
	}
	switch {
	case o.Body != "":
		return o.Body, nil
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
		editor := ResolveEditor(c.Getenv, c.ConfigEditor)
		return editor.Edit(ctx, "", c.Editor)
	default:
		return "", nil
	}
}
