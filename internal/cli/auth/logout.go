package auth

import (
	"fmt"
	"slices"

	"github.com/spf13/cobra"

	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/cli/cmdutil"
	"github.com/maymeow/fjo/internal/config"
	"github.com/maymeow/fjo/internal/fjoerr"
)

func newLogoutCmd(f *app.Factory) *cobra.Command {
	var hostname, user string
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Log out of a Forgejo account",
		Long: `Remove a Forgejo account from fjo and delete its stored token.

The token is only removed locally; revoke it in the Forgejo web UI if it
should stop working. When the active account is removed, the next account
on that host becomes active.`,
		Example: `  fjo auth logout
  fjo auth logout --hostname git.example.com --user developer`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := f.Config()
			if err != nil {
				return err
			}
			host, err := pickHost(f, cfg, hostname, func(*config.Host) bool { return true }, "log out of")
			if err != nil {
				return err
			}
			h := cfg.Host(host)
			u, err := pickUser(f, host, user, h.Users, "log out of")
			if err != nil {
				return err
			}
			if !slices.Contains(h.Users, u) {
				return fjoerr.New(fjoerr.KindGeneral, "not logged in to %s as %s", host, u)
			}

			store, err := f.CredentialStore()
			if err != nil {
				return err
			}
			if err := store.Delete(host, u); err != nil {
				return fjoerr.Wrap(fjoerr.KindGeneral, err, "cannot remove the stored token")
			}
			wasActive := h.ActiveUser == u
			newActive, _ := cfg.RemoveUser(host, u)
			if err := f.SaveConfig(cfg); err != nil {
				return err
			}

			w := f.IO.ErrOut
			fmt.Fprintf(w, "✓ Logged out of %s account %s\n", host, u)
			if wasActive && newActive != "" {
				fmt.Fprintf(w, "✓ Switched active account for %s to %s\n", host, newActive)
			}
			if f.Getenv("FJO_TOKEN") != "" {
				fmt.Fprintln(w, "! FJO_TOKEN is still set and will be used for authentication")
			}
			return nil
		},
	}
	cmdutil.AddHostnameFlag(cmd, &hostname, true)
	cmd.Flags().StringVarP(&user, "user", "u", "", "Account to log out of")
	return cmd
}

func newSwitchCmd(f *app.Factory) *cobra.Command {
	var hostname, user string
	cmd := &cobra.Command{
		Use:   "switch",
		Short: "Switch the active account for a host",
		Long: `Switch which logged-in account fjo uses for a host.

Without --user, fjo switches to the only other account, or asks when there
are several. Without a terminal an ambiguous choice is an error.`,
		Example: `  fjo auth switch
  fjo auth switch --hostname git.example.com --user deploy-bot`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := f.Config()
			if err != nil {
				return err
			}
			multi := func(h *config.Host) bool { return h != nil && len(h.Users) > 1 }
			host, err := pickHost(f, cfg, hostname, multi, "switch accounts on")
			if err != nil {
				return err
			}
			h := cfg.Host(host)
			u, err := pickUser(f, host, user, without(h.Users, h.ActiveUser), "switch to")
			if err != nil {
				return err
			}
			if !slices.Contains(h.Users, u) {
				return fjoerr.New(fjoerr.KindGeneral, "not logged in to %s as %s", host, u).
					WithHint("run 'fjo auth login --hostname " + host + "' to add the account")
			}
			h.ActiveUser = u
			if err := f.SaveConfig(cfg); err != nil {
				return err
			}
			fmt.Fprintf(f.IO.ErrOut, "✓ Switched active account for %s to %s\n", host, u)
			return nil
		},
	}
	cmdutil.AddHostnameFlag(cmd, &hostname, true)
	cmd.Flags().StringVarP(&user, "user", "u", "", "Account to switch to")
	return cmd
}

func newTokenCmd(f *app.Factory) *cobra.Command {
	var hostname, user string
	cmd := &cobra.Command{
		Use:   "token",
		Short: "Print the authentication token fjo uses for a host",
		Long: `Print the token fjo uses for a host and account.

This prints a secret to standard output. It exists for scripting
compatibility (e.g. 'git -c http.extraHeader=...'); never paste its output
into logs or issue reports. When FJO_TOKEN is set, that value is printed.`,
		Example: `  fjo auth token
  fjo auth token --hostname git.example.com --user deploy-bot`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			host, err := f.ResolveHost(hostname)
			if err != nil {
				return err
			}
			var acct *app.Account
			if user != "" {
				acct, err = f.StoredAccount(host, user)
			} else {
				acct, err = f.AccountFor(host)
			}
			if err != nil {
				return err
			}
			fmt.Fprintln(f.IO.Out, acct.Token)
			return nil
		},
	}
	cmdutil.AddHostnameFlag(cmd, &hostname, true)
	cmd.Flags().StringVarP(&user, "user", "u", "", "Account whose token to print")
	return cmd
}
