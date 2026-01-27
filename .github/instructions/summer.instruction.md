# Summer Library Usage Guide

This document outlines the standard patterns and utilities provided by the `summer` library (`github.com/NYCU-SDC/summer`). When developing Go code for projects using this library, follow these guidelines strictly to ensure consistency and maintainability.

## 1. Project Initialization (CLI)

The `summer` CLI is used to scaffold projects.

- **Command:** `summer -b main init`
    
- **Structure Created:**
    
    - `cmd/main.go`: Entry point.
        
    - `internal/`: Application logic.
        
    - `scripts/`: Helper scripts (e.g., `create_full_schema.sh`).
        

## 2. Package Usage Guidelines

### 2.1 Configuration (`pkg/config`)

- **Usage:** Use `configutil.Merge[T]` to handle configuration overrides (e.g., merging default config with environment-specific config).
    
- **Pattern:**
    
    ```go
    import configutil "[github.com/NYCU-SDC/summer/pkg/config](https://github.com/NYCU-SDC/summer/pkg/config)"
    
    func LoadConfig() (*Config, error) {
        defaults := &Config{...}
        overrides := &Config{...} // Load from file/env
        return configutil.Merge(defaults, overrides)
    }
    ```
    

### 2.2 HTTP Handlers (`pkg/handler`)

- **Request Parsing:** Always use `handlerutil.ParseAndValidateRequestBody` to unmarshal JSON and validate struct tags (using `go-playground/validator/v10`).
    
- **Response Writing:** Use `handlerutil.WriteJSONResponse` for successful (2xx) responses.
    
- **UUID Parsing:** Use `handlerutil.ParseUUID` to safely parse UUID strings.
    
- **Standard Errors:** Use defined errors like `ErrNotFound`, `ErrForbidden`, etc., in your business logic.
    

**Example Handler Pattern:**

```go
import (
    handlerutil "[github.com/NYCU-SDC/summer/pkg/handler](https://github.com/NYCU-SDC/summer/pkg/handler)"
    "[github.com/go-playground/validator/v10](https://github.com/go-playground/validator/v10)"
)

type CreateUserRequest struct {
    Name string `json:"name" validate:"required"`
}

func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
    var req CreateUserRequest
    if err := handlerutil.ParseAndValidateRequestBody(r.Context(), h.validator, r, &req); err != nil {
        // Let the problem writer handle validation errors
        h.problemWriter.WriteError(r.Context(), w, err, h.logger)
        return
    }

    // ... business logic ...

    handlerutil.WriteJSONResponse(w, http.StatusCreated, response)
}
```

### 2.3 Error Handling & Problem Details (`pkg/problem`)

- **Concept:** Implements RFC 7807 Problem Details.
    
- **Usage:** Inject `problem.HttpWriter` into your handlers.
    
- **Writing Errors:** Use `WriteError(ctx, w, err, logger)`. It automatically maps standard errors (from `pkg/handler`, `pkg/database`, etc.) to appropriate HTTP status codes and JSON responses.
    
- **Custom Mapping:** You can provide a custom mapping function via `problem.NewWithMapping`.
    

### 2.4 Database (`pkg/database`)

- **Error Wrapping:** **CRITICAL**. Always wrap database errors using `databaseutil.WrapDBError` or `databaseutil.WrapDBErrorWithKeyValue` before returning them from the repository layer.
    
    - This converts low-level Postgres errors (unique violation, FK violation, deadlock) into domain errors.
        
- **Migrations:** Use `databaseutil.MigrationUp` and `databaseutil.MigrationDown` to handle schema changes programmatically.
    

**Repository Pattern:**

```go
import databaseutil "[github.com/NYCU-SDC/summer/pkg/database](https://github.com/NYCU-SDC/summer/pkg/database)"

func (r *Repo) Create(ctx context.Context, user *User) error {
    _, err := r.db.Exec(ctx, "INSERT ...")
    // Operation name helps with logging
    return databaseutil.WrapDBError(err, r.logger, "create user")
}
```

### 2.5 Logging (`pkg/log`)

- **Configuration:** Use `logutil.ZapProductionConfig()` or `logutil.ZapDevelopmentConfig()` based on the environment.
    
- **Context Awareness:** Use `logutil.WithContext(ctx, logger)` within handlers to automatically attach Trace IDs and Span IDs to logs for observability.
    

### 2.6 Middleware (`pkg/middleware`, `pkg/cors`, `pkg/trace`)

- **Chaining:** Use `middleware.NewSet()` to create a middleware chain.
    
- **Standard Stack:**
    
    1. `traceutil.TraceMiddleware`: Starts OpenTelemetry spans.
        
    2. `traceutil.RecoverMiddleware`: Recovers from panics and logs them.
        
    3. `cors.CORSMiddleware`: Handles CORS headers.
        
- **Usage:**
    
    ```go
    mw := middleware.NewSet(
        func(next http.HandlerFunc) http.HandlerFunc {
            return traceutil.TraceMiddleware(next, logger)
        },
        // ... add others
    )
    finalHandler := mw.HandlerFunc(myHandler)
    ```
    

### 2.7 Pagination (`pkg/pagination`)

- **Factory:** Initialize `pagination.Factory[T]` with `MaxPageSize` and `SortableColumns`.
    
- **Request:** Use `factory.GetRequest(r)` to parse `page`, `size`, `sort`, and `sortBy` from query parameters.
    
- **Response:** Use `factory.NewResponse(...)` to generate a standardized paginated JSON response.
    

## 3. Recommended Project Structure

When generating a full application, adhere to this structure:

```
.
├── cmd/
│   └── main.go           # Entry point, dependency injection, server start
├── internal/
│   ├── config/           # App configuration
│   ├── controller/       # HTTP handlers (uses pkg/handler, pkg/problem)
│   ├── repository/       # Database access (uses pkg/database)
│   ├── service/          # Business logic
│   └── entity/           # Domain models
├── scripts/
│   └── create_full_schema.sh # SQLC helper
└── go.mod
```

## 4. SQL Generation (sqlc)

- If `sqlc` is used, utilizing `resource/scripts/create_sqlc_full_schema.sh` is recommended to merge split SQL schema files into a single file for generation.