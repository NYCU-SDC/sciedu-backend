package main

import (
	"log"
	"net/http"
	"os"

	databaseutil "github.com/NYCU-SDC/summer/pkg/database"
	logutil "github.com/NYCU-SDC/summer/pkg/log"
	"github.com/joho/godotenv"
	"go.uber.org/zap"
)

func main() {
	logger, err := initLogger()
	if err != nil {
		log.Fatalf("Failed to initalize logger: %v, exiting...", err)
	}

	logger.Info("Hello, World!")

	err = godotenv.Load()
	if err != nil {
		logger.Warn("No .env file loaded, using environment variables", zap.Error(err))
	}

	migrationSource := os.Getenv("MIGRATION_SOURCE")
	databaseURL := os.Getenv("DATABASE_URL")
	err = databaseutil.MigrationUp(migrationSource, databaseURL, logger)
	if err != nil {
		logger.Fatal("Failed to run database migration", zap.Error(err))
	}

	mux := http.NewServeMux()

	// Health check route
	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte("OK"))
		if err != nil {
			logger.Error("Failed to write response", zap.Error(err))
		}
	})

	logger.Info("Start listening on port: 8080")

	err = http.ListenAndServe(":8080", mux)
	if err != nil {
		panic(err)
	}
}

func initLogger() (*zap.Logger, error) {
	var logger *zap.Logger

	logger, err := logutil.ZapDevelopmentConfig().Build()
	if err != nil {
		return nil, err
	}

	defer func() {
		err := logger.Sync()
		if err != nil {
			zap.S().Errorw("Failed to sync logger", zap.Error(err))
		}
	}()

	return logger, nil
}
