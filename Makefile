.PHONY: fmt vet build test race eval bench cover check docker clean

fmt:
	gofmt -w .

vet:
	go vet ./...

build:
	CGO_ENABLED=0 go build ./...

test:
	go test ./...

race:
	go test -race ./...

eval:
	go run ./cmd/memorigo-eval

bench:
	go test ./memori -run '^$$' -bench 'Benchmark(Recall|BuildContext)1000' -benchmem

cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

check: fmt vet build test eval

docker:
	docker build -t memorigo-mcp .

clean:
	rm -f coverage.out
	rm -rf bin
