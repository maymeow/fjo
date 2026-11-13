// Package auth implements `fjo auth` (IMPLEMENTATION §5.2, §7).
package auth

import (
	"fmt"
	"slices"

	"github.com/spf13/cobra"

	"github.com/maymeow/fjo/internal/app"
	"github.com/maymeow/fjo/internal/config"
	"github.com/maymeow/fjo/internal/fjoerr"
)

// NewCmd returns the `auth` command group.
func NewCmd(f *app.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth <command>",
		Short: "Authenticate fjo with Forgejo hosts",
		Long: "Manage logins to Forgejo hosts. Tokens are stored in the OS credential store\n" +
			"(Windows Credential Manager, macOS Keychain, Linux Secret Service), never in\n" +
			"the config file. FJO_TOKEN overrides stored tokens for the targeted host.",
	}
	cmd.AddCommand(
		newLoginCmd(f),
		newLogoutCmd(f),
		newStatusCmd(f),
		newSwitchCmd(f),
		newTokenCmd(f),
	)
	return cmd
}

// pickHost resolves the host for logout/switch: the flag, or the only host
// matching pred, or an interactive choice.
func pickHost(f *app.Factory, cfg *config.Config, flag string, pred func(*config.Host) bool, what string) (string, error) {
	if flag != "" {
		id, err := config.NormalizeHost(flag)
		if err != nil {
			return "", fjoerr.Usage("invalid --hostname: %v", err)
		}
		if cfg.Host(id) == nil {
			return "", fjoerr.New(fjoerr.KindGeneral, "not logged in to %s", id)
		}
		return id, nil
	}
	var ids []string
	for _, id := range cfg.HostIDs() {
		if pred(cfg.Hosts[id]) {
			ids = append(ids, id)
		}
	}
	switch {
	case len(ids) == 0:
		return "", fjoerr.New(fjoerr.KindGeneral, "no hosts to %s", what).
			WithHint("run 'fjo auth status' to see your logins")
	case len(ids) == 1:
		return ids[0], nil
	case !f.IO.CanPrompt():
		return "", fjoerr.Usage("several hosts are configured; specify --hostname")
	}
	i, err := f.Prompter.Select(fmt.Sprintf("Which host do you want to %s?", what), ids)
	if err != nil {
		return "", err
	}
	return ids[i], nil
}

// pickUser resolves the user on a host: the flag, the only candidate, or an
// interactive choice among candidates.
func pickUser(f *app.Factory, host, flag string, candidates []string, what string) (string, error) {
	if flag != "" {
		return flag, nil
	}
	switch {
	case len(candidates) == 0:
		return "", fjoerr.New(fjoerr.KindGeneral, "no accounts to %s on %s", what, host)
	case len(candidates) == 1:
		return candidates[0], nil
	case !f.IO.CanPrompt():
		return "", fjoerr.Usage("several accounts on %s; specify --user", host)
	}
	i, err := f.Prompter.Select(fmt.Sprintf("Which account on %s do you want to %s?", host, what), candidates)
	if err != nil {
		return "", err
	}
	return candidates[i], nil
}

func without(list []string, s string) []string {
	return slices.DeleteFunc(slices.Clone(list), func(v string) bool { return v == s })
}
