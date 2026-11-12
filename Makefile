.PHONY: all clear start test request clean help

# Default number of concurrent requests
N ?= 5

# Default target: full pipeline
all: clear start test

# Kill existing server processes and clean up logs
clear:
	@echo "Cleaning up any existing processes on port 8080..."
	@lsof -ti:8080 | xargs kill -9 2>/dev/null || true
	@pkill -9 -f "go run.*main.go" 2>/dev/null || true
	@pkill -9 -f "interview-fm-backend" 2>/dev/null || true
	@rm -f server.log
	@sleep 1
	@echo "Clean complete"

# Start the server with race detector in background
start:
	@echo "Starting server with race detector..."
	@go run -race . > server.log 2>&1 &
	@sleep 2
	@echo "Server started"

# Run tests against an already-running server
# Usage: make test N=10
test:
	@echo "Running tests with N=$(N) concurrent requests..."
	@echo ""
	@cd test && go run main.go -n=$(N); \
	TEST_EXIT=$$?; \
	echo ""; \
	if [ $$TEST_EXIT -eq 0 ]; then \
		echo "✅ Tests PASSED"; \
		echo ""; \
		if grep -q "WARNING: DATA RACE" ../server.log 2>/dev/null; then \
			echo "❌ RACE CONDITIONS DETECTED"; \
			grep -A 10 "WARNING: DATA RACE" ../server.log | head -30; \
			exit 1; \
		else \
			echo "✅ No race conditions detected"; \
		fi; \
	else \
		echo "❌ Tests FAILED"; \
		echo ""; \
		echo "Server logs (last 20 lines):"; \
		tail -20 ../server.log; \
		exit $$TEST_EXIT; \
	fi

# Fire a resize request using req.json
# Usage: make request or make request ASYNC=false
ASYNC ?= true
request:
	@curl -s -X POST "http://localhost:8080/v1/resize?async=$(ASYNC)" \
		-H "Content-Type: application/json" \
		-d @req.json | jq .

# Alias for clear
clean: clear

# Help
help:
	@echo "Available targets:"
	@echo "  make            - Full pipeline: clear + start + test (default, N=5)"
	@echo "  make clear      - Kill server processes and remove logs"
	@echo "  make start      - Start server with race detector in background"
	@echo "  make test       - Run tests against already-running server"
	@echo "  make test N=10  - Run tests with 10 concurrent requests"
	@echo "  make request    - Send resize request from req.json (async=true by default)"
	@echo "  make request ASYNC=false - Send blocking resize request"
	@echo "  make clean      - Alias for clear"
	@echo "  make help       - Show this help message"
	@echo ""
	@echo "Examples:"
	@echo "  make              # Full pipeline with 5 concurrent requests (default)"
	@echo "  make N=100        # Full pipeline with 100 concurrent requests"
	@echo "  make start        # Start server, then run 'make test' separately"
	@echo "  make test N=1000  # Stress test against running server"
	@echo "  make request      # Send async resize request from req.json"
