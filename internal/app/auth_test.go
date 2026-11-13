package app

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maymeow/fjo/internal/config"
	"github.com/maymeow/fjo/internal/credentials"
	"github.com/maymeow/fjo/internal/fjoerr"
)

// factoryWithConfig writes cfg to a temporary config directory and returns a
// factory wired to it and to the given environment.
func factoryWithConfig(t *testing.T, cfg *config.Config, env map[string]string) *Factory {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, config.FileName)
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	if env == nil {
		env = map[string]string{}
	}
	env["FJO_CONFIG_DIR"] = dir
	return newFactory(env)
}

func memoryCredentials(f *Factory) (*credentials.Memory, *credentials.Memory) {
	keyring, file := credentials.NewMemory(), credentials.NewMemory()
	f.Credentials = &credentials.Manager{Keyring: keyring, File: file}
	return keyring, file
}

func TestSaveConfigPersistsAndUpdatesCache(t *testing.T) {
	f := factoryWithConfig(t, config.New(), nil)
	cfg, err := f.Config()
	if err != nil {
		t.Fatal(err)
	}
	cfg.AddUser("git.example.com", "alice")
	if err := f.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	path, err := f.ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if h := loaded.Host("git.example.com"); h == nil || h.ActiveUser != "alice" {
		t.Errorf("saved host = %+v", h)
	}
	if got, err := f.Config(); err != nil || got != cfg {
		t.Errorf("cached config = %p, %v; want %p", got, err, cfg)
	}
}

func TestSaveConfigRejectsInvalidConfig(t *testing.T) {
	f := factoryWithConfig(t, config.New(), nil)
	cfg := &config.Config{Version: 99}
	err := f.SaveConfig(cfg)
	if fjoerr.KindOf(err) != fjoerr.KindGeneral {
		t.Fatalf("kind = %q, err = %v", fjoerr.KindOf(err), err)
	}
}

func TestCredentialStoreUsesConfigDirectory(t *testing.T) {
	f := factoryWithConfig(t, config.New(), nil)
	store, err := f.CredentialStore()
	if err != nil {
		t.Fatalf("CredentialStore: %v", err)
	}
	path, err := f.ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if store.FilePath != filepath.Join(filepath.Dir(path), credentials.FileName) {
		t.Errorf("FilePath = %q", store.FilePath)
	}
	if again, _ := f.CredentialStore(); again != store {
		t.Error("CredentialStore should be cached")
	}
}

