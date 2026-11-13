package config

// Source names where a resolved setting came from (used by diagnostics and
// a future `fjo config list --show-source`).
type Source string

// Setting sources, highest priority first (IMPLEMENTATION §6.1).
const (
	SourceFlag     Source = "flag"
	SourceEnv      Source = "env"
	SourceRepo     Source = "repo" // repository-local settings (Phase 2)
	SourceUser     Source = "config"
	SourceDefault  Source = "default"
	SourceNotFound Source = ""
)

// Layer is one level of the precedence chain. An empty value means "not set".
type Layer struct {
	Source Source
	Value  string
}

// Value is a resolved setting.
type Value struct {
	Value  string
	Source Source
}

// Resolve returns the first non-empty layer in order. Callers pass layers from
// highest to lowest priority: flag, env, repo, user config, built-in default.
func Resolve(layers ...Layer) Value {
	for _, l := range layers {
		if l.Value != "" {
			return Value{Value: l.Value, Source: l.Source}
		}
	}
	return Value{Source: SourceNotFound}
}

// Settings are the resolved, nonsecret settings available to every command.
type Settings struct {
	Host   Value
	Color  Value
	Pager  Value
	Editor Value
}

// Overrides carries values supplied on the command line.
type Overrides struct {
	Host  string
	Color string
}

// ResolveSettings applies the precedence chain to the global settings.
// Repository-local settings are not implemented yet (Phase 2).
func ResolveSettings(cfg *Config, flags Overrides, getenv func(string) string) Settings {
	if cfg == nil {
		cfg = New()
	}
	return Settings{
		Host: Resolve(
			Layer{SourceFlag, flags.Host},
			Layer{SourceEnv, getenv("FJO_HOST")},
			Layer{SourceUser, cfg.Defaults.Host},
		),
		Color: Resolve(
			Layer{SourceFlag, flags.Color},
			Layer{SourceUser, cfg.Output.Color},
			Layer{SourceDefault, "auto"},
		),
		Pager: Resolve(
			Layer{SourceEnv, getenv("FJO_PAGER")},
			Layer{SourceUser, cfg.Defaults.Pager},
			Layer{SourceEnv, getenv("PAGER")},
		),
		Editor: Resolve(
			Layer{SourceEnv, getenv("FJO_EDITOR")},
			Layer{SourceUser, cfg.Defaults.Editor},
			Layer{SourceEnv, getenv("VISUAL")},
			Layer{SourceEnv, getenv("EDITOR")},
		),
	}
}
