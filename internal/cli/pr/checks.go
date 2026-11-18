package pr

import (
	"github.com/spf13/cobra"

	"github.com/maymeow/fjo/internal/api"
	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/cli/cmdutil"
	"github.com/maymeow/fjo/internal/fjoerr"
	"github.com/maymeow/fjo/internal/output"
)

func newChecksCmd(f *app.Factory) *cobra.Command {
	var repo string
	cmd := &cobra.Command{Use: "checks [<number|url|branch>]", Short: "Show checks for a pull request", Args: cobra.MaximumNArgs(1)}
	j := output.AddJSONFlags(cmd, output.CheckFields)
	cmdutil.AddRepoFlag(cmd, &repo)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		target, err := resolve(ctx, f, repo, firstArg(args))
		if err != nil {
			return err
		}
		pull, err := target.read(ctx)
		if err != nil {
			return err
		}
		sha := pull.Head.SHA
		if sha == "" {
			return fjoerr.New(fjoerr.KindGeneral, "pull request #%d has no head SHA", target.number)
		}
		items, err := target.client.ListCommitStatuses(ctx, target.repo.Owner, target.repo.Name, sha)
		if err != nil {
			return err
		}
		checks := make([]output.Check, 0, len(items))
		table := output.Table{Headers: []string{"NAME", "STATE", "DESCRIPTION", "URL"}}
		var failed, pending bool
		for _, status := range latestStatuses(items) {
			check := output.NewCheck(status)
			checks = append(checks, check)
			failed = failed || check.Bucket == "fail"
			pending = pending || check.Bucket == "pending"
			table.Rows = append(table.Rows, []output.Cell{
				output.Text(cmdutil.OneLine(check.Name)), output.Text(cmdutil.OneLine(check.State)),
				output.Text(cmdutil.OneLine(check.Description)), output.Text(cmdutil.OneLine(check.URL)),
			})
		}
		switch {
		case j.Changed():
			err = j.Write(ctx, f.IO.Out, checks, cmdutil.JSONOptions(f))
		case len(checks) == 0:
			return fjoerr.New(fjoerr.KindGeneral, "no checks reported for pull request #%d", target.number)
		default:
			err = table.Print(f.IO.Out, cmdutil.TableOptions(f))
		}
		if err != nil {
			return err
		}
		if failed || len(checks) == 0 {
			return fjoerr.Silent(fjoerr.KindGeneral)
		}
		if pending {
			return fjoerr.Silent(fjoerr.KindPending)
		}
		return nil
	}
	return cmd
}

// Forgejo can return previous attempts for a context. Only the latest counts.
func latestStatuses(items []api.CommitStatus) []api.CommitStatus {
	out := make([]api.CommitStatus, 0, len(items))
	index := make(map[string]int, len(items))
	for _, status := range items {
		name := status.Context
		if i, ok := index[name]; ok {
			if status.ID > out[i].ID {
				out[i] = status
			}
			continue
		}
		index[name] = len(out)
		out = append(out, status)
	}
	return out
}
