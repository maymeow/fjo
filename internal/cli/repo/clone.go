package repo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/maymeow/fjo/internal/api"
	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/fjoerr"
	"github.com/maymeow/fjo/internal/git"
)

func newCloneCmd(f *app.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clone <repository> [<directory>] [-- <gitflags>...]",
		Short: "Clone a repository locally",
		Long: `Clone a Forgejo repository with the installed git executable.

The clone URL is chosen from the host's configured git protocol (ssh or
https; set with 'fjo auth login --git-protocol' or in config.yaml). Arguments
after -- are passed to git clone unchanged.`,
		Example: `  fjo repo clone my-org/my-project
  fjo repo clone my-org/my-project ./project
  fjo repo clone my-org/my-project -- --depth 1`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runClone(cmd.Context(), f, args)
		},
	}
	return cmd
}

func runClone(ctx context.Context, f *app.Factory, args []string) error {
	ref, err := f.ParseRepo(args[0])
	if err != nil {
		return err
	}
	rest := args[1:]
	dest := ""
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		dest = rest[0]
		rest = rest[1:]
	}

	client, _, err := f.ClientFor(ref.Host)
	if err != nil {
		return err
	}
	repo, err := client.GetRepository(ctx, ref.Owner, ref.Name)
	if err != nil {
		if fjoerr.KindOf(err) == fjoerr.KindNotFound {
			return fjoerr.New(fjoerr.KindNotFound, "repository %s not found", ref).
				WithHint("check the name and that your account can access it")
		}
		return err
	}

	dir, err := cloneRepository(ctx, f, ref.Host, repo, dest, rest)
	if err != nil {
		return err
	}
	if f.IO.IsStderrTTY() {
		fmt.Fprintf(f.IO.ErrOut, "✓ Cloned %s to %s\n", repo.FullName, dir)
	}
	return nil
}

// cloneURL picks the SSH or HTTPS URL according to the host/default git
// protocol (IMPLEMENTATION §5.3, §8.3).
func cloneURL(f *app.Factory, host string, repo *api.Repository) (string, error) {
	cfg, err := f.Config()
	if err != nil {
		return "", err
	}
	protocol := cfg.Defaults.GitProtocol
	if h := cfg.Host(host); h != nil && h.GitProtocol != "" {
		protocol = h.GitProtocol
	}
	var u string
	switch protocol {
	case "", "https":
		if repo.CloneURL == "" {
			return "", fjoerr.New(fjoerr.KindGeneral, "the server returned no HTTPS clone URL for %s", repo.FullName)
		}
		u = repo.CloneURL
	case "ssh":
		if repo.SSHURL == "" {
			return "", fjoerr.New(fjoerr.KindGeneral, "the server returned no SSH clone URL for %s", repo.FullName)
		}
		u = repo.SSHURL
	default:
		return "", fjoerr.Usage("invalid git protocol %q for %s: use ssh or https", protocol, host)
	}
	if err := checkCloneURL(u); err != nil {
		return "", fjoerr.New(fjoerr.KindGeneral, "refusing clone URL %q returned by %s: %v", u, host, err)
	}
	return u, nil
}

// checkCloneURL accepts only network URLs git cannot mistake for an option
// or a special transport. Clone URLs come from the server and are therefore
// untrusted: a value like "--config=core.sshCommand=..." would otherwise run
// a local command (see also the "--" in cloneRepository).
func checkCloneURL(u string) error {
	if strings.HasPrefix(u, "-") {
		return fmt.Errorf("it looks like a command-line option")
	}
	if strings.IndexFunc(u, func(r rune) bool { return r <= ' ' || r == 0x7f }) >= 0 {
		return fmt.Errorf("it contains whitespace or control characters")
	}
	if i := strings.Index(u, "::"); i >= 0 && !strings.Contains(u[:i], "/") && !strings.Contains(u[:i], "@") {
		return fmt.Errorf("git remote helper transports (<helper>::) are not allowed")
	}
	p, err := git.ParseURL(u)
	if err != nil {
		return err
	}
	switch p.Scheme {
	case "https", "http", "ssh":
		return nil
	}
	return fmt.Errorf("unsupported scheme %q", p.Scheme)
}

// checkDirName validates a directory name derived from the server (the
// repository name): one path element that git cannot read as an option or a
// remote.
func checkDirName(name string) error {
	switch {
	case name == "", name == ".", name == "..":
		return fmt.Errorf("empty or relative name")
	case strings.HasPrefix(name, "-"):
		return fmt.Errorf("it starts with '-'")
	case strings.ContainsAny(name, `/\:`):
		return fmt.Errorf("it contains a path separator or ':'")
	case strings.IndexFunc(name, func(r rune) bool { return r < ' ' || r == 0x7f }) >= 0:
		return fmt.Errorf("it contains control characters")
	}
	return nil
}

// cloneRepository runs git clone and returns the destination directory.
func cloneRepository(ctx context.Context, f *app.Factory, host string, repo *api.Repository, dest string, gitArgs []string) (string, error) {
	u, err := cloneURL(f, host, repo)
	if err != nil {
		return "", err
	}
	if dest == "" {
		if err := checkDirName(repo.Name); err != nil {
			return "", fjoerr.New(fjoerr.KindGeneral, "cannot use repository name %q from the server as a directory: %v", repo.Name, err).
				WithHint("pass the destination directory explicitly")
		}
		dest = repo.Name
	}
	// "--" ends option parsing: the URL and directory are never read as git
	// options, whatever the server returned.
	args := []string{"clone"}
	args = append(args, gitArgs...)
	args = append(args, "--", u, dest)
	if err := f.Git().RunWithIO(ctx, f.IO.Out, f.IO.ErrOut, args...); err != nil {
		return "", fjoerr.Wrap(fjoerr.KindGeneral, err, "cannot clone %s", repo.FullName).
			WithHint("check that git can access " + u)
	}
	if !filepath.IsAbs(dest) {
		base := f.GitDir
		if base == "" {
			var err error
			if base, err = os.Getwd(); err != nil {
				return dest, nil
			}
		}
		return filepath.Join(base, dest), nil
	}
	return dest, nil
}
