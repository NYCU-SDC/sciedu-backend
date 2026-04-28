package main

import (
	"context"
	"log"
	"net/http"
	"time"

	databaseutil "github.com/NYCU-SDC/summer/pkg/database"
	logutil "github.com/NYCU-SDC/summer/pkg/log"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"sciedu-backend/internal/chat"
	"sciedu-backend/internal/config"
	"sciedu-backend/internal/question"
)

func main() {
	cfg, configLogger := config.Load()

	logger, err := initLogger(cfg)
	if err != nil {
		log.Fatalf("Failed to initalize logger: %v, exiting...", err)
	}
	defer func() {
		if err := logger.Sync(); err != nil {
			zap.S().Errorw("Failed to sync logger", zap.Error(err))
		}
	}()

	configLogger.FlushToZap(logger)

	if cfg.DatabaseURL == "" {
		logger.Fatal("database url is required")
	}

	if err := databaseutil.MigrationUp(cfg.MigrationSource, cfg.DatabaseURL, logger); err != nil {
		logger.Fatal("failed to migrate database", zap.Error(err))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Fatal("failed to connect database", zap.Error(err))
	}
	defer db.Close()

	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte("OK"))
		if err != nil {
			logger.Error("Failed to write response", zap.Error(err))
		}
	})

	questionRepo := question.NewRepository(db, logger)
	questionService := question.NewService(questionRepo)
	question.NewHandler(questionService, logger).Register(mux)

	chatRepo := chat.NewRepository(db, logger)
	llmClient := chat.NewLLMClient(cfg.LLMModuleURL, &http.Client{Timeout: 0})
	chatService := chat.NewService(chatRepo, llmClient, logger)
	chat.NewHandler(chatService, logger).Register(mux)

	addr := cfg.Host + ":" + cfg.Port
	logger.Info("Start listening", zap.String("addr", addr))

	err = http.ListenAndServe(addr, mux)
	if err != nil {
		panic(err)
	}
}

func initLogger(cfg config.Config) (*zap.Logger, error) {
	var logger *zap.Logger

	var err error
	if cfg.Debug {
		logger, err = logutil.ZapDevelopmentConfig().Build()
	} else {
		logger, err = logutil.ZapProductionConfig().Build()
	}
	if err != nil {
		return nil, err
	}

	return logger, nil
}
