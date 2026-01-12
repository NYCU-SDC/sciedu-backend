package main

import (
	"log"
	"net/http"
	"sciedu-backend/internal"
	"sciedu-backend/internal/questions"

	// databaseutil "github.com/NYCU-SDC/summer/pkg/database"
	logutil "github.com/NYCU-SDC/summer/pkg/log"
	"go.uber.org/zap"
)

func main() {
	logger, err := initLogger()
	if err != nil {
		log.Fatalf("Failed to initalize logger: %v, exiting...", err)
	}

	logger.Info("Hello, World!")

	validator := internal.NewValidator()

	questionService := questions.NewService(logger)

	questionsHandler := questions.NewHandler(logger, validator, questionService)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/questions", questionsHandler.Create)
	mux.HandleFunc("GET /api/questions", questionsHandler.List)

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
