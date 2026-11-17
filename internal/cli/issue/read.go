package issue

import (
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"

	"github.com/maymeow/fjo/internal/api"
	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/fjoerr"
	"github.com/maymeow/fjo/internal/output"
	"github.com/spf13/cobra"
)

func newListCmd(f *app.Factory) *cobra.Command {
	var repo string
	var s api.IssueSearch
	var limit int
	var web, noPager bool
	var j *output.JSONFlags
	cmd := &cobra.Command{Use: "list", Aliases: []string{"ls"}, Short: "List issues in a repository", Args: cobra.NoArgs}
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()
		if limit < 1 {
			return fjoerr.Usage("--limit must be at least 1")
		}
		if s.State != "open" && s.State != "closed" && s.State != "all" {
			return fjoerr.Usage("invalid --state: use open, closed or all")
		}
		ref, err := f.ResolveRepo(ctx, repo)
		if err != nil {
			return err
		}
		c, _, err := f.ClientFor(ref.Host)
		if err != nil {
			return err
		}
		if s.Author == "@me" || s.Assignee == "@me" {
			u, err := c.CurrentUser(ctx)
			if err != nil {
				return err
			}
			if s.Author == "@me" {
				s.Author = u.Login
			}
			if s.Assignee == "@me" {
				s.Assignee = u.Login
			}
		}
		// Validate names because Forgejo silently drops unknown label/milestone filters.
		if _, err := c.ResolveLabelIDs(ctx, ref.Owner, ref.Name, s.Labels); err != nil {
			return err
		}
		if s.Milestone != "" {
			id, err := milestoneID(ctx, c, ref, s.Milestone)
			if err != nil {
				return err
			}
			s.Milestone = strconv.FormatInt(id, 10)
		}
		if web {
			q := url.Values{"state": {s.State}, "type": {"all"}}
			if s.Search != "" {
				q.Set("q", s.Search)
			}
			if len(s.Labels) > 0 {
				ids, err := c.ResolveLabelIDs(ctx, ref.Owner, ref.Name, s.Labels)
				if err != nil {
					return err
				}
				values := []string{}
				for _, id := range ids {
					values = append(values, strconv.FormatInt(id, 10))
				}
				q.Set("labels", strings.Join(values, ","))
			}
			if s.Milestone != "" {
				q.Set("milestone", s.Milestone)
			}
			for k, name := range map[string]string{"poster": s.Author, "assignee": s.Assignee} {
				if name != "" {
					u, err := c.GetUser(ctx, name)
					if err != nil {
						return err
					}
					q.Set(k, strconv.FormatInt(u.ID, 10))
				}
			}
			return openBrowser(f, webURL(f, ref, "issues")+"?"+q.Encode())
		}
		items, err := c.ListIssues(ctx, ref.Owner, ref.Name, s, limit)
		if err != nil {
			return err
		}
		if j.Changed() {
			out := make([]output.Issue, len(items))
			for i := range items {
				out[i] = output.NewIssue(&items[i])
			}
			return j.Write(ctx, f.IO.Out, out, jsonOptions(f))
		}
		if len(items) == 0 {
			fmt.Fprintf(f.IO.ErrOut, "no %s issues in %s\n", s.State, ref.FullName())
			return nil
		}
		table := output.Table{Headers: []string{"NUMBER", "TITLE", "LABELS", "UPDATED"}}
		for _, v := range items {
			labels := []string{}
			for _, l := range v.Labels {
				labels = append(labels, l.Name)
			}
			updated := v.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z")
			if f.IO.IsStdoutTTY() {
				updated = output.TimeAgo(v.UpdatedAt)
			}
			table.Rows = append(table.Rows, []output.Cell{output.Text(fmt.Sprintf("#%d", v.Number)), output.Text(line(v.Title)), output.Text(line(strings.Join(labels, ", "))), output.Text(updated)})
		}
		return page(ctx, f, noPager, func(w io.Writer) error {
			return table.Print(w, output.TableOptions{TTY: f.IO.IsStdoutTTY(), Width: f.IO.TerminalWidth(), Color: f.IO.ColorEnabled()})
		})
	}
	repoFlag(cmd, &repo)
	fl := cmd.Flags()
	fl.StringVarP(&s.State, "state", "s", "open", "Filter by state: open, closed or all")
	fl.StringVar(&s.Author, "author", "", "Filter by author (or @me)")
	fl.StringVarP(&s.Assignee, "assignee", "a", "", "Filter by assignee (or @me)")
	fl.StringSliceVarP(&s.Labels, "label", "l", nil, "Filter by label (repeatable)")
	fl.StringVarP(&s.Milestone, "milestone", "m", "", "Filter by milestone title or ID")
	fl.StringVarP(&s.Search, "search", "S", "", "Search issue text")
	fl.IntVarP(&limit, "limit", "L", 30, "Maximum number of issues")
	fl.BoolVarP(&web, "web", "w", false, "Open in the browser")
	fl.BoolVar(&noPager, "no-pager", false, "Disable the pager")
	j = output.AddJSONFlags(cmd, output.IssueFields)
	return cmd
}

func newViewCmd(f *app.Factory) *cobra.Command {
	var repo string
	var comments, web, noPager bool
	var j *output.JSONFlags
	cmd := &cobra.Command{Use: "view <number|url>", Short: "View an issue", Args: cobra.ExactArgs(1)}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		ref, n, err := resolve(ctx, f, repo, args[0])
		if err != nil {
			return err
		}
		if web {
			return openBrowser(f, webURL(f, ref, "issues/"+strconv.Itoa(n)))
		}
		c, _, err := f.ClientFor(ref.Host)
		if err != nil {
			return err
		}
		v, err := getIssue(ctx, c, ref, n)
		if err != nil {
			return err
		}
		if j.Changed() {
			if comments {
				return fjoerr.Usage("--comments cannot be combined with --json; the comments JSON field is a count")
			}
			return j.Write(ctx, f.IO.Out, output.NewIssue(v), jsonOptions(f))
		}
		var discussion []api.Comment
		if comments {
			discussion, err = c.ListIssueComments(ctx, ref.Owner, ref.Name, n)
			if err != nil {
				return err
			}
		}
		return page(ctx, f, noPager, func(w io.Writer) error {
			fmt.Fprintf(w, "%s #%d\n%s • %s\n\n%s\n\n%s\n", line(v.Title), v.Number, line(strings.ToUpper(v.State)), line(v.User.Login), output.Sanitize(v.Body), line(v.HTMLURL))
			for _, comment := range discussion {
				fmt.Fprintf(w, "\n%s • %s\n%s\n", line(comment.User.Login), comment.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"), output.Sanitize(comment.Body))
			}
			return nil
		})
	}
	repoFlag(cmd, &repo)
	cmd.Flags().BoolVarP(&comments, "comments", "c", false, "Show comments")
	cmd.Flags().BoolVarP(&web, "web", "w", false, "Open in the browser")
	cmd.Flags().BoolVar(&noPager, "no-pager", false, "Disable the pager")
	j = output.AddJSONFlags(cmd, output.IssueFields)
	return cmd
}
