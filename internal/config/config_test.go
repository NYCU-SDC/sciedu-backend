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

func TestLoadDefaultsToDevEnvironment(t *testing.T) {
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
	require.Equal(t, "dev", config.Environment)
	require.NoError(t, config.Validate())
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

func resetFlags(t *testing.T) {
	t.Helper()
	oldCommandLine := flag.CommandLine
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	t.Cleanup(func() {
		flag.CommandLine = oldCommandLine
	})
}
