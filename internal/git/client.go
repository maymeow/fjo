// Package git runs the installed git executable (IMPLEMENTATION §8.3) and
// parses remote URLs (§8.1). Commands are always argument arrays, never
// shell strings, and never prompt for credentials.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// ErrNotRepository means the working directory is not inside a git work tree.
var ErrNotRepository = errors.New("not a git repository")

// ErrNotInstalled means no git executable was found.
var ErrNotInstalled = errors.New("git is not installed or not in PATH")

// ResolvedKey is the per-remote config key that records the default
// repository chosen with `fjo repo set-default` (BACKLOG B-14, mirrors gh's
// remote.<name>.gh-resolved). The value is "base" or "<host>/<owner>/<repo>".
const ResolvedKey = "fjo-resolved"

// Client runs git in Dir ("" = current directory).
type Client struct {
	Dir string
	// Path is the git executable; "" looks up "git" in PATH.
	Path string
}

// Remote is one configured git remote.
type Remote struct {
	Name     string
	FetchURL string
	PushURL  string
	// Resolved is the value of remote.<name>.fjo-resolved, if set.
	Resolved string
}

// Error is a failed git invocation. Stderr is trimmed.
type Error struct {
	Args     []string
	ExitCode int
	Stderr   string
}

func (e *Error) Error() string {
	msg := fmt.Sprintf("git %s failed (exit %d)", strings.Join(e.Args, " "), e.ExitCode)
	if e.Stderr != "" {
		msg += ": " + e.Stderr
	}
	return msg
}

// Run executes git with args and returns stdout.
func (c *Client) Run(ctx context.Context, args ...string) (string, error) {
	path := c.Path
	if path == "" {
		p, err := exec.LookPath("git")
		if err != nil {
			return "", ErrNotInstalled
		}
		path = p
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = c.Dir
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0", // never block on a credential prompt
		"LC_ALL=C",              // stable, parseable messages
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			msg := strings.TrimSpace(stderr.String())
			if strings.Contains(msg, "not a git repository") {
				return "", ErrNotRepository
			}
			return "", &Error{Args: args, ExitCode: ee.ExitCode(), Stderr: msg}
		}
		return "", fmt.Errorf("run git: %w", err)
	}
	return stdout.String(), nil
}

// TopLevel returns the root of the current work tree.
func (c *Client) TopLevel(ctx context.Context) (string, error) {
	out, err := c.Run(ctx, "rev-parse", "--show-toplevel")
	return strings.TrimSpace(out), err
}

// CurrentBranch returns the checked-out branch name; it fails on a detached
// HEAD.
func (c *Client) CurrentBranch(ctx context.Context) (string, error) {
	out, err := c.Run(ctx, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		var ge *Error
		if errors.As(err, &ge) && ge.ExitCode == 1 {
			return "", fmt.Errorf("not on a branch (detached HEAD)")
		}
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// Remotes lists remotes sorted by name, with fetch/push URLs and the
// fjo-resolved marker.
func (c *Client) Remotes(ctx context.Context) ([]Remote, error) {
	out, err := c.Run(ctx, "remote", "-v")
	if err != nil {
		return nil, err
	}
	byName := map[string]*Remote{}
	for _, line := range strings.Split(out, "\n") {
		// "<name>\t<url> (fetch|push)"
		name, rest, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		u, kind, ok := strings.Cut(strings.TrimSpace(rest), " ")
		if !ok {
			continue
		}
		r := byName[name]
		if r == nil {
			r = &Remote{Name: name}
			byName[name] = r
		}
		switch kind {
		case "(fetch)":
			r.FetchURL = u
		case "(push)":
			r.PushURL = u
		}
	}

	resolved, err := c.Run(ctx, "config", "--get-regexp", `^remote\..*\.`+ResolvedKey+`$`)
	var ge *Error
	switch {
	case errors.As(err, &ge) && ge.ExitCode == 1: // no keys set
	case err != nil:
		return nil, err
	}
	for _, line := range strings.Split(resolved, "\n") {
		key, val, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(key, "remote."), "."+ResolvedKey)
		if r := byName[name]; r != nil {
			r.Resolved = strings.TrimSpace(val)
		}
	}

	list := make([]Remote, 0, len(byName))
	for _, r := range byName {
		list = append(list, *r)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list, nil
}

// SetResolved records the default repository on remote name.
func (c *Client) SetResolved(ctx context.Context, name, value string) error {
	_, err := c.Run(ctx, "config", "--local", "remote."+name+"."+ResolvedKey, value)
	return err
}

// UnsetResolved removes the default-repository marker from every remote.
func (c *Client) UnsetResolved(ctx context.Context) error {
	remotes, err := c.Remotes(ctx)
	if err != nil {
		return err
	}
	for _, r := range remotes {
		if r.Resolved == "" {
			continue
		}
		if _, err := c.Run(ctx, "config", "--local", "--unset-all", "remote."+r.Name+"."+ResolvedKey); err != nil {
			return err
		}
	}
	return nil
}
