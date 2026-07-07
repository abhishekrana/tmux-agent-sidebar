.PHONY: build test install-hooks clean

build:
	go build -o bin/tmux-agent-sidebar ./cmd/tmux-agent-sidebar

test:
	go test ./...

install-hooks: build
	./bin/tmux-agent-sidebar install-hooks

clean:
	rm -rf bin
