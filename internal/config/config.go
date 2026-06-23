package config

import (
	"errors"
	"flag"
	"os"
	"strings"

	configutil "github.com/NYCU-SDC/summer/pkg/config"
	"github.com/joho/godotenv"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

const DefaultSecret = "default-secret"

var ErrInsecureProductionSecret = errors.New("production secret must be configured")

type Config struct {
	Debug                      bool   `yaml:"debug"              envconfig:"DEBUG"`
	Host                       string `yaml:"host"               envconfig:"HOST"`
	Port                       string `yaml:"port"               envconfig:"PORT"`
	Secret                     string `yaml:"secret"             envconfig:"SECRET"`
	Environment                string `yaml:"environment"        envconfig:"ENVIRONMENT"`
	DatabaseURL                string `yaml:"database_url"       envconfig:"DATABASE_URL"`
	MigrationSource            string `yaml:"migration_source"   envconfig:"MIGRATION_SOURCE"`
	LLMURL                     string `yaml:"llm_url"            envconfig:"LLM_URL"`
	AllowOrigins               string `yaml:"allow_origins"      envconfig:"ALLOW_ORIGINS"`
	GoogleOAuthClientID        string `yaml:"google_oauth_client_id"     envconfig:"GOOGLE_OAUTH_CLIENT_ID"`
	GoogleOAuthClientSecret    string `yaml:"google_oauth_client_secret" envconfig:"GOOGLE_OAUTH_CLIENT_SECRET"`
	GoogleOAuthRedirectURL     string `yaml:"google_oauth_redirect_url"  envconfig:"GOOGLE_OAUTH_REDIRECT_URL"`
	GoogleOAuthCredentialsFile string `yaml:"google_oauth_credentials_file" envconfig:"GOOGLE_OAUTH_CREDENTIALS_FILE"`
	AuthRedirectAllowlist      string `yaml:"auth_redirect_allowlist"    envconfig:"AUTH_REDIRECT_ALLOWLIST"`
}

type LogBuffer struct {
	buffer []logEntry
}

type logEntry struct {
	msg  string
	err  error
	meta map[string]string
}

func NewConfigLogger() *LogBuffer {
	return &LogBuffer{}
}

func (cl *LogBuffer) Warn(msg string, err error, meta map[string]string) {
	cl.buffer = append(cl.buffer, logEntry{msg: msg, err: err, meta: meta})
}

func (cl *LogBuffer) FlushToZap(logger *zap.Logger) {
	for _, e := range cl.buffer {
		var fields []zap.Field
		if e.err != nil {
			fields = append(fields, zap.Error(e.err))
		}
		for k, v := range e.meta {
			fields = append(fields, zap.String(k, v))
		}
		logger.Warn(e.msg, fields...)
	}
	cl.buffer = nil
}

func Load() (Config, *LogBuffer) {
	logger := NewConfigLogger()

	config := &Config{
		Debug:                      false,
		Host:                       "localhost",
		Port:                       "8080",
		Secret:                     DefaultSecret,
		Environment:                "dev",
		DatabaseURL:                "",
		MigrationSource:            "file://internal/database/migrations",
		LLMURL:                     "https://llm.dev.sciedu.sdc.nycu.club",
		AllowOrigins:               "http://localhost:5173",
		GoogleOAuthClientID:        "",
		GoogleOAuthClientSecret:    "",
		GoogleOAuthRedirectURL:     "",
		GoogleOAuthCredentialsFile: "",
		AuthRedirectAllowlist:      "",
	}

	var err error
	config, err = FromFile("config.yaml", config, logger)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Warn("Failed to load config from file", err, map[string]string{"path": "config.yaml"})
		}
	}

	config, err = FromEnv(config, logger)
	if err != nil {
		logger.Warn("Failed to load config from env", err, map[string]string{"path": ".env"})
	}

	config, err = FromFlags(config)
	if err != nil {
		logger.Warn("Failed to load config from flags", err, map[string]string{"path": "flags"})
	}

	config.Environment = normalizeEnvironment(config.Environment)

	return *config, logger
}

