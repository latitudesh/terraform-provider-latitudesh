TEST?=$$(go list ./... | grep -v 'vendor')
HOSTNAME=latitude.sh
NAMESPACE=iac
NAME=latitudesh
BINARY=terraform-provider-${NAME}
VERSION=0.0.1
# Automatically detect OS and architecture
OS=$(shell go env GOOS)
ARCH=$(shell go env GOARCH)
OS_ARCH=$(OS)_$(ARCH)

default: install

build:
	go build -o ${BINARY}

release:
	goreleaser release --rm-dist --snapshot --skip-publish  --skip-sign

install: build
	mkdir -p ~/.terraform.d/plugins/${HOSTNAME}/${NAMESPACE}/${NAME}/${VERSION}/${OS_ARCH}
	mv ${BINARY} ~/.terraform.d/plugins/${HOSTNAME}/${NAMESPACE}/${NAME}/${VERSION}/${OS_ARCH}

test: 
	go test -i $(TEST) || exit 1                                                   
	echo $(TEST) | xargs -t -n4 go test $(TESTARGS) -timeout=30s -parallel=4                    

testacc:
	TF_ACC=1 go test $(TEST) -v $(TESTARGS) -timeout 120m

# Coverage of the pinned SDK's service groups by this provider. Both targets are
# offline: they read the SDK from the module cache and need no API token.
coverage-report:
	go run ./cmd/sdkcoverage report -format text

coverage-check:
	go run ./cmd/sdkcoverage check

# Validate generated scaffolding against the same offline gate the pipeline runs
# before opening a draft PR. KINDS is what the coverage report requested for the
# group — the gate fails any requested kind that was not delivered.
#   make scaffold-validate GROUP=PublicNetworks TYPE_NAME=latitudesh_public_network KINDS=resource,datasource
scaffold-validate:
	scripts/scaffold-validate.sh --group "$(GROUP)" --type-name "$(TYPE_NAME)" --kinds "$(KINDS)"

# Score the scaffolding agent on a known-good target. Manual/local only: it needs
# the authenticated claude CLI and spends tokens (~$8-12 a full run). Add --dry-run
# via ARGS to ablate and render the prompt without invoking the agent.
#   make eval-scaffold-agent CASE=elastic_ip
#   make eval-scaffold-agent CASE=elastic_ip ARGS=--dry-run
eval-scaffold-agent:
	scripts/eval-scaffold.sh --case "$(CASE)" $(ARGS) 