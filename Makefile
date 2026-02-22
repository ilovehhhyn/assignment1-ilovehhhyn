default: all

all: client server

client: client.go
	go build client.go

server: server.go
	go build server.go

# Run all tests
test: client server
	SOLUTION_DIR=$$(pwd) go test ./tests/ -v > test.log 2>&1; r=$$?; \
	grep -e '--- FAIL' -e '--- SKIP' -e '_test.go:' test.log || true; \
	if [ $$r -eq 0 ]; then echo "ok — all tests passed"; fi; \
	exit $$r

# Run only tests in server_test.go (TestServer*)
test-server: client server
	SOLUTION_DIR=$$(pwd) go test ./tests/ -run TestServer -v > test.log 2>&1; r=$$?; \
	grep -e '--- FAIL' -e '--- SKIP' -e '_test.go:' test.log || true; \
	if [ $$r -eq 0 ]; then echo "ok — all server tests passed"; fi; \
	exit $$r

# Run only tests in client_test.go (TestClient*)
test-client: client server
	SOLUTION_DIR=$$(pwd) go test ./tests/ -run TestClient -v > test.log 2>&1; r=$$?; \
	grep -e '--- FAIL' -e '--- SKIP' -e '_test.go:' test.log || true; \
	if [ $$r -eq 0 ]; then echo "ok — all client tests passed"; fi; \
	exit $$r

clean:
	rm -f server client test.log