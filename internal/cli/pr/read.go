package pr

import (
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/maymeow/fjo/internal/api"
	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/fjoerr"
	"github.com/maymeow/fjo/internal/output"
)

func newListCmd(f *app.Factory) *cobra.Command {
	var search api.PullSearch
	var repo, head string
	var labels []string
	var limit int
	var web, noPager bool
	cmd := &cobra.Command{Use: "list", Aliases: []string{"ls"}, Short: "List pull requests", Args: cobra.NoArgs}
	j := output.AddJSONFlags(cmd, output.PullRequestFields)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()
		if limit < 1 {
			return fjoerr.Usage("--limit must be at least 1")
		}
		switch search.State {
		case "open", "closed", "merged", "all":
		default:
			return fjoerr.Usage("invalid --state: use open, closed, merged or all")
		}
		if web && (j.Changed() || search.Base != "" || head != "" || search.Draft || search.State == "merged") {
			return fjoerr.Usage("--web cannot be combined with --json, --base, --head, --draft or --state merged")
		}
		s := search
		s.Head = head
		if owner, branch, ok := strings.Cut(head, ":"); ok {
			if owner == "" || branch == "" {
				return fjoerr.Usage("--head expects BRANCH or OWNER:BRANCH")
			}
			s.HeadOwner, s.Head = owner, branch
		}
		ref, err := f.ResolveRepo(ctx, repo)
		if err != nil {
			return err
		}
		client, _, err := f.ClientFor(ref.Host)
		if err != nil {
			return err
		}
		if s.Author == "@me" || s.Assignee == "@me" {
			user, err := client.CurrentUser(ctx)
			if err != nil {
				return err
			}
			if s.Author == "@me" {
				s.Author = user.Login
			}
			if s.Assignee == "@me" {
				s.Assignee = user.Login
			}
		}
		s.Labels, err = client.ResolveLabelIDs(ctx, ref.Owner, ref.Name, labels)
		if err != nil {
			return err
		}
		if web {
			q := url.Values{"state": {s.State}, "type": {"all"}}
			ids := make([]string, 0, len(s.Labels))
			for _, id := range s.Labels {
				ids = append(ids, strconv.FormatInt(id, 10))
			}
			if len(ids) > 0 {
				q.Set("labels", strings.Join(ids, ","))
			}
			for key, login := range map[string]string{"poster": s.Author, "assignee": s.Assignee} {
				if login == "" {
					continue
				}
				user, err := client.GetUser(ctx, login)
				if err != nil {
					return err
				}
				q.Set(key, strconv.FormatInt(user.ID, 10))
			}
			return openBrowser(f, pullURL(client, ref)+"?"+q.Encode())
		}
		pulls, err := client.ListPulls(ctx, ref.Owner, ref.Name, s, limit)
		if err != nil {
			return err
		}
		if j.Changed() {
			data := make([]output.PullRequest, len(pulls))
			for i := range pulls {
				data[i] = output.NewPullRequest(&pulls[i])
			}
			return j.Write(ctx, f.IO.Out, data, jsonOptions(f))
		}
		if len(pulls) == 0 {
			_, err := fmt.Fprintf(f.IO.ErrOut, "no pull requests matching the filters in %s\n", ref.FullName())
			return err
		}
		table := output.Table{Headers: []string{"ID", "TITLE", "BRANCH", "STATE", "UPDATED"}}
		for i := range pulls {
			pull := output.NewPullRequest(&pulls[i])
			state := pull.State
			if state == "open" && pull.IsDraft {
				state = "draft"
			}
			branch := pull.HeadRefName
			if owner := pulls[i].Head.Owner(); pull.IsCrossRepo && owner != "" {
				branch = owner + ":" + branch
			}
			updated := pull.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z")
			if f.IO.IsStdoutTTY() {
				updated = output.TimeAgo(pull.UpdatedAt)
			}
			table.Rows = append(table.Rows, []output.Cell{
				output.Text(fmt.Sprintf("#%d", pull.Number)), output.Text(oneLine(pull.Title)),
				output.Text(oneLine(branch)), output.Text(state), output.Text(updated),
			})
		}
		return page(ctx, f, noPager, func(w io.Writer) error {
			return table.Print(w, tableOptions(f))
		})
	}
	repoFlag(cmd, &repo)
	flags := cmd.Flags()
	flags.StringVarP(&search.State, "state", "s", "open", "Filter by state: open, closed, merged or all")
	flags.StringVarP(&search.Author, "author", "A", "", "Filter by author (or @me)")
	flags.StringVarP(&search.Assignee, "assignee", "a", "", "Filter by assignee (or @me)")
	flags.StringSliceVarP(&labels, "label", "l", nil, "Filter by label (repeatable)")
	flags.StringVarP(&search.Base, "base", "B", "", "Filter by base branch")
	flags.StringVarP(&head, "head", "H", "", "Filter by head branch or OWNER:BRANCH")
	flags.BoolVarP(&search.Draft, "draft", "d", false, "Include only draft pull requests")
	flags.IntVarP(&limit, "limit", "L", 30, "Maximum number of pull requests")
	flags.BoolVarP(&web, "web", "w", false, "Open the pull request list in the browser")
	flags.BoolVar(&noPager, "no-pager", false, "Disable the pager")
	return cmd
}

