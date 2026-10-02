.PHONY: build-frontend build run test lint fmt clean

build-frontend:
	cd web && npm ci --ignore-scripts && npm run build
	mkdir -p internal/webassets/dist
	rm -rf internal/webassets/dist/*
	cp -r web/dist/. internal/webassets/dist/

build: build-frontend
	go build -o groundtruth ./cmd/groundtruth

run: build
	./groundtruth

test:
	go test ./... -race

lint:
	golangci-lint run

fmt:
	gofmt -l .

clean:
	rm -f groundtruth
	rm -rf internal/webassets/dist/*
