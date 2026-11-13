package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/maymeow/fjo/internal/config"
	"github.com/maymeow/fjo/internal/fjoerr"
	"github.com/maymeow/fjo/internal/git"
	"github.com/maymeow/fjo/internal/reporef"
)

// Git returns a git client for the working directory (GitDir in tests).
func (f *Factory) Git() *git.Client { return &git.Client{Dir: f.GitDir} }

// RemoteRepo is a git remote that points to a Forgejo repository.
type RemoteRepo struct {
	Remote git.Remote
	Repo   reporef.Ref
}

// ParseRepo parses a repository argument; OWNER/REPO uses the host from
// FJO_HOST, defaults.host or the only configured host.
func (f *Factory) ParseRepo(s string) (reporef.Ref, error) {
	cfg, err := f.Config()
	if err != nil {
		return reporef.Ref{}, err
	}
	ref, err := reporef.Parse(s, cfg, func() (string, error) { return f.ResolveHost("") })
	if err != nil {
		var fe *fjoerr.Error
		if errors.As(err, &fe) {
			return reporef.Ref{}, err
		}
		return reporef.Ref{}, fjoerr.Usage("%v", err)
	}
	return ref, nil
}

// ResolveRepo selects the repository a command works on (IMPLEMENTATION
// §8.2): the -R/--repo flag, then FJO_REPO, then the default set with
// `fjo repo set-default`, then the git remotes pointing to a known Forgejo
// host. Several different candidate repositories are an error, never a
// silent guess; `origin` has no special meaning.
func (f *Factory) ResolveRepo(ctx context.Context, flag string) (reporef.Ref, error) {
	if flag != "" {
		return f.ParseRepo(flag)
	}
	if env := f.Getenv("FJO_REPO"); env != "" {
		ref, err := f.ParseRepo(env)
		if err != nil {
			return reporef.Ref{}, fjoerr.Usage("invalid FJO_REPO: %v", err)
		}
		return ref, nil
	}

	remotes, err := f.Git().Remotes(ctx)
	if err != nil {
		return reporef.Ref{}, GitContextError(err)
	}
	if ref, ok, err := f.resolvedDefault(remotes); ok || err != nil {
		return ref, err
	}

	candidates, err := f.RemoteRepos(ctx, remotes)
	if err != nil {
		return reporef.Ref{}, err
	}
	unique := DistinctRepos(candidates)
	switch len(unique) {
	case 0:
		return reporef.Ref{}, fjoerr.Usage("no git remote points to a known Forgejo host").
			WithHint(remoteHint(remotes))
	case 1:
		return unique[0].Repo, nil
	}
	names := make([]string, len(unique))
	for i, c := range unique {
		names[i] = fmt.Sprintf("%s (%s)", c.Repo, c.Remote.Name)
	}
	return reporef.Ref{}, fjoerr.Usage("several remotes point to different repositories: %s", strings.Join(names, ", ")).
		WithHint("specify OWNER/REPO (argument or --repo) or choose a default with 'fjo repo set-default'")
}

// RemoteRepos maps git remotes to repositories on known Forgejo hosts
// (configured hosts and FJO_HOST). Remotes on other hosts (GitHub, a local
// path, ...) are ignored. remotes may be nil to read them from git.
func (f *Factory) RemoteRepos(ctx context.Context, remotes []git.Remote) ([]RemoteRepo, error) {
	if remotes == nil {
		var err error
		if remotes, err = f.Git().Remotes(ctx); err != nil {
			return nil, GitContextError(err)
		}
	}
	cfg, err := f.knownHosts()
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, id := range cfg.HostIDs() {
		known[id] = true
	}

	var out []RemoteRepo
	for _, r := range remotes {
		raw := r.FetchURL
		if raw == "" {
			raw = r.PushURL
		}
		u, err := git.ParseURL(raw)
		if err != nil {
			f.Logger.Debug("skipping remote", "remote", r.Name, "err", err)
			continue
		}
		ref, err := reporef.FromURL(u, cfg)
		if err != nil {
			f.Logger.Debug("skipping remote", "remote", r.Name, "err", err)
			continue
		}
		if !known[ref.Host] {
			continue
		}
		out = append(out, RemoteRepo{Remote: r, Repo: ref})
	}
	return out, nil
}

// knownHosts returns the configured hosts plus the FJO_HOST host, so remote
// matching (including SSH hostname matching) treats an environment-selected
// host like a configured one. The user configuration is not modified.
func (f *Factory) knownHosts() (*config.Config, error) {
	cfg, err := f.Config()
	if err != nil {
		return nil, err
	}
	env := f.Getenv("FJO_HOST")
	if env == "" {
		return cfg, nil
	}
	id, err := config.NormalizeHost(env)
	if err != nil || cfg.Host(id) != nil {
		return cfg, nil
	}
	merged := &config.Config{Hosts: map[string]*config.Host{id: {}}}
	if cfg != nil {
		for k, v := range cfg.Hosts {
			merged.Hosts[k] = v
		}
	}
	return merged, nil
}

// resolvedDefault applies remote.<name>.fjo-resolved.
func (f *Factory) resolvedDefault(remotes []git.Remote) (reporef.Ref, bool, error) {
	cfg, err := f.knownHosts()
	if err != nil {
		return reporef.Ref{}, false, err
	}
	for _, r := range remotes {
		switch r.Resolved {
		case "":
			continue
		case "base":
			u, err := git.ParseURL(r.FetchURL)
			if err == nil {
				var ref reporef.Ref
				if ref, err = reporef.FromURL(u, cfg); err == nil {
					return ref, true, nil
				}
			}
			return reporef.Ref{}, true, fjoerr.Usage("default remote %q is not a Forgejo repository: %v", r.Name, err).
				WithHint("run 'fjo repo set-default' again")
		default:
			ref, err := reporef.Parse(r.Resolved, cfg, func() (string, error) { return f.ResolveHost("") })
			if err != nil {
				return reporef.Ref{}, true, fjoerr.Usage("invalid default repository %q on remote %q: %v", r.Resolved, r.Name, err).
					WithHint("run 'fjo repo set-default' again")
			}
			return ref, true, nil
		}
	}
	return reporef.Ref{}, false, nil
}

// DistinctRepos keeps the first remote of each repository, ordered by
// remote name.
func DistinctRepos(list []RemoteRepo) []RemoteRepo {
	var out []RemoteRepo
next:
	for _, c := range list {
		for _, o := range out {
			if o.Repo.Equal(c.Repo) {
				continue next
			}
		}
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Remote.Name < out[j].Remote.Name })
	return out
}

func remoteHint(remotes []git.Remote) string {
	if len(remotes) == 0 {
		return "this repository has no remotes; specify OWNER/REPO (argument or --repo)"
	}
	return "log in to the remote's host with 'fjo auth login', or specify OWNER/REPO (argument or --repo)"
}

// GitContextError turns "not a repository" / "git missing" into usage
// errors with a hint, since --repo makes git unnecessary.
func GitContextError(err error) error {
	switch {
	case errors.Is(err, git.ErrNotRepository):
		return fjoerr.Usage("not in a git repository").
			WithHint("run the command inside a clone or specify OWNER/REPO (argument or --repo)")
	case errors.Is(err, git.ErrNotInstalled):
		return fjoerr.Usage("git is not installed").
			WithHint("install git or specify OWNER/REPO (argument or --repo)")
	}
	return fjoerr.Wrap(fjoerr.KindGeneral, err, "cannot read git remotes")
}
