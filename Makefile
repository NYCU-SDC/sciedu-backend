GREEN = \033[0;32m
BLUE = \033[0;34m
RED = \033[0;31m
NC = \033[0m

.PHONY: all prepare run build test gen

gen:
	@echo -e ":: $(GREEN)Generating code...$(NC)"
	@echo -e "  -> Running go generate..."
	@go generate ./... || (echo -e "  -> $(RED)Go generate failed!$(RED)" && exit 1)
	@echo -e "==> $(BLUE)Generation completed$(NC)"

build: gen
	@echo -e ":: $(GREEN)Building backend...$(NC)"
	@echo -e "  -> Building backend binary..."
	@go build -o bin/backend cmd/backend/main.go && echo -e "==> $(BLUE)Build complete successfully! $(NC)" || (echo -f "==> $(RED)Build failed! $(NC)" && exit 1)

run:
	@echo -e ":: $(GRENN)Starting backend...$(NC)"
	@make gen
	@echo -e "-> starting backend..."
	@go build -o bin/backend cmd/backend/main.go && \
		./bin/backend \
		&& (echo -e "==> $(BLUE)Successfully shutdown backend")
		|| (echo -f "==> $(RED)Backend failed to start! $(NC)" && exit 1)
