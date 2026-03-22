package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"sciedu-backend/internal/content"

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

	questionQueries := question.New(pool)
	questionService := question.NewQuestionService(questionQueries, logger)
	optionService := question.NewOptionService(questionQueries, logger)
	questionHandler := question.NewHandler(questionService, optionService, logger)

	contentQueries := content.New(pool)
	contentService := content.NewService(contentQueries, logger)
	contentHandler := content.NewHandler(contentService, logger)

	mux := http.NewServeMux()

	corsMiddleware := cors.NewMiddleware(logger, []string{"*"})
	middlewareSet := middlewareutil.NewSet(
		corsMiddleware.HandlerFunc,
	)

	questionHandler.RegisterRoutes(mux, middlewareSet)
	contentHandler.RegisterRoutes(mux, middlewareSet)

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

// initDatabasePool creates a new pgxpool.Pool with the given database URL in the config, it uses the default config
// provided by pgxpool.ParseConfig:
//
//   - pool_max_conns: 4
//   - pool_min_conns: 0
//   - pool_max_conn_lifetime: 1 hour
//   - pool_max_conn_idle_time: 30 minutes
//   - pool_health_check_period: 1 minute
//   - pool_max_conn_lifetime_jitter: 0
func initDatabasePool(databaseURL string) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		log.Fatalf("Unable to parse config: %v", err)
	}

	pool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil {
		return nil, err
	}

	return pool, nil
}
