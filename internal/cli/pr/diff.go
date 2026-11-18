package pr

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/fjoerr"
	"github.com/maymeow/fjo/internal/output"
)

func newDiffCmd(f *app.Factory) *cobra.Command {
	var repo string
	var names, patch, noPager bool
	cmd := &cobra.Command{Use: "diff [<number|url|branch>]", Short: "View pull request changes", Args: cobra.MaximumNArgs(1)}
	repoFlag(cmd, &repo)
	cmd.Flags().BoolVar(&names, "name-only", false, "Show only changed file names")
	cmd.Flags().BoolVar(&patch, "patch", false, "Show the changes in patch format")
	cmd.Flags().BoolVar(&noPager, "no-pager", false, "Disable the pager")
	cmd.RunE = func(cmd *cobra.Command, args []string) (err error) {
		if names && patch {
			return fjoerr.Usage("--name-only and --patch cannot be combined")
		}
		ctx := cmd.Context()
		target, err := resolve(ctx, f, repo, firstArg(args))
		if err != nil {
			return err
		}
		if names {
			files, err := target.client.ListPullFiles(ctx, target.repo.Owner, target.repo.Name, target.number)
			if err != nil {
				return err
			}
			return page(ctx, f, noPager, func(w io.Writer) error {
				for _, file := range files {
					name := file.Name
					if f.IO.IsStdoutTTY() {
						name = output.Sanitize(strings.ReplaceAll(name, "\n", "\\n"))
					}
					if _, err := fmt.Fprintln(w, name); err != nil {
						return err
					}
				}
				return nil
			})
		}
		stream, err := target.client.PullDiff(ctx, target.repo.Owner, target.repo.Name, target.number, patch)
		if err != nil {
			return err
		}
		defer func() { err = fjoerr.JoinCleanup(err, stream.Close()) }()
		return page(ctx, f, noPager, func(w io.Writer) error {
			color := f.IO.ColorEnabled()
			if color || f.IO.IsStdoutTTY() {
				return printDiff(w, stream, color)
			}
			_, err := io.Copy(w, stream)
			return err
		})
	}
	return cmd
}

func printDiff(w io.Writer, stream io.Reader, color bool) error {
	reader := bufio.NewReader(stream)
	style := ""
	start := true
	var tail []byte
	for {
		// Peek at the first byte of each line to choose its color.
		if start {
			prefix, err := reader.Peek(1)
			if err != nil {
				if err == io.EOF {
					return nil
				}
				return err
			}
			style = ""
			if color {
				switch prefix[0] {
				case '+':
					style = "\x1b[32m"
				case '-':
					style = "\x1b[31m"
				case '@':
					style = "\x1b[36m"
				}
			}
			start = false
		}

		// ReadSlice bounds the allocation even when the diff has long lines.
		chunk, err := reader.ReadSlice('\n')
		if len(tail) > 0 {
			chunk = append(tail, chunk...)
			tail = nil
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			// Keep an incomplete UTF-8 rune for the next chunk.
			for i := len(chunk) - 1; i >= len(chunk)-utf8.UTFMax+1; i-- {
				if utf8.RuneStart(chunk[i]) {
					if !utf8.FullRune(chunk[i:]) {
						tail = append(tail, chunk[i:]...)
						chunk = chunk[:i]
					}
					break
				}
			}
		}
		if len(chunk) > 0 {
			text := output.Sanitize(string(chunk))
			if style != "" {
				body := strings.TrimSuffix(text, "\n")
				text = style + body + "\x1b[0m" + text[len(body):]
			}
			if _, writeErr := io.WriteString(w, text); writeErr != nil {
				return writeErr
			}
			start = chunk[len(chunk)-1] == '\n'
		}
		switch {
		case err == nil, errors.Is(err, bufio.ErrBufferFull):
		case errors.Is(err, io.EOF):
			return nil
		default:
			return err
		}
	}
}
