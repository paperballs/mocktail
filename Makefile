.PHONY: clean lint test build

default: clean lint test build

lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.3.0 run

clean:
	rm -rf cover.out

test: clean
	go test -v -cover ./...

build: clean
	CGO_ENABLED=0 go build -trimpath -ldflags '-w -s'
