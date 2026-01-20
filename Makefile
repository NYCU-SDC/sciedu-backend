GREEN = \033[0;32m
BLUE = \033[0;34m
RED = \033[0;31m
NC = \033[0m

.PHONY: all prepare run build test gen

all: build

prepare:
	@echo -e ":: $(GREEN) Preparing environment...$(NC)"
	@echo -e "-> Downloading go dependencies..."
	@go mod download \
		|| (echo -e "-> $(RED) Failed to download go dependencies$(NC)" && exit 1)
	@echo -e "==> $(BLUE)Environment preparation completed$(NC)"

gen:
	@echo -e ":: $(GREEN)Generating schema and code...$(NC)"
	@echo -e "  -> Running schema creation script..."
	@./scripts/create_sqlc_full_schema.sh || (echo -e "  -> $(RED)Schema creation failed$(NC)" && exit 1)
	@echo -e "  -> Generating SQLC code..."
	@sqlc generate || (echo -e "  -> $(RED)SQLC generation failed$(NC)" && exit 1)
	@echo -e "  -> Running go generate..."
	@go generate ./... || (echo -e "  -> $(RED)Go generate failed!$(RED)" && exit 1)
	@echo -e "==> $(BLUE)Generation completed$(NC)"

build: gen
	@echo -e ":: $(GREEN)Building backend...$(NC)"
	@echo -e "  -> Building backend binary..."
	@go build -o bin/backend cmd/backend/main.go && echo -e "==> $(BLUE)Build complete successfully! $(NC)" || (echo -e "==> $(RED)Build failed! $(NC)" && exit 1)

run:
	@echo -e ":: $(GRENN)Starting backend...$(NC)"
	@make gen
	@echo -e "-> starting backend..."
	@go build -o bin/backend cmd/backend/main.go && \
		./bin/backend \
		&& (echo -e "==> $(BLUE)Successfully shutdown backend! $(NC)") \
		|| (echo -e "==> $(RED)Backend failed to start! $(NC)" && exit 1)
