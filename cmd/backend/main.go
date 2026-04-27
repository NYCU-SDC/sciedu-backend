package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"sciedu-backend/internal/chat"

	databaseutil "github.com/NYCU-SDC/summer/pkg/database"
	"github.com/jackc/pgx/v5/pgxpool"

	// databaseutil "github.com/NYCU-SDC/summer/pkg/database"
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

	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		logger.Fatal("Failed to initialize database pool", zap.Error(err))
	}
	defer pool.Close()

	if err = pool.Ping(context.Background()); err != nil {
		logger.Fatal("Failed to connect to database", zap.Error(err))
	}

	chatQueriers := chat.New(pool)
	chatProvider := chat.NewProvider(os.Getenv("LLM_URL")+"/chat", &http.Client{}, nil)
	chatStreamHub := chat.NewStreamHub()
	chatService := chat.NewService(chatProvider, chatQueriers, chatStreamHub, logger)
	chatHandler := chat.NewHandler(chatService, logger)

	mux := http.NewServeMux()

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
	mux.HandleFunc("GET /api/chat/messages/{chatID}", chatHandler.GetChat)
	mux.HandleFunc("POST /api/chat/messages/{chatID}", chatHandler.CreateMessage)

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
