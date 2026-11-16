package iostreams

import "testing"

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestColorEnabled(t *testing.T) {
	tests := []struct {
		name string
		mode ColorMode
		tty  bool
		env  map[string]string
		want bool
	}{
		{"auto tty", ColorAuto, true, nil, true},
		{"auto pipe", ColorAuto, false, nil, false},
		{"always pipe", ColorAlways, false, nil, true},
		{"never tty", ColorNever, true, nil, false},
		{"NO_COLOR", ColorAuto, true, map[string]string{"NO_COLOR": "1"}, false},
		{"FJO_NO_COLOR", ColorAuto, true, map[string]string{"FJO_NO_COLOR": "1"}, false},
		{"always beats NO_COLOR", ColorAlways, true, map[string]string{"NO_COLOR": "1"}, true},
		{"CLICOLOR_FORCE pipe", ColorAuto, false, map[string]string{"CLICOLOR_FORCE": "1"}, true},
		{"CLICOLOR_FORCE=0", ColorAuto, false, map[string]string{"CLICOLOR_FORCE": "0"}, false},
		{"NO_COLOR beats CLICOLOR_FORCE", ColorAuto, false, map[string]string{"CLICOLOR_FORCE": "1", "NO_COLOR": "1"}, false},
		{"dumb term", ColorAuto, true, map[string]string{"TERM": "dumb"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _, _, _ := Test()
			s.SetTTY(false, tt.tty, false)
			s.SetGetenv(envOf(tt.env))
			s.SetColorMode(tt.mode)
			if got := s.ColorEnabled(); got != tt.want {
				t.Errorf("ColorEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCanPrompt(t *testing.T) {
	s, _, _, _ := Test()
	if s.CanPrompt() {
		t.Error("non-tty must not prompt")
	}
	s.SetTTY(true, true, true)
	if !s.CanPrompt() {
		t.Error("tty should prompt")
	}
	s.SetGetenv(envOf(map[string]string{"FJO_PROMPT_DISABLED": "1"}))
	if s.CanPrompt() {
		t.Error("FJO_PROMPT_DISABLED must disable prompts")
	}
}

func TestTerminalWidth(t *testing.T) {
	s, _, _, _ := Test()
	if got := s.TerminalWidth(); got != 80 {
		t.Errorf("default width = %d", got)
	}
	s.SetTTY(false, true, false)
	s.SetGetenv(envOf(map[string]string{"COLUMNS": "120"}))
	if got := s.TerminalWidth(); got != 120 {
		t.Errorf("COLUMNS width = %d", got)
	}
	s.SetTerminalWidth(42)
	if got := s.TerminalWidth(); got != 42 {
		t.Errorf("override width = %d", got)
	}
}

func TestParseColorMode(t *testing.T) {
	for _, ok := range []string{"auto", "always", "never"} {
		if _, valid := ParseColorMode(ok); !valid {
			t.Errorf("%q should be valid", ok)
		}
	}
	if _, valid := ParseColorMode("yes"); valid {
		t.Error("yes should be invalid")
	}
}
