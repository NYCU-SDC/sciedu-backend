package main

import (
	"context"
	logutil "github.com/NYCU-SDC/summer/pkg/log"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sciedu-backend/databaseutil"
	"sciedu-backend/internal"
	"sciedu-backend/internal/config"
	"sciedu-backend/internal/questions"
	"time"

	// databaseutil "github.com/NYCU-SDC/summer/pkg/databaseutil"
	problemutil "github.com/NYCU-SDC/summer/pkg/problem"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"go.uber.org/zap"
)

var AppName = "no-app-name"

var Version = "no-version"

var BuildTime = "no-build-time"

var CommitHash = "no-commit-hash"

var Environment = "no-env"

func main() {
	AppName = os.Getenv("APP_NAME")
	if AppName == "" {
		AppName = "sciedu-backend"
	}

	if BuildTime == "no-build-time" {
		now := time.Now()
		BuildTime = "not provided (now: " + now.Format(time.RFC3339) + ")"
	}

	Environment = os.Getenv("ENV")
	if Environment == "" {
		Environment = "no-env"
	}

	appMetadata := []zap.Field{
		zap.String("app_name", AppName),
		zap.String("version", Version),
		zap.String("build_time", BuildTime),
		zap.String("commit_hash", CommitHash),
		zap.String("environment", Environment),
	}

	cfg, cfgLog := config.Load()

	logger, err := initLogger(&cfg, appMetadata)
	if err != nil {
		log.Fatalf("Failed to initalize logger: %v, exiting...", err)
	}

	cfgLog.FlushToZap(logger)

	logger.Info("Hello, World!")

	if os.Getenv("ENV") != "snapshot" && os.Getenv("ENV") != "stage" {
		// get absolute path to local .env file
		_, filename, _, ok := runtime.Caller(0)
		if !ok {
			log.Fatal("failed to get current file path", zap.Error(err))
		}
		envPath := filepath.Join(filepath.Dir(filename), "../../.env")
		envPath, err = filepath.Abs(envPath)
		if err != nil {
			log.Fatal("failed to get absolute path", zap.Error(err))
		}

		err = godotenv.Load(envPath)
		if err != nil {
			logger.Fatal("failed to load .env file", zap.Error(err))
		}
	}

	databaseUrl := os.Getenv("DATABASE_URL")
	migrationSource := os.Getenv("MIGRATION_SOURCE")

	err = databaseutil.MigrationUp(migrationSource, databaseUrl, logger)
	if err != nil {
		logger.Fatal("failed to run database migration", zap.Error(err))
	}

	validator := internal.NewValidator()

	poolConfig, err := pgxpool.ParseConfig(databaseUrl)
	if err != nil {
		logger.Fatal("Failed to parse database URL", zap.Error(err))
	}
	dbPool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil {
		logger.Fatal("Failed to create database connection pool", zap.Error(err))
	}
	defer dbPool.Close()

	problemWriter := problemutil.New()

	questionService := questions.NewService(logger, dbPool)

	questionsHandler := questions.NewHandler(logger, problemWriter, validator, questionService)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/questions", questionsHandler.CreateQuestion)
	mux.HandleFunc("GET /api/questions", questionsHandler.ListQuestion)
	mux.HandleFunc("GET /api/questions/{id}", questionsHandler.GetQuestion)
	mux.HandleFunc("PUT /api/questions/{id}", questionsHandler.UpdateQuestion)
	mux.HandleFunc("DELETE /api/questions/{id}", questionsHandler.DelQuestion)
	mux.HandleFunc("POST /api/questions/{id}/answers", questionsHandler.SubmitAnswer)
	mux.HandleFunc("GET /api/questions/{id}/answers", questionsHandler.ListAnswers)

	logger.Info("Start listening on port: 8080")

	err = http.ListenAndServe(":8080", mux)
	if err != nil {
		panic(err)
	}
}

func initLogger(cfg *config.Config, appMetadata []zap.Field) (*zap.Logger, error) {
	var err error
	var logger *zap.Logger
	if cfg.Debug {
		logger, err = logutil.ZapDevelopmentConfig().Build()
		if err != nil {
			return nil, err
		}
		logger.Info("Running in debug mode", appMetadata...)
	} else {
		logger, err = logutil.ZapProductionConfig().Build()
		if err != nil {
			return nil, err
		}

		logger = logger.With(appMetadata...)
	}
	defer func() {
		err := logger.Sync()
		if err != nil {
			zap.S().Errorw("Failed to sync logger", zap.Error(err))
		}
	}()

	return logger, nil
}
