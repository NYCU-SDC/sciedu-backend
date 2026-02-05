package main

import (
	"log"
	"net/http"
	"sciedu-backend/internal/chatPrototype"
	"sciedu-backend/internal/chatPrototype/mockLLM"
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

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte("ok"))
		if err != nil {
			panic(err)
		}
	})

	llm_mock := mockLLM.NewMockUpstream(logger)
	mux.Handle("/mock-llm", llm_mock)

	chatHttpClient := &http.Client{
		Timeout: 0, // streaming: avoid short timeout; can use Transport timeouts instead later
	}
	chatClient := chatPrototype.NewHTTPSSEClient(logger, chatHttpClient, "http://localhost:8090/mock-llm")
	chatService := chatPrototype.NewService(logger, chatClient)
	chatHandler := chatPrototype.NewHandler(logger, chatService)
	mux.HandleFunc("/chat/stream", chatHandler.Downstream)

	logger.Info("Start listening on port: 8090")

	err = http.ListenAndServe(":8090", mux)
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
