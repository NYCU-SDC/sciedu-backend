package main

import (
	"context"
	"log"
	"net/http"
	"strings"

	"sciedu-backend/internal/auth"
	"sciedu-backend/internal/chat"
	"sciedu-backend/internal/config"
	"sciedu-backend/internal/content"
	"sciedu-backend/internal/cors"
	"sciedu-backend/internal/course"
	"sciedu-backend/internal/experiment"
	"sciedu-backend/internal/page"
	"sciedu-backend/internal/question"
	"sciedu-backend/internal/user"

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
	if err := cfg.Validate(); err != nil {
		logger.Fatal("Invalid configuration", zap.Error(err))
	}

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
	answerService := question.NewAnswerService(questionStore, questionService, logger)
	correctAnswerService := question.NewCorrectAnswerService(questionStore, questionService, logger)
	questionHandler := question.NewHandler(questionService, answerService, correctAnswerService, logger).
		WithSubmission(question.NewAnswerSubmissionOrchestrator(answerService, questionStore, questionStore))
	syncContext, cancelSync := context.WithCancel(context.Background())
	defer cancelSync()
	go question.RunAnswerSynchronization(syncContext, questionStore, logger)

	contentQueries := content.New(pool)
	contentService := content.NewService(contentQueries, logger)
	contentHandler := content.NewHandler(contentService, logger)

	chatQueriers := chat.New(pool)
	chatProvider := chat.NewProvider(cfg.LLMURL+"/chat", &http.Client{}, nil)
	chatStreamHub := chat.NewStreamHub()
	chatService := chat.NewService(chatProvider, chatQueriers, chatStreamHub, logger)
	chatHandler := chat.NewHandler(chatService, logger)
	mux := http.NewServeMux()

	allowOrigins := parseAllowOrigins(cfg.AllowOrigins)
	corsMiddleware := cors.NewMiddleware(logger, allowOrigins)
	middlewareSet := middlewareutil.NewSet(
		corsMiddleware.HandlerFunc,
	)
	authStore := auth.NewStore(pool)
	oauthProvider, err := initGoogleOAuthProvider(cfg)
	if err != nil {
		logger.Fatal("Failed to initialize Google OAuth provider", zap.Error(err))
	}
	authService := auth.NewService(authStore, auth.ServiceConfig{
		Secret:                cfg.Secret,
		Environment:           cfg.Environment,
		OAuthProvider:         oauthProvider,
		RedirectURLAllowlist:  parseAllowOrigins(cfg.AuthRedirectAllowlist),
		RedirectPreviewDomain: cfg.AuthRedirectPreviewDomain,
		BootstrapAdminEmail:   cfg.AuthBootstrapAdminEmail,
	}, logger)
	authHandler := auth.NewHandler(authService, auth.CookieConfig{
		Environment: cfg.Environment,
	}, logger)
	authMiddleware := auth.NewMiddleware(authService, logger)
	protectedMiddlewareSet := middlewareSet.Append(authMiddleware.HandlerFunc)
	authorizer := auth.NewAuthorizer(authStore, logger)
	questionHandler.WithResults(question.NewAnswerResultService(questionStore, questionStore, logger), authStore)

	userStore := user.NewStore(pool)
	userService := user.NewService(userStore, logger)
	userHandler := user.NewHandler(userService, logger)
	experimentStore := experiment.NewStore(pool)
	experimentService := experiment.NewServiceWithRoles(experimentStore, authStore, logger)
	experimentHandler := experiment.NewHandler(experimentService, logger)

	courseStore := course.NewStore(pool)
	courseService := course.NewService(courseStore, authStore, courseStore, logger)
	courseHandler := course.NewHandler(courseService, logger)

	pageStore := page.NewStore(pool)
	blockService := page.NewBlockService(pageStore, pageStore, contentService, questionService, logger)
	pageService := page.NewPageService(pageStore, blockService, courseService, logger)
	pageHandler := page.NewHandler(pageService, blockService, logger)

	// Health check route
	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte("OK"))
		if err != nil {
			logger.Error("Failed to write response", zap.Error(err))
		}
	})

	authHandler.RegisterRoutes(mux, middlewareSet)
	userHandler.RegisterRoutes(mux, protectedMiddlewareSet, authorizer)
	courseHandler.RegisterRoutes(mux, protectedMiddlewareSet, authorizer)
	experimentHandler.RegisterRoutes(mux, protectedMiddlewareSet, authorizer)
	questionHandler.RegisterRoutes(mux, protectedMiddlewareSet, authorizer)
	contentHandler.RegisterRoutes(mux, protectedMiddlewareSet)
	chatHandler.RegisterRoutes(mux, protectedMiddlewareSet)
	pageHandler.RegisterRoutes(mux, protectedMiddlewareSet, authorizer)

	logger.Info("Start listening on port: 8080")

	err = http.ListenAndServe(":8080", middlewareSet.HandlerFunc(mux.ServeHTTP))
	if err != nil {
		panic(err)
	}
}

func initGoogleOAuthProvider(cfg config.Config) (auth.OAuthProvider, error) {
	googleClientID := cfg.GoogleOAuthClientID
	googleClientSecret := cfg.GoogleOAuthClientSecret
	googleRedirectURL := cfg.GoogleOAuthRedirectURL
	if cfg.GoogleOAuthCredentialsFile != "" {
		credentials, err := auth.LoadGoogleOAuthCredentials(cfg.GoogleOAuthCredentialsFile)
		if err != nil {
			return nil, err
		}
		if googleClientID == "" {
			googleClientID = credentials.ClientID
		}
		if googleClientSecret == "" {
			googleClientSecret = credentials.ClientSecret
		}
		if googleRedirectURL == "" && len(credentials.RedirectURIs) > 0 {
			googleRedirectURL = credentials.RedirectURIs[0]
		}
	}

	if cfg.Environment == auth.EnvironmentDev && googleClientID == "" && googleClientSecret == "" {
		return nil, nil
	}
	if googleClientID == "" && googleClientSecret == "" && googleRedirectURL == "" {
		return nil, nil
	}

	return auth.NewGoogleOAuthProvider(auth.GoogleOAuthConfig{
		ClientID:     googleClientID,
		ClientSecret: googleClientSecret,
		RedirectURL:  googleRedirectURL,
		HTTPClient:   http.DefaultClient,
	})
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

func parseAllowOrigins(origins string) []string {
	if origins == "" {
		return nil
	}
	parts := strings.Split(origins, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimRight(strings.TrimSpace(part), "/")
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
