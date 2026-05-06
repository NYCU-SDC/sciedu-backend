package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"sciedu-backend/internal/cors"
	"sciedu-backend/internal/question"

	databaseutil "github.com/NYCU-SDC/summer/pkg/database"
	logutil "github.com/NYCU-SDC/summer/pkg/log"
	middlewareutil "github.com/NYCU-SDC/summer/pkg/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
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

	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		logger.Fatal("Failed to initialize database pool", zap.Error(err))
	}
	defer pool.Close()

	if err = pool.Ping(context.Background()); err != nil {
		logger.Fatal("Failed to connect to database", zap.Error(err))
	}

	queries := question.New(pool)
	optionService := question.NewOptionService(queries, logger)
	questionService := question.NewQuestionService(queries, optionService, logger)
	questionHandler := question.NewHandler(questionService, optionService, logger)

	mux := http.NewServeMux()

	corsMiddleware := cors.NewMiddleware(logger, []string{"*"})
	middlewareSet := middlewareutil.NewSet(
		corsMiddleware.HandlerFunc,
	)

	questionHandler.RegisterRoutes(mux, middlewareSet)
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
