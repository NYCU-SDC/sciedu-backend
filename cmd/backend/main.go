package main

import (
	"log"
	"net/http"
	"sciedu-backend/internal/chat"
	"sciedu-backend/internal/chat/mockLLM"

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

	mux := http.NewServeMux()

	//----------------Mock LLM Endpoint----------------
	mockLLMServer := mockLLM.NewMockLLM()
	mux.HandleFunc("POST /mock-llm", mockLLMServer.Handle)
	//-------------------------------------------------

	chatProvider := chat.NewProvider(mockLLMServer.URL(), &http.Client{}, nil)
	chatService := chat.NewService(chatProvider, logger)
	chatHandler := chat.NewHandler(chatService, logger)

	mux.HandleFunc("/chat/stream", chatHandler.StreamChat)

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte("ok"))
		if err != nil {
			panic(err)
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
