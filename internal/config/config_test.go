package config

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeEnvironment(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "local maps to dev", in: "local", want: "dev"},
		{name: "local with spaces maps to dev", in: "  local  ", want: "dev"},
		{name: "prod stays prod", in: "prod", want: "prod"},
		{name: "already dev stays dev", in: "dev", want: "dev"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, normalizeEnvironment(tt.in))
		})
	}
}

func TestFromEnvUsesENVAndNormalizesLocal(t *testing.T) {
	workdir := t.TempDir()
	oldwd, err := os.Getwd()
	require.NoError(t, err)
	defer func() {
		require.NoError(t, os.Chdir(oldwd))
	}()
	require.NoError(t, os.Chdir(workdir))

	t.Setenv("ENVIRONMENT", "")
	t.Setenv("ENV", "local")

	config, err := FromEnv(&Config{Environment: "prod"}, NewConfigLogger())
	require.NoError(t, err)
	require.Equal(t, "dev", config.Environment)
}

func TestLoadNormalizesLocalEnvironmentFromEnvFileFallback(t *testing.T) {
	resetFlags(t)
	workdir := t.TempDir()
	oldwd, err := os.Getwd()
	require.NoError(t, err)
	defer func() {
		require.NoError(t, os.Chdir(oldwd))
	}()
	require.NoError(t, os.Chdir(workdir))

	configPath := filepath.Join(workdir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("environment: local\n"), 0o600))

	config, _ := Load()
	require.Equal(t, "dev", config.Environment)
}

func TestLoadDefaultsToProdEnvironment(t *testing.T) {
	resetFlags(t)
	workdir := t.TempDir()
	oldwd, err := os.Getwd()
	require.NoError(t, err)
	defer func() {
		require.NoError(t, os.Chdir(oldwd))
	}()
	require.NoError(t, os.Chdir(workdir))

	t.Setenv("ENVIRONMENT", "")
	t.Setenv("ENV", "")

	config, _ := Load()
	require.Equal(t, "prod", config.Environment)
	require.Equal(t, "*.sciedu.sdc.nycu.club", config.AllowOrigins)
	require.ErrorIs(t, config.Validate(), ErrInsecureProductionSecret)
}

func TestValidateRejectsDefaultProductionSecret(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name:    "prod default secret rejected",
			config:  Config{Environment: "prod", Secret: DefaultSecret},
			wantErr: true,
		},
		{
			name:   "dev default secret allowed",
			config: Config{Environment: "dev", Secret: DefaultSecret},
		},
		{
			name:   "prod custom secret allowed",
			config: Config{Environment: "prod", Secret: "strong-random-secret"},
		},
		{
			name:   "local default secret allowed",
			config: Config{Environment: "local", Secret: DefaultSecret},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.ErrorIs(t, err, ErrInsecureProductionSecret)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestValidateLocalhostRequiresDev(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "dev allows localhost cors and redirect",
			config: Config{
				Environment:           "dev",
				Secret:                DefaultSecret,
				AllowOrigins:          "http://localhost:5173,http://localhost:3000",
				AuthRedirectAllowlist: "http://localhost:5173",
			},
		},
		{
			name: "prod rejects localhost cors",
			config: Config{
				Environment:  "prod",
				Secret:       "strong-random-secret",
				AllowOrigins: "https://sciedu.sdc.nycu.club,http://localhost:5173",
			},
			wantErr: true,
		},
		{
			name: "stage rejects localhost redirect",
			config: Config{
				Environment:           "stage",
				Secret:                "strong-random-secret",
				AllowOrigins:          "*.sciedu.sdc.nycu.club",
				AuthRedirectAllowlist: "https://stage.sciedu.sdc.nycu.club,http://127.0.0.1:3000",
			},
			wantErr: true,
		},
		{
			name: "prod allows sciedu origins",
			config: Config{
				Environment:           "prod",
				Secret:                "strong-random-secret",
				AllowOrigins:          "*.sciedu.sdc.nycu.club",
				AuthRedirectAllowlist: "https://sciedu.sdc.nycu.club",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.ErrorIs(t, err, ErrLocalhostRequiresDev)
				return
			}
			require.NoError(t, err)
		})
	}
}

func resetFlags(t *testing.T) {
	t.Helper()
	oldCommandLine := flag.CommandLine
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	t.Cleanup(func() {
		flag.CommandLine = oldCommandLine
	})
}