func TestResolveHostPrecedence(t *testing.T) {
	cfg := config.New()
	cfg.AddUser("default.example.com", "alice")
	cfg.AddUser("other.example.com", "bob")
	cfg.Defaults.Host = "default.example.com"

	tests := []struct {
		name string
		flag string
		env  string
		want string
	}{
		{"flag wins", "https://Flag.Example.com/", "env.example.com", "flag.example.com"},
		{"environment", "", "env.example.com", "env.example.com"},
		{"default", "", "", "default.example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := map[string]string{}
			if tt.env != "" {
				env["FJO_HOST"] = tt.env
			}
			f := factoryWithConfig(t, cfg, env)
			got, err := f.ResolveHost(tt.flag)
			if err != nil {
				t.Fatalf("ResolveHost: %v", err)
			}
			if got != tt.want {
				t.Errorf("host = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveHostOnlyConfiguredHost(t *testing.T) {
	cfg := config.New()
	cfg.AddUser("only.example.com", "alice")
	cfg.Defaults.Host = ""
	f := factoryWithConfig(t, cfg, nil)

	got, err := f.ResolveHost("")
	if err != nil || got != "only.example.com" {
		t.Fatalf("ResolveHost = %q, %v", got, err)
	}
}

func TestResolveHostErrors(t *testing.T) {
	t.Run("no host", func(t *testing.T) {
		f := factoryWithConfig(t, config.New(), nil)
		_, err := f.ResolveHost("")
		if fjoerr.KindOf(err) != fjoerr.KindUsage {
			t.Errorf("kind = %q, err = %v", fjoerr.KindOf(err), err)
		}
	})
	t.Run("invalid flag", func(t *testing.T) {
		f := factoryWithConfig(t, config.New(), nil)
		_, err := f.ResolveHost("https://")
		if fjoerr.KindOf(err) != fjoerr.KindUsage {
			t.Errorf("kind = %q, err = %v", fjoerr.KindOf(err), err)
		}
	})
	t.Run("invalid environment", func(t *testing.T) {
		f := factoryWithConfig(t, config.New(), map[string]string{"FJO_HOST": "https://"})
		_, err := f.ResolveHost("")
		if fjoerr.KindOf(err) != fjoerr.KindUsage || !strings.Contains(err.Error(), "env") {
			t.Errorf("err = %v", err)
		}
	})
}

func TestAccountForEnvironment(t *testing.T) {
	f := factoryWithConfig(t, config.New(), map[string]string{"FJO_TOKEN": "env-token"})
	acct, err := f.AccountFor("git.example.com")
	if err != nil {
		t.Fatalf("AccountFor: %v", err)
	}
	if acct.Token != "env-token" || acct.Source != credentials.SourceEnv || acct.User != "" {
		t.Errorf("account = %+v", acct)
	}
	if strings.Contains(f.Redactor.String("x env-token y"), "env-token") {
		t.Error("token must be registered with the redactor")
	}
}

func TestAccountForStoredToken(t *testing.T) {
	cfg := config.New()
	cfg.AddUser("git.example.com", "alice")
	f := factoryWithConfig(t, cfg, nil)
	keyring, _ := memoryCredentials(f)
	if err := keyring.Set("git.example.com", "alice", "stored-token"); err != nil {
		t.Fatal(err)
	}

	acct, err := f.AccountFor("git.example.com")
	if err != nil {
		t.Fatalf("AccountFor: %v", err)
	}
	if acct.User != "alice" || acct.Token != "stored-token" || acct.Source != credentials.SourceKeyring {
		t.Errorf("account = %+v", acct)
	}
}

func TestAccountForFileToken(t *testing.T) {
	cfg := config.New()
	cfg.AddUser("git.example.com", "alice")
	f := factoryWithConfig(t, cfg, nil)
	_, file := memoryCredentials(f)
	if err := file.Set("git.example.com", "alice", "file-token"); err != nil {
		t.Fatal(err)
	}

	acct, err := f.AccountFor("git.example.com")
	if err != nil || acct.Source != credentials.SourceFile || acct.Token != "file-token" {
		t.Fatalf("AccountFor = %+v, %v", acct, err)
	}
}

func TestAccountForNotLoggedIn(t *testing.T) {
	f := factoryWithConfig(t, config.New(), nil)
	_, err := f.AccountFor("git.example.com")
	if fjoerr.KindOf(err) != fjoerr.KindAuth {
		t.Fatalf("kind = %q, err = %v", fjoerr.KindOf(err), err)
	}
	if !strings.Contains(fjoerr.HintOf(err), "auth login") {
		t.Errorf("hint = %q", fjoerr.HintOf(err))
	}
}

func TestStoredAccountMissingToken(t *testing.T) {
	f := factoryWithConfig(t, config.New(), nil)
	memoryCredentials(f)
	_, err := f.StoredAccount("git.example.com", "alice")
	if fjoerr.KindOf(err) != fjoerr.KindAuth {
		t.Fatalf("kind = %q, err = %v", fjoerr.KindOf(err), err)
	}
}

func TestStoredAccountStoreFailure(t *testing.T) {
	f := factoryWithConfig(t, config.New(), nil)
	keyring, _ := memoryCredentials(f)
	keyring.Fail = errors.New("secret service unavailable")
	_, err := f.StoredAccount("git.example.com", "alice")
	if fjoerr.KindOf(err) != fjoerr.KindAuth {
		t.Fatalf("kind = %q, err = %v", fjoerr.KindOf(err), err)
	}
	if !strings.Contains(fjoerr.HintOf(err), "insecure-storage") {
		t.Errorf("hint = %q", fjoerr.HintOf(err))
	}
}

func TestClientForSendsStoredToken(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "token stored-token" {
			t.Errorf("Authorization = %q", got)
		}
		_, _ = io.WriteString(w, `{"login":"alice"}`)
	}))
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "https://")
	cfg := config.New()
	cfg.AddUser(host, "alice")
	f := factoryWithConfig(t, cfg, nil)
	f.HTTPTransport = srv.Client().Transport
	keyring, _ := memoryCredentials(f)
	if err := keyring.Set(host, "alice", "stored-token"); err != nil {
		t.Fatal(err)
	}

	client, acct, err := f.ClientFor(host)
	if err != nil {
		t.Fatalf("ClientFor: %v", err)
	}
	if acct.User != "alice" {
		t.Errorf("account = %+v", acct)
	}
	user, err := client.CurrentUser(t.Context())
	if err != nil || user.Login != "alice" {
		t.Fatalf("CurrentUser = %+v, %v", user, err)
	}
}

func TestAPIClientRejectsMissingCAFile(t *testing.T) {
	f := factoryWithConfig(t, config.New(), nil)
	_, err := f.APIClient("git.example.com", &config.Host{CAFile: filepath.Join(t.TempDir(), "missing.pem")}, "tok")
	if fjoerr.KindOf(err) != fjoerr.KindConfig {
		t.Fatalf("kind = %q, err = %v", fjoerr.KindOf(err), err)
	}
}
