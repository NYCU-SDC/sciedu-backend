package config

import (
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
		{name: "local maps to dev", in: "local", want: EnvironmentDev},
		{name: "local with spaces maps to dev", in: "  local  ", want: EnvironmentDev},
		{name: "prod stays prod", in: EnvironmentProd, want: EnvironmentProd},
		{name: "already dev stays dev", in: EnvironmentDev, want: EnvironmentDev},
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

	config, err := FromEnv(&Config{Environment: EnvironmentProd}, NewConfigLogger())
	require.NoError(t, err)
	require.Equal(t, EnvironmentDev, config.Environment)
}

func TestLoadNormalizesLocalEnvironmentFromEnvFileFallback(t *testing.T) {
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
	require.Equal(t, EnvironmentDev, config.Environment)
}
