package repo

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/maymeow/fjo/internal/api"
	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/fjoerr"
)

type createOptions struct {
	hostname      string
	owner         string
	description   string
	private       bool
	public        bool
	initialize    bool
	gitignore     string
	license       string
	defaultBranch string
	clone         bool
}

func newCreateCmd(f *app.Factory) *cobra.Command {
	var o createOptions
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a repository on Forgejo",
		Long: `Create a repository for the authenticated user or an organization.

<name> may be OWNER/NAME to create it under an organization. Without --private
or --public the server's default visibility is used. --initialize creates an
initial commit; --gitignore, --license and --default-branch require it.`,
		Example: `  fjo repo create demo --private
  fjo repo create my-org/demo --description "Example"
  fjo repo create demo --initialize --gitignore Go --license MIT
  fjo repo create demo --private --clone`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCreate(cmd.Context(), f, o, args[0])
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&o.hostname, "hostname", "", "Forgejo host (default: FJO_HOST, defaults.host)")
	fl.StringVar(&o.owner, "owner", "", "Organization that will own the repository")
	fl.StringVar(&o.description, "description", "", "Repository description")
	fl.BoolVar(&o.private, "private", false, "Create a private repository")
	fl.BoolVar(&o.public, "public", false, "Create a public repository")
	fl.BoolVar(&o.initialize, "initialize", false, "Create an initial commit")
	fl.StringVar(&o.gitignore, "gitignore", "", "Gitignore template for the initial commit (requires --initialize)")
	fl.StringVar(&o.license, "license", "", "License template for the initial commit (requires --initialize)")
	fl.StringVar(&o.defaultBranch, "default-branch", "", "Default branch name (requires --initialize)")
	fl.BoolVar(&o.clone, "clone", false, "Clone the new repository after creating it")
	return cmd
}

func runCreate(ctx context.Context, f *app.Factory, o createOptions, arg string) error {
	if o.private && o.public {
		return fjoerr.Usage("--private and --public cannot be used together")
	}
	if !o.initialize && (o.gitignore != "" || o.license != "" || o.defaultBranch != "") {
		return fjoerr.Usage("--gitignore, --license and --default-branch require --initialize")
	}

	owner, name := o.owner, strings.TrimSpace(arg)
	if strings.Contains(name, "/") {
		if o.owner != "" {
			return fjoerr.Usage("--owner cannot be combined with an OWNER/NAME argument")
		}
		var ok bool
		owner, name, ok = strings.Cut(name, "/")
		if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
			return fjoerr.Usage("invalid repository name %q: use NAME or OWNER/NAME", arg)
		}
	}
	if name == "" {
		return fjoerr.Usage("repository name must not be empty")
	}

	host, err := f.ResolveHost(o.hostname)
	if err != nil {
		return err
	}
	var private *bool
	switch {
	case o.private:
		v := true
		private = &v
	case o.public:
		v := false
		private = &v
	}
	client, _, err := f.ClientFor(host)
	if err != nil {
		return err
	}
	repo, err := client.CreateRepository(ctx, owner, api.CreateRepoOptions{
		Name:          name,
		Description:   o.description,
		Private:       private,
		AutoInit:      o.initialize,
		DefaultBranch: o.defaultBranch,
		Gitignores:    o.gitignore,
		License:       o.license,
	})
	if err != nil {
		switch fjoerr.KindOf(err) {
		case fjoerr.KindNotFound:
			return fjoerr.Wrap(fjoerr.KindNotFound, err, "organization %q not found on %s", owner, host)
		case fjoerr.KindConflict:
			full := name
			if owner != "" {
				full = owner + "/" + name
			}
			return fjoerr.Wrap(fjoerr.KindConflict, err, "repository %s already exists on %s", full, host)
		}
		return err
	}
	if repo.FullName == "" {
		repo.FullName = strings.TrimPrefix(owner+"/"+name, "/")
	}

	if o.clone {
		dir, err := cloneRepository(ctx, f, host, repo, "", nil)
		if err != nil {
			return fjoerr.Wrap(fjoerr.KindOf(err), err, "repository %s was created but could not be cloned", repo.FullName).
				WithHint("clone it manually with 'fjo repo clone " + repo.FullName + "'")
		}
		if f.IO.IsStderrTTY() {
			fmt.Fprintf(f.IO.ErrOut, "✓ Cloned %s to %s\n", repo.FullName, dir)
		}
	}
	reportRepo(f, "Created repository", repo)
	return nil
}
