.PHONY: run build test clean deps start stop force-stop

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
	rm -f funding_bot.log

deps:
	go mod tidy

start: build
	rm -f funding_bot.log
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

restart: stop start

force-stop:
	@echo "Force stopping all bot processes..."
	@# Kill process holding the port first
	@fuser -k 8078/tcp >/dev/null 2>&1 || true
	@# Kill the binary if it exists
	@pkill -x "bot" || true
	@# Kill go run process, being careful not to kill make
	@# Kill go run process, being careful not to kill make
	@pgrep -f "go run.*cmd/bot/main.go" | grep -v $$ | xargs -r kill -9 || true
	@rm -f bot.pid
	@echo "All bot processes stopped and port 8078 freed."

analyze:
	@go run cmd/analyzer/main.go
