COMPOSE ?= $(shell if docker compose version >/dev/null 2>&1; then echo 'docker compose'; else echo 'docker-compose'; fi)
GOLANGCI_LINT ?= golangci-lint

.PHONY: help build up down restart logs clean test test-race test-integration test-e2e lint

help:
	@echo "Доступные команды:"
	@echo "  make build     - Собрать Docker образы"
	@echo "  make up        - Собрать и запустить сервис"
	@echo "  make down      - Остановить сервис"
	@echo "  make restart   - Перезапустить сервис"
	@echo "  make logs      - Показать логи"
	@echo "  make clean     - Остановить сервис и удалить данные БД"
	@echo "  make test      - Запустить тесты"
	@echo "  make test-race - Запустить тесты с детектором гонок"
	@echo "  make test-integration - Тесты с race и PostgreSQL (PR_SERVICE_TEST_DATABASE_URL)"
	@echo "  make test-e2e  - Проверить запущенный API (PR_SERVICE_E2E_URL)"
	@echo "  make lint      - Проверить код и форматирование (golangci-lint v2)"

build:
	$(COMPOSE) build

up:
	$(COMPOSE) up -d --build

down:
	$(COMPOSE) down

restart:
	$(COMPOSE) restart

logs:
	$(COMPOSE) logs -f

clean:
	$(COMPOSE) down -v

test:
	go test ./...

test-race:
	go test -race ./...

test-integration:
	@test -n "$$PR_SERVICE_TEST_DATABASE_URL" || (echo 'Укажите PR_SERVICE_TEST_DATABASE_URL для тестовой PostgreSQL'; exit 1)
	go test -race -count=1 ./...

test-e2e:
	@test -n "$(PR_SERVICE_E2E_URL)" || (echo 'Укажите PR_SERVICE_E2E_URL для тестового API'; exit 1)
	go test -race -count=1 -v ./tests/e2e

lint:
	$(GOLANGCI_LINT) run ./...
	$(GOLANGCI_LINT) fmt --diff
