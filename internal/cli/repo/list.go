package repo

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/maymeow/fjo/internal/api"
	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/cli/cmdutil"
	"github.com/maymeow/fjo/internal/fjoerr"
	"github.com/maymeow/fjo/internal/output"
)

type listOptions struct {
	hostname   string
	limit      int
	visibility string
	archived   bool
	noArchived bool
	fork       bool
	source     bool
	noPager    bool
	json       *output.JSONFlags
}

func newListCmd(f *app.Factory) *cobra.Command {
	var o listOptions
	cmd := &cobra.Command{
		Use:   "list [<owner>]",
		Short: "List repositories owned by a user or organization",
		Long: `List repositories owned by a user or organization, most recently updated
first. Without an owner, the repositories of the authenticated user are listed.`,
		Example: `  fjo repo list
  fjo repo list my-org --limit 100
  fjo repo list --visibility private --no-archived
  fjo repo list --json nameWithOwner,description,updatedAt`,
		Aliases: []string{"ls"},
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			owner := ""
			if len(args) > 0 {
				owner = args[0]
			}
			return runList(cmd.Context(), f, o, owner)
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&o.hostname, "hostname", "", "Forgejo host (default: FJO_HOST, defaults.host)")
	fl.IntVarP(&o.limit, "limit", "L", 30, "Maximum number of repositories to list")
	fl.StringVar(&o.visibility, "visibility", "", `Filter by visibility: "public" or "private"`)
	fl.BoolVar(&o.archived, "archived", false, "Show only archived repositories")
	fl.BoolVar(&o.noArchived, "no-archived", false, "Omit archived repositories")
	fl.BoolVar(&o.fork, "fork", false, "Show only forks")
	fl.BoolVar(&o.source, "source", false, "Show only non-forks")
	fl.BoolVar(&o.noPager, "no-pager", false, "Disable the pager")
	o.json = output.AddJSONFlags(cmd, output.RepositoryFields)
	return cmd
}

func runList(ctx context.Context, f *app.Factory, o listOptions, owner string) error {
	if o.limit < 1 {
		return fjoerr.Usage("invalid --limit %d: must be at least 1", o.limit)
	}
	if o.archived && o.noArchived {
		return fjoerr.Usage("--archived and --no-archived cannot be used together")
	}
	if o.fork && o.source {
		return fjoerr.Usage("--fork and --source cannot be used together")
	}
	search := api.RepoSearch{Sort: "updated", Order: "desc"}
	switch o.visibility {
	case "":
	case "public", "private":
		v := o.visibility == "private"
		search.Private = &v
	default:
		return fjoerr.Usage(`invalid --visibility %q: use "public" or "private"`, o.visibility)
	}
	switch {
	case o.archived:
		v := true
		search.Archived = &v
	case o.noArchived:
		v := false
		search.Archived = &v
	}
	switch {
	case o.fork:
		search.Mode = "fork"
	case o.source:
		search.Mode = "source"
	}

	host, err := f.ResolveHost(o.hostname)
	if err != nil {
		return err
	}
	client, _, err := f.ClientFor(host)
	if err != nil {
		return err
	}
	var who *api.User
	if owner == "" {
		who, err = client.CurrentUser(ctx)
	} else {
		who, err = client.GetUser(ctx, owner)
		if fjoerr.KindOf(err) == fjoerr.KindNotFound {
			return fjoerr.New(fjoerr.KindNotFound, "user or organization %q not found on %s", owner, host)
		}
	}
	if err != nil {
		return err
	}
	search.OwnerID = who.ID

	repos, total, err := client.SearchRepositories(ctx, search, o.limit)
	if err != nil {
		return err
	}

	if o.json.Changed() {
		items := make([]output.Repository, len(repos))
		for i := range repos {
			items[i] = output.NewRepository(&repos[i])
		}
		return o.json.Write(ctx, f.IO.Out, items, output.WriteOptions{
			TTY:   f.IO.IsStdoutTTY(),
			Color: f.IO.ColorEnabled(),
			Width: f.IO.TerminalWidth(),
		})
	}

	tty := f.IO.IsStdoutTTY()
	if len(repos) == 0 {
		if tty {
			fmt.Fprintf(f.IO.ErrOut, "No repositories match your search in @%s\n", who.Login)
		}
		return nil
	}
	if total < int64(len(repos)) {
		total = int64(len(repos))
	}

	rows := make([][]output.Cell, len(repos))
	for i := range repos {
		r := &repos[i]
		updated := r.UpdatedAt.UTC().Format(time.RFC3339)
		if tty {
			updated = output.TimeAgo(r.UpdatedAt)
		}
		rows[i] = []output.Cell{
			output.Text(r.FullName),
			output.Text(cmdutil.OneLine(r.Description)),
			output.Text(info(r)),
			output.Text(updated),
		}
	}
	table := &output.Table{Headers: []string{"NAME", "DESCRIPTION", "INFO", "UPDATED"}, Rows: rows}
	write := func(w io.Writer) error {
		if tty {
			fmt.Fprintf(w, "\nShowing %d of %d repositories in @%s\n\n", len(repos), total, who.Login)
		}
		return table.Print(w, output.TableOptions{
			TTY:   tty,
			Width: f.IO.TerminalWidth(),
			Color: f.IO.ColorEnabled(),
		})
	}
	pager := output.ResolvePager(f.Getenv, configPager(f))
	return pager.Run(ctx, f.IO.Out, f.IO.ErrOut, output.PagerOptions{Disabled: o.noPager}, write)
}

func configPager(f *app.Factory) string {
	cfg, err := f.Config()
	if err != nil || cfg == nil {
		return ""
	}
	return cfg.Defaults.Pager
}

func info(r *api.Repository) string {
	parts := []string{visibility(r)}
	if r.Fork {
		parts = append(parts, "fork")
	}
	if r.Mirror {
		parts = append(parts, "mirror")
	}
	if r.Archived {
		parts = append(parts, "archived")
	}
	return strings.Join(parts, ", ")
}
