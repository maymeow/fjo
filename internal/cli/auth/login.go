package auth

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/browser"
	"github.com/maymeow/fjo/internal/cli/cmdutil"
	"github.com/maymeow/fjo/internal/config"
	"github.com/maymeow/fjo/internal/credentials"
	"github.com/maymeow/fjo/internal/fjoerr"
)

type loginOptions struct {
	hostname        string
	withToken       bool
	web             bool
	gitProtocol     string
	insecureStorage bool
	insecureHTTP    bool
}

func newLoginCmd(f *app.Factory) *cobra.Command {
	var o loginOptions
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in to a Forgejo host",
		Long: `Log in to a Forgejo host with a personal access token.

Create a token in the Forgejo web UI under Settings > Applications. The
token is verified against the server (GET /user) and stored in the OS
credential store under the host ID and your Forgejo username. Logging in
again to the same host with another account adds that account and makes
it active.

The token is never accepted as a command-line argument. Pass it on
standard input with --with-token, or paste it at the prompt.

Use --insecure-storage only where no OS credential store exists (headless
Linux, containers): the token is then written in plain text to
credentials.yaml in the fjo config directory.`,
		Example: `  # interactive
  fjo auth login --hostname git.example.com

  # from a file or a secret manager
  fjo auth login --hostname codeberg.org --with-token < token.txt

  # instance under a subpath, plain HTTP dev server
  fjo auth login --hostname https://example.com/forgejo
  fjo auth login --hostname http://localhost:3000 --insecure-http`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLogin(cmd, f, o)
		},
	}
	cmdutil.AddHostnameFlag(cmd, &o.hostname, true)
	fl := cmd.Flags()
	fl.BoolVar(&o.withToken, "with-token", false, "Read the token from standard input")
	fl.BoolVarP(&o.web, "web", "w", false, "Open the token settings page in the browser")
	fl.StringVarP(&o.gitProtocol, "git-protocol", "p", "", `Protocol for git operations on this host: "ssh" or "https"`)
	fl.BoolVar(&o.insecureStorage, "insecure-storage", false, "Store the token in a plain-text file instead of the OS credential store")
	fl.BoolVar(&o.insecureHTTP, "insecure-http", false, "Allow plain HTTP for this host (trusted development hosts only)")
	return cmd
}

func runLogin(cmd *cobra.Command, f *app.Factory, o loginOptions) error {
	if f.Getenv("FJO_TOKEN") != "" {
		return fjoerr.New(fjoerr.KindGeneral, "the FJO_TOKEN environment variable is being used for authentication").
			WithHint("to store credentials with fjo instead, first unset FJO_TOKEN")
	}
	if o.gitProtocol != "" && o.gitProtocol != "ssh" && o.gitProtocol != "https" {
		return fjoerr.Usage("invalid --git-protocol %q: use ssh or https", o.gitProtocol)
	}
	if o.withToken && o.web {
		return fjoerr.Usage("--with-token and --web cannot be used together")
	}

	cfg, err := f.Config()
	if err != nil {
		return err
	}

	raw := o.hostname
	if raw == "" {
		raw = f.Getenv("FJO_HOST")
	}
	if raw == "" {
		if !f.IO.CanPrompt() {
			return fjoerr.Usage("--hostname is required when not running interactively")
		}
		if raw, err = f.Prompter.Input("Forgejo host (e.g. codeberg.org):"); err != nil {
			return err
		}
	}
	host, err := config.NormalizeHost(raw)
	if err != nil {
		return fjoerr.Usage("invalid host: %v", err)
	}

	// Work on a copy so a failed login leaves the config untouched.
	h := &config.Host{}
	if existing := cfg.Host(host); existing != nil {
		*h = *existing
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(raw)), "http://") && !o.insecureHTTP && !h.InsecureHTTP {
		return fjoerr.Usage("refusing to send a token over plain HTTP to %s", host).
			WithHint("use https, or pass --insecure-http for a trusted development host")
	}
	if o.insecureHTTP {
		h.InsecureHTTP = true
	}

	tokenPage := strings.TrimSuffix(strings.TrimSuffix(config.APIURL(host, h), "/"), "/api/v1") + "/user/settings/applications"
	token, err := readToken(f, o, tokenPage)
	if err != nil {
		return err
	}
	f.Redactor.Add(token)

	client, err := f.APIClient(host, h, token)
	if err != nil {
		return err
	}
	ctx := cmd.Context()
	user, err := client.CurrentUser(ctx)
	if err != nil {
		if fjoerr.KindOf(err) == fjoerr.KindAuth {
			return fjoerr.Wrap(fjoerr.KindAuth, err, "the token was rejected by %s", host).
				WithHint("check that the token is valid and has at least the read:user scope")
		}
		return fjoerr.Wrap(fjoerr.KindOf(err), err, "cannot verify the token with %s", host)
	}
	if user.Login == "" {
		return fjoerr.New(fjoerr.KindGeneral, "%s returned no username for the token", host)
	}
	version, verr := client.ServerVersion(ctx)
	if verr != nil {
		f.Logger.Debug("server version unavailable", "err", verr)
	}

	store, err := f.CredentialStore()
	if err != nil {
		return err
	}
	src, err := store.Set(host, user.Login, token, o.insecureStorage)
	if err != nil {
		return fjoerr.Wrap(fjoerr.KindGeneral, err, "cannot store the token").
			WithHint("without an OS credential store use --insecure-storage, or set FJO_TOKEN instead of logging in")
	}

	entry := cfg.AddUser(host, user.Login)
	entry.InsecureHTTP = h.InsecureHTTP
	if o.gitProtocol != "" {
		entry.GitProtocol = o.gitProtocol
	}
	if err := f.SaveConfig(cfg); err != nil {
		return err
	}

	w := f.IO.ErrOut
	if version != "" {
		fmt.Fprintf(w, "✓ Logged in to %s account %s (Forgejo %s)\n", host, user.Login, version)
	} else {
		fmt.Fprintf(w, "✓ Logged in to %s account %s\n", host, user.Login)
	}
	if src == credentials.SourceFile {
		fmt.Fprintf(w, "! Token stored in plain text: %s\n", store.FilePath)
	}
	return nil
}

func readToken(f *app.Factory, o loginOptions, tokenPage string) (string, error) {
	var token string
	switch {
	case o.withToken:
		b, err := io.ReadAll(io.LimitReader(f.IO.In, 64<<10))
		if err != nil {
			return "", fjoerr.Wrap(fjoerr.KindGeneral, err, "cannot read token from standard input")
		}
		token = string(b)
	case !f.IO.CanPrompt():
		return "", fjoerr.Usage("--with-token is required when not running interactively")
	default:
		w := f.IO.ErrOut
		fmt.Fprintf(w, "Create a personal access token at:\n  %s\n", tokenPage)
		fmt.Fprintln(w, "Scopes: read:user to log in; add write:repository, write:issue, ... for the commands you use.")
		if o.web {
			if err := f.Browser(tokenPage); err != nil {
				if !errors.Is(err, browser.ErrNoBrowser) {
					f.Logger.Debug("cannot open browser", "err", err)
				}
				fmt.Fprintln(w, "! Could not open a browser; open the URL above manually.")
			}
		}
		var err error
		if token, err = f.Prompter.Password("Paste your token:"); err != nil {
			return "", err
		}
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return "", fjoerr.Usage("no token provided")
	}
	if strings.ContainsAny(token, " \t\r\n") {
		return "", fjoerr.Usage("the token must be a single line without spaces")
	}
	return token, nil
}