func FromFile(filePath string, config *Config, logger *LogBuffer) (*Config, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return config, err
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			logger.Warn("Failed to close config file", err, map[string]string{"path": filePath})
		}
	}(file)

	fileConfig := Config{}
	if err := yaml.NewDecoder(file).Decode(&fileConfig); err != nil {
		return config, err
	}

	return configutil.Merge[Config](config, &fileConfig)
}

func FromEnv(config *Config, logger *LogBuffer) (*Config, error) {
	if err := godotenv.Overload(); err != nil {
		if os.IsNotExist(err) {
			logger.Warn("No .env file found", err, map[string]string{"path": ".env"})
		} else {
			return nil, err
		}
	}

	envConfig := &Config{
		Debug:                      os.Getenv("DEBUG") == "true",
		Host:                       os.Getenv("HOST"),
		Port:                       os.Getenv("PORT"),
		Secret:                     os.Getenv("SECRET"),
		Environment:                firstNonEmpty(os.Getenv("ENVIRONMENT"), os.Getenv("ENV")),
		DatabaseURL:                os.Getenv("DATABASE_URL"),
		MigrationSource:            os.Getenv("MIGRATION_SOURCE"),
		LLMURL:                     os.Getenv("LLM_URL"),
		AllowOrigins:               os.Getenv("ALLOW_ORIGINS"),
		GoogleOAuthClientID:        os.Getenv("GOOGLE_OAUTH_CLIENT_ID"),
		GoogleOAuthClientSecret:    os.Getenv("GOOGLE_OAUTH_CLIENT_SECRET"),
		GoogleOAuthRedirectURL:     os.Getenv("GOOGLE_OAUTH_REDIRECT_URL"),
		GoogleOAuthCredentialsFile: os.Getenv("GOOGLE_OAUTH_CREDENTIALS_FILE"),
		AuthRedirectAllowlist:      os.Getenv("AUTH_REDIRECT_ALLOWLIST"),
	}

	merged, err := configutil.Merge[Config](config, envConfig)
	if err != nil {
		return nil, err
	}

	merged.Environment = normalizeEnvironment(merged.Environment)
	return merged, nil
}

func FromFlags(config *Config) (*Config, error) {
	flagConfig := &Config{}

	flag.BoolVar(&flagConfig.Debug, "debug", false, "debug mode")
	flag.StringVar(&flagConfig.Host, "host", "", "host")
	flag.StringVar(&flagConfig.Port, "port", "", "port")
	flag.StringVar(&flagConfig.Secret, "secret", "", "secret")
	flag.StringVar(&flagConfig.Environment, "environment", "", "environment")
	flag.StringVar(&flagConfig.DatabaseURL, "database_url", "", "database url")
	flag.StringVar(&flagConfig.MigrationSource, "migration_source", "", "migration source")
	flag.StringVar(&flagConfig.LLMURL, "llm_url", "", "LLM url")
	flag.StringVar(&flagConfig.AllowOrigins, "allow_origins", "", "allowed CORS origins (comma-separated)")
	flag.StringVar(&flagConfig.GoogleOAuthClientID, "google_oauth_client_id", "", "Google OAuth client ID")
	flag.StringVar(&flagConfig.GoogleOAuthClientSecret, "google_oauth_client_secret", "", "Google OAuth client secret")
	flag.StringVar(&flagConfig.GoogleOAuthRedirectURL, "google_oauth_redirect_url", "", "Google OAuth redirect URL")
	flag.StringVar(&flagConfig.GoogleOAuthCredentialsFile, "google_oauth_credentials_file", "", "Google OAuth client secret JSON path")
	flag.StringVar(&flagConfig.AuthRedirectAllowlist, "auth_redirect_allowlist", "", "allowed post-login redirect URL prefixes (comma-separated)")

	flag.Parse()

	return configutil.Merge[Config](config, flagConfig)
}

func (c Config) Validate() error {
	if normalizeEnvironment(c.Environment) != "dev" && strings.TrimSpace(c.Secret) == DefaultSecret {
		return ErrInsecureProductionSecret
	}
	return nil
}

func normalizeEnvironment(environment string) string {
	environment = strings.ToLower(strings.TrimSpace(environment))
	if environment == "local" {
		return "dev"
	}
	return environment
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
