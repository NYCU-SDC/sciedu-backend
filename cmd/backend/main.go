package main

import (
	"context"
	"log"
	"net/http"
	"sciedu-backend/databaseutil"
	"sciedu-backend/internal"
	"sciedu-backend/internal/questions"

	// databaseutil "github.com/NYCU-SDC/summer/pkg/databaseutil"
	logutil "github.com/NYCU-SDC/summer/pkg/log"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

func main() {
	logger, err := initLogger()
	if err != nil {
		log.Fatalf("Failed to initalize logger: %v, exiting...", err)
	}

	logger.Info("Hello, World!")

	err = databaseutil.MigrationUp("file://internal/database/migrations", "postgresql://postgres:SciEdu@localhost:5432/postgres?sslmode=disable", logger)
	if err != nil {
		logger.Fatal("failed to run database migration", zap.Error(err))
	}

	validator := internal.NewValidator()

	poolConfig, err := pgxpool.ParseConfig("postgresql://postgres:SciEdu@localhost:5432/postgres?sslmode=disable")
	if err != nil {
		logger.Fatal("Failed to parse database URL", zap.Error(err))
	}
	dbPool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil {
		logger.Fatal("Failed to create database connection pool", zap.Error(err))
	}
	defer dbPool.Close()

	questionService := questions.NewService(logger, dbPool)

	questionsHandler := questions.NewHandler(logger, validator, questionService)

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
