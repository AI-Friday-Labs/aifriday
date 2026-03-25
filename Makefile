.PHONY: build clean test brief

build:
	go build -o ai-friday-bot ./cmd/srv
	go build -o gen-brief ./cmd/brief

clean:
	rm -f ai-friday-bot gen-brief

test:
	go test ./...

brief:
	./gen-brief --dry-run

brief-post:
	./gen-brief

restart: build
	sudo systemctl restart srv
