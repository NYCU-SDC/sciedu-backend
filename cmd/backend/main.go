package main

import (
	"context"
	"log"
	"net/http"
	"sciedu-backend/databaseutil"
	"sciedu-backend/internal"
	logutil "sciedu-backend/internal/error"
	"sciedu-backend/internal/questions"

	// databaseutil "github.com/NYCU-SDC/summer/pkg/databaseutil"
	problemutil "github.com/NYCU-SDC/summer/pkg/problem"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

func main() {
	logger, err := logutil.InitLogger()
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
