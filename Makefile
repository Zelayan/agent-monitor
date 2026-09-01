.PHONY: all build build-monitor build-reporter build-all test clean

BIN_DIR := bin
DIST_DIR := dist
LDFLAGS := -s -w

all: build

build: build-monitor build-reporter

build-monitor:
	@mkdir -p $(BIN_DIR)
	go build -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/agent-monitor main.go
	@echo "✓ built $(BIN_DIR)/agent-monitor"

build-reporter:
	@mkdir -p $(BIN_DIR)
	go build -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/agent-reporter cmd/reporter/main.go
	@echo "✓ built $(BIN_DIR)/agent-reporter"

# 跨平台全矩阵静态编译 (CGO_ENABLED=0，纯 Go 标准库)
build-all: clean
	@mkdir -p $(DIST_DIR)
	@echo "Building cross-platform releases..."
	
	# macOS Apple Silicon
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o $(DIST_DIR)/agent-monitor-darwin-arm64 main.go
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o $(DIST_DIR)/agent-reporter-darwin-arm64 cmd/reporter/main.go
	
	# macOS Intel
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(DIST_DIR)/agent-monitor-darwin-amd64 main.go
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(DIST_DIR)/agent-reporter-darwin-amd64 cmd/reporter/main.go
	
	# Linux x86_64
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(DIST_DIR)/agent-monitor-linux-amd64 main.go
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(DIST_DIR)/agent-reporter-linux-amd64 cmd/reporter/main.go
	
	# Linux ARM64
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o $(DIST_DIR)/agent-monitor-linux-arm64 main.go
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o $(DIST_DIR)/agent-reporter-linux-arm64 cmd/reporter/main.go
	
	# Windows x86_64
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(DIST_DIR)/agent-monitor-windows-amd64.exe main.go
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(DIST_DIR)/agent-reporter-windows-amd64.exe cmd/reporter/main.go
	
	@echo "✓ All platform binaries successfully generated in $(DIST_DIR)/"

test:
	go test -v -race ./...

clean:
	rm -rf $(BIN_DIR) $(DIST_DIR)
