package main

import (
	"context"
	"log"
	"net/http"
	"sciedu-backend/internal/chat"

	"sciedu-backend/internal/config"

	"sciedu-backend/internal/cors"
	"sciedu-backend/internal/question"

	databaseutil "github.com/NYCU-SDC/summer/pkg/database"
	logutil "github.com/NYCU-SDC/summer/pkg/log"
	middlewareutil "github.com/NYCU-SDC/summer/pkg/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

func main() {
	logger, err := initLogger()
	if err != nil {
		log.Fatalf("Failed to initalize logger: %v, exiting...", err)
	}

	logger.Info("Hello, World!")

	cfg, configLogger := config.Load()
	configLogger.FlushToZap(logger)

	err = databaseutil.MigrationUp(cfg.MigrationSource, cfg.DatabaseURL, logger)
	if err != nil {
		logger.Fatal("Failed to run database migration", zap.Error(err))
	}

	pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		logger.Fatal("Failed to initialize database pool", zap.Error(err))
	}
	defer pool.Close()

	if err = pool.Ping(context.Background()); err != nil {
		logger.Fatal("Failed to connect to database", zap.Error(err))
	}

	questionStore := question.NewStore(pool)
	optionService := question.NewOptionService(questionStore, logger)
	questionService := question.NewQuestionService(questionStore, optionService, logger)
	questionHandler := question.NewHandler(questionService, optionService, logger)

	chatQueriers := chat.New(pool)
	chatProvider := chat.NewProvider(cfg.LLMURL+"/chat", &http.Client{}, nil)
	chatStreamHub := chat.NewStreamHub()
	chatService := chat.NewService(chatProvider, chatQueriers, chatStreamHub, logger)
	chatHandler := chat.NewHandler(chatService, logger)
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

	mux.HandleFunc("POST /api/chat", chatHandler.CreateChat)
	mux.HandleFunc("GET /api/chat/stream/{messageID}", chatHandler.Stream)
	mux.HandleFunc("GET /api/chat/{chatID}", chatHandler.GetChat)
	mux.HandleFunc("POST /api/chat/{chatID}", chatHandler.CreateMessage)

	logger.Info("Start listening on port: 8080")

	err = http.ListenAndServe(":8080", mux)
	if err != nil {
		panic(err)
	}
}

func initLogger() (*zap.Logger, error) {
	var logger *zap.Logger

	logger, err := logutil.ZapProductionConfig().Build()
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