func tableOptions(f *app.Factory) output.TableOptions {
	return output.TableOptions{TTY: f.IO.IsStdoutTTY(), Width: f.IO.TerminalWidth(), Color: f.IO.ColorEnabled()}
}

func newViewCmd(f *app.Factory) *cobra.Command {
	var repo string
	var comments, web, noPager bool
	cmd := &cobra.Command{
		Use: "view [<number|url|branch>]", Short: "View a pull request", Args: cobra.MaximumNArgs(1),
	}
	j := output.AddJSONFlags(cmd, output.PullRequestFields)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		if web && (j.Changed() || comments) {
			return fjoerr.Usage("--web cannot be combined with --json or --comments")
		}
		if comments && j.Changed() {
			return fjoerr.Usage("--comments cannot be combined with --json because the JSON comments field is a count")
		}
		target, err := resolve(ctx, f, repo, firstArg(args))
		if err != nil {
			return err
		}
		if web {
			return openBrowser(f, fmt.Sprintf("%s/%d", pullURL(target.client, target.repo), target.number))
		}
		pull, err := target.read(ctx)
		if err != nil {
			return err
		}
		data := output.NewPullRequest(pull)
		if j.Changed() {
			return j.Write(ctx, f.IO.Out, data, jsonOptions(f))
		}
		var items []api.Comment
		if comments {
			items, err = target.client.ListIssueComments(ctx, target.repo.Owner, target.repo.Name, target.number)
			if err != nil {
				return err
			}
		}
		return page(ctx, f, noPager, func(w io.Writer) error {
			if _, err := fmt.Fprintf(w, "%s (#%d)\n%s · %s · %s → %s\n\n%s\n\n%s\n",
				oneLine(data.Title), data.Number, oneLine(data.State), oneLine(data.Author.Login),
				oneLine(data.HeadRefName), oneLine(data.BaseRefName), output.Sanitize(data.Body), oneLine(data.URL)); err != nil {
				return err
			}
			for _, item := range items {
				if _, err := fmt.Fprintf(w, "\n%s · %s\n%s\n", oneLine(item.User.Login),
					item.CreatedAt.Format("2006-01-02 15:04 MST"), output.Sanitize(item.Body)); err != nil {
					return err
				}
			}
			return nil
		})
	}
	repoFlag(cmd, &repo)
	cmd.Flags().BoolVarP(&comments, "comments", "c", false, "Include discussion comments")
	cmd.Flags().BoolVarP(&web, "web", "w", false, "Open the pull request in the browser")
	cmd.Flags().BoolVar(&noPager, "no-pager", false, "Disable the pager")
	return cmd
}

func firstArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}
