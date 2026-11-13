package repo

import (
	"context"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/maymeow/fjo/internal/api"
	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/fjoerr"
)

type listOptions struct {
	hostname   string
	limit      int
	visibility string
	archived   bool
	noArchived bool
	fork       bool
	source     bool
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
  fjo repo list --visibility private --no-archived`,
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

	tty := f.IO.IsStdoutTTY()
	if len(repos) == 0 {
		if tty {
			fmt.Fprintf(f.IO.ErrOut, "No repositories match your search in @%s\n", who.Login)
		}
		return nil
	}
	if tty {
		if total < int64(len(repos)) {
			total = int64(len(repos))
		}
		fmt.Fprintf(f.IO.Out, "\nShowing %d of %d repositories in @%s\n\n", len(repos), total, who.Login)
		tw := tabwriter.NewWriter(f.IO.Out, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "NAME\tDESCRIPTION\tINFO\tUPDATED")
		for i := range repos {
			r := &repos[i]
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.FullName, truncate(oneLine(r.Description), 50), info(r), ago(r.UpdatedAt))
		}
		return tw.Flush()
	}
	for i := range repos {
		r := &repos[i]
		fmt.Fprintf(f.IO.Out, "%s\t%s\t%s\t%s\n", r.FullName, oneLine(r.Description), info(r), r.UpdatedAt.UTC().Format(time.RFC3339))
	}
	return nil
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

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// now is replaced in tests.
var now = time.Now

func ago(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := now().Sub(t)
	switch {
	case d < time.Minute:
		return "less than a minute ago"
	case d < time.Hour:
		return plural(int(d.Minutes()), "minute") + " ago"
	case d < 24*time.Hour:
		return plural(int(d.Hours()), "hour") + " ago"
	case d < 30*24*time.Hour:
		return plural(int(d.Hours()/24), "day") + " ago"
	}
	return t.Format("Jan 2, 2006")
}

func plural(n int, unit string) string {
	if n == 1 {
		return "about 1 " + unit
	}
	return fmt.Sprintf("about %d %ss", n, unit)
}
