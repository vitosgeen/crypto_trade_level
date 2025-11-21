.PHONY: run build test clean deps

APP_NAME=bot
CMD_PATH=cmd/bot/main.go
BUILD_DIR=bin

run:
	go run $(CMD_PATH)

build:
	mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(APP_NAME) $(CMD_PATH)

test:
	go test -v ./...

clean:
	rm -rf $(BUILD_DIR)
	rm -f bot.db
	rm -f test_e2e.db
	rm -f bot.pid
	rm -f bot.log

deps:
	go mod tidy

start: build
	@echo "Starting bot in background..."
	@nohup ./$(BUILD_DIR)/$(APP_NAME) > bot.log 2>&1 & echo $$! > bot.pid
	@echo "Bot started with PID $$(cat bot.pid)"

stop:
	@if [ -f bot.pid ]; then \
		echo "Stopping bot with PID $$(cat bot.pid)..."; \
		kill $$(cat bot.pid) || true; \
		rm bot.pid; \
		echo "Bot stopped."; \
	else \
		echo "No bot.pid file found."; \
	fi
