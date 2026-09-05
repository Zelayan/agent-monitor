.PHONY: all build build-monitor build-reporter build-all test clean extension bundle-extension-binaries vsix

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

# 本地代码智能审查（对比 master 或未提交更改）
review:
	@python3 scripts/ai_reviewer.py --local

# 本地严格代码审查（若有 BLOCK 阻断问题退出码为 1）
review-strict:
	@python3 scripts/ai_reviewer.py --local --strict

# 提 PR 前的一键自动化质检闭环：跑测试 + 严格本地 AI 审查
pre-pr: test review-strict
	@echo "✓ All pre-PR checks and local AI reviews passed cleanly. Ready to create PR."

# 安装可选的 git pre-push hook（推送前自动执行本地 review）
install-hooks:
	@mkdir -p .git/hooks
	@echo '#!/bin/sh' > .git/hooks/pre-push
	@echo 'echo "==> Running pre-push AI code review..."' >> .git/hooks/pre-push
	@echo 'python3 scripts/ai_reviewer.py --local --strict' >> .git/hooks/pre-push
	@chmod +x .git/hooks/pre-push
	@echo "✓ Installed git pre-push hook at .git/hooks/pre-push"

extension:
	@echo "Building Cursor / VS Code extension..."
	cd extensions/cursor && npm run build
	@echo "✓ Extension bundled successfully in extensions/cursor/dist/"

bundle-extension-binaries:
	@echo "Bundling cross-platform binaries into extensions/cursor/bin/..."
	bash scripts/bundle_cursor_binaries.sh

vsix: bundle-extension-binaries extension
	@echo "Packaging cross-platform VSIX archive..."
	mkdir -p $(DIST_DIR)
	cd extensions/cursor && npx --yes @vscode/vsce package --no-dependencies --out $(CURDIR)/$(DIST_DIR)/agent-monitor-cursor-$(shell node -p "require('./extensions/cursor/package.json').version").vsix
	@echo "✓ VSIX package generated in $(DIST_DIR)/"

clean:
	rm -rf $(BIN_DIR) $(DIST_DIR) extensions/cursor/dist extensions/cursor/bin extensions/cursor/node_modules
