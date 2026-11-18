package pr

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/maymeow/fjo/internal/api"
	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/cli/cmdutil"
	"github.com/maymeow/fjo/internal/fjoerr"
	"github.com/maymeow/fjo/internal/output"
)

var mergeMethods = []string{"merge", "squash", "rebase", "rebase-merge", "fast-forward-only"}

func newMergeCmd(f *app.Factory) *cobra.Command {
	var repo, subject string
	var auto, deleteBranch bool
	var body output.BodyOptions
	var methods [5]bool
	cmd := &cobra.Command{Use: "merge [<number|url|branch>]", Short: "Merge a pull request", Args: cobra.MaximumNArgs(1)}
	cmdutil.AddRepoFlag(cmd, &repo)
	flags := cmd.Flags()
	for i, method := range mergeMethods {
		flags.BoolVar(&methods[i], method, false, "Merge using "+method)
	}
	flags.BoolVar(&auto, "auto", false, "Merge when checks succeed")
	flags.BoolVarP(&deleteBranch, "delete-branch", "d", false, "Delete the head branch after merging")
	flags.StringVar(&subject, "subject", "", "Merge commit subject")
	output.AddBodyFlags(cmd, &body)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		var method string
		for i, enabled := range methods {
			if enabled {
				if method != "" {
					return fjoerr.Usage("choose only one merge method")
				}
				method = mergeMethods[i]
			}
		}
		if method == "" && !f.IO.CanPrompt() {
			return fjoerr.Usage("choose a merge method: --%s", strings.Join(mergeMethods, ", --"))
		}
		ctx := cmd.Context()
		target, pull, err := readTarget(ctx, f, repo, firstArg(args))
		if err != nil {
			return err
		}
		if pull.IsMerged() || pull.State == "closed" || pull.IsDraft() {
			return fjoerr.New(fjoerr.KindConflict, "pull request #%d is merged, closed or a draft", target.number)
		}
		settings, err := target.client.GetRepository(ctx, target.repo.Owner, target.repo.Name)
		if err != nil {
			return err
		}
		allowed := allowedMergeMethods(settings)
		if len(allowed) == 0 {
			return fjoerr.New(fjoerr.KindUnsupported, "%s has no enabled merge methods", target.repo.FullName())
		}
		if method == "" {
			index, err := f.Prompter.Select("Merge method:", allowed)
			if err != nil {
				return err
			}
			method = allowed[index]
		}
		if !slices.Contains(allowed, method) {
			return fjoerr.New(fjoerr.KindUnsupported, "merge method %q is disabled; allowed methods: %s", method, strings.Join(allowed, ", "))
		}
		if !auto && pull.Mergeable != nil && !*pull.Mergeable {
			return fjoerr.New(fjoerr.KindConflict, "pull request #%d is not mergeable at %s", target.number, pull.Head.SHA)
		}
		text, err := readBody(ctx, f, &body)
		if err != nil {
			return err
		}
		err = target.client.MergePull(ctx, target.repo.Owner, target.repo.Name, target.number, api.MergePullOptions{
			Method: method, Subject: subject, Body: text, HeadSHA: pull.Head.SHA, Auto: auto, DeleteBranch: deleteBranch,
		})
		if err != nil {
			var apiErr *api.APIError
			if errors.As(err, &apiErr) && (apiErr.StatusCode == http.StatusMethodNotAllowed || apiErr.StatusCode == http.StatusBadRequest) {
				return fjoerr.Wrap(fjoerr.KindConflict, err, "could not merge pull request #%d", target.number)
			}
			return err
		}
		if auto {
			fmt.Fprintf(f.IO.ErrOut, "Merge requested for pull request #%d\n", target.number)
			return nil
		}
		if deleteBranch {
			deleteLocalHead(ctx, f, target.repo, pull)
		}
		fmt.Fprintf(f.IO.ErrOut, "Merged pull request #%d\n", target.number)
		return nil
	}
	return cmd
}

func allowedMergeMethods(settings *api.Repository) []string {
	var allowed []string
	for i, enabled := range []bool{settings.AllowMergeCommits, settings.AllowSquashMerge, settings.AllowRebase, settings.AllowRebaseExplicit, settings.AllowFastForwardOnly} {
		if enabled {
			allowed = append(allowed, mergeMethods[i])
		}
	}
	return allowed
}
