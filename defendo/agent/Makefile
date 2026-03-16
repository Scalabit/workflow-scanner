TRUFFLEHOG_VERSION := 3.93.8
TRUFFLEHOG_TAR     := trufflehog_$(TRUFFLEHOG_VERSION)_windows_amd64.tar.gz
TRUFFLEHOG_URL     := https://github.com/trufflesecurity/trufflehog/releases/download/v$(TRUFFLEHOG_VERSION)/$(TRUFFLEHOG_TAR)
TRUFFLEHOG_OUT     := checks/tools/trufflehog.exe

OSQUERY_VERSION    := 5.21.0
OSQUERY_ZIP        := osquery-$(OSQUERY_VERSION).windows_x86_64.zip
OSQUERY_URL        := https://github.com/osquery/osquery/releases/download/$(OSQUERY_VERSION)/$(OSQUERY_ZIP)
OSQUERY_OUT        := checks/tools/osqueryi.exe

.PHONY: build download-tools clean test lint check

## Download TruffleHog + osquery Windows binaries (run once before building).
download-tools: $(TRUFFLEHOG_OUT) $(OSQUERY_OUT)

$(TRUFFLEHOG_OUT):
	@echo "Downloading TruffleHog v$(TRUFFLEHOG_VERSION)..."
	curl -fsSL -o /tmp/$(TRUFFLEHOG_TAR) $(TRUFFLEHOG_URL)
	tar -xzf /tmp/$(TRUFFLEHOG_TAR) -C checks/tools/ trufflehog.exe
	rm /tmp/$(TRUFFLEHOG_TAR)
	@echo "TruffleHog downloaded to $(TRUFFLEHOG_OUT)"

$(OSQUERY_OUT):
	@echo "Downloading osquery v$(OSQUERY_VERSION)..."
	curl -fsSL -o /tmp/$(OSQUERY_ZIP) $(OSQUERY_URL)
	unzip -o /tmp/$(OSQUERY_ZIP) "osquery-$(OSQUERY_VERSION).windows_x86_64/Program Files/osquery/osqueryi.exe" -d /tmp/osquery-extract/
	mv "/tmp/osquery-extract/osquery-$(OSQUERY_VERSION).windows_x86_64/Program Files/osquery/osqueryi.exe" $(OSQUERY_OUT)
	rm -rf /tmp/$(OSQUERY_ZIP) /tmp/osquery-extract/
	@echo "osqueryi downloaded to $(OSQUERY_OUT)"

## Build the Windows agent binary (downloads tools first if needed).
build: $(TRUFFLEHOG_OUT) $(OSQUERY_OUT)
	GOOS=windows GOARCH=amd64 go build -o guardify-agent.exe .
	@echo "Built guardify-agent.exe"

## Run unit tests (compile-only cross-compilation for Windows).
test:
	GOOS=windows GOARCH=amd64 go test -c -o /dev/null ./...

## Run golangci-lint (install from https://golangci-lint.run/welcome/install/).
lint:
	GOOS=windows golangci-lint run ./...

## Run tests + lint.
check: test lint

clean:
	rm -f guardify-agent.exe $(TRUFFLEHOG_OUT)
