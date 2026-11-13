package auth

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/cli/cmdutil"
	"github.com/maymeow/fjo/internal/config"
	"github.com/maymeow/fjo/internal/credentials"
	"github.com/maymeow/fjo/internal/fjoerr"
)

type statusOptions struct {
	hostname  string
	showToken bool
	active    bool
}

func newStatusCmd(f *app.Factory) *cobra.Command {
	var o statusOptions
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show authentication status",
		Long: `Show the accounts fjo is logged in to and verify each token with the
server. Exits with status 1 if any token is invalid or a host cannot be
reached.`,
		Example: `  fjo auth status
  fjo auth status --hostname git.example.com --active`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runStatus(cmd.Context(), f, o)
		},
	}
	cmdutil.AddHostnameFlag(cmd, &o.hostname, true)
	cmd.Flags().BoolVarP(&o.showToken, "show-token", "t", false, "Display the token (sensitive)")
	cmd.Flags().BoolVarP(&o.active, "active", "a", false, "Show only the active account of each host")
	return cmd
}

type accountLine struct {
	host, user string
	source     credentials.Source
	token      string
	active     bool
	err        error
}

func runStatus(ctx context.Context, f *app.Factory, o statusOptions) error {
	cfg, err := f.Config()
	if err != nil {
		return err
	}

	filter := ""
	if o.hostname != "" {
		if filter, err = config.NormalizeHost(o.hostname); err != nil {
			return fjoerr.Usage("invalid --hostname: %v", err)
		}
	}

	envHost := ""
	if f.Getenv("FJO_TOKEN") != "" {
		if envHost, err = f.ResolveHost(o.hostname); err != nil {
			envHost = ""
		}
	}

	hosts := cfg.HostIDs()
	if envHost != "" && cfg.Host(envHost) == nil {
		hosts = append(hosts, envHost)
	}
	if filter != "" {
		hosts = keep(hosts, filter)
	}
	if len(hosts) == 0 {
		if filter != "" {
			return fjoerr.New(fjoerr.KindGeneral, "not logged in to %s", filter).
				WithHint("run 'fjo auth login --hostname " + filter + "'")
		}
		return fjoerr.New(fjoerr.KindGeneral, "not logged in to any Forgejo host").
			WithHint("run 'fjo auth login --hostname <host>'")
	}

	failed := false
	w := f.IO.Out
	for i, host := range hosts {
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintln(w, host)
		h := cfg.Host(host)

		var lines []accountLine
		if host == envHost {
			lines = append(lines, accountLine{host: host, source: credentials.SourceEnv,
				token: f.Getenv("FJO_TOKEN"), active: true})
		}
		if h != nil {
			for _, u := range h.Users {
				active := u == h.ActiveUser && host != envHost
				if o.active && !active {
					continue
				}
				l := accountLine{host: host, user: u, active: active}
				if acct, err := f.StoredAccount(host, u); err != nil {
					l.err = err
				} else {
					l.token, l.source = acct.Token, acct.Source
				}
				lines = append(lines, l)
			}
		}

		for _, l := range lines {
			if l.err == nil {
				l.user, l.err = verify(ctx, f, h, l)
			}
			if l.err != nil {
				failed = true
			}
			printAccount(w, f, h, l, o.showToken)
		}
	}
	if failed {
		return fjoerr.Silent(fjoerr.KindGeneral)
	}
	return nil
}

// verify checks the token with GET /user and returns the server username.
func verify(ctx context.Context, f *app.Factory, h *config.Host, l accountLine) (string, error) {
	client, err := f.APIClient(l.host, h, l.token)
	if err != nil {
		return l.user, err
	}
	u, err := client.CurrentUser(ctx)
	if err != nil {
		return l.user, err
	}
	if l.user != "" && u.Login != l.user {
		return l.user, fmt.Errorf("token belongs to %s, not %s", u.Login, l.user)
	}
	return u.Login, nil
}

func printAccount(w io.Writer, f *app.Factory, h *config.Host, l accountLine, showToken bool) {
	user := l.user
	if user == "" {
		user = "(unknown)"
	}
	src := string(l.source)
	if src == "" {
		src = "no token"
	}
	if l.err != nil {
		fmt.Fprintf(w, "  X Failed to log in to %s account %s (%s)\n", l.host, user, src)
		fmt.Fprintf(w, "  - %s\n", f.Redactor.String(l.err.Error()))
		// Auth failures always get the re-login advice: the generic 401 hint
		// ("check your credentials with 'fjo auth status'") would point the
		// user back at the command they are running.
		if fjoerr.KindOf(l.err) == fjoerr.KindAuth {
			fmt.Fprintf(w, "  - To re-authenticate, run: fjo auth login --hostname %s\n", l.host)
		} else if hint := fjoerr.HintOf(l.err); hint != "" {
			fmt.Fprintf(w, "  - %s\n", hint)
		}
	} else {
		fmt.Fprintf(w, "  ✓ Logged in to %s account %s (%s)\n", l.host, user, src)
	}
	fmt.Fprintf(w, "  - Active account: %t\n", l.active)
	if h != nil && l.active {
		proto := h.GitProtocol
		if proto == "" {
			proto = "(default)"
		}
		fmt.Fprintf(w, "  - Git operations protocol: %s\n", proto)
		fmt.Fprintf(w, "  - API: %s\n", config.APIURL(l.host, h))
	}
	if l.token != "" {
		tok := "****************"
		if showToken {
			tok = l.token
		}
		fmt.Fprintf(w, "  - Token: %s\n", tok)
	}
}

func keep(list []string, s string) []string {
	for _, v := range list {
		if v == s {
			return []string{s}
		}
	}
	return nil
}
