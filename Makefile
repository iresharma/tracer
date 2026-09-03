.PHONY: build test vet fmt tidy devcluster-up devcluster-down devcluster-reload devcluster-ui

build:
	go build ./...

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l .

tidy:
	go mod tidy

# Local minikube playground: tracer + self-driving log generators (an HTTP
# service mesh and a CI/CD pipeline simulator), for interactively exploring
# the UI during development. See scripts/devcluster-*.sh for details.
devcluster-up:
	./scripts/devcluster-up.sh

devcluster-down:
	./scripts/devcluster-down.sh

devcluster-reload:
	./scripts/devcluster-reload.sh

devcluster-ui:
	./scripts/devcluster-ui.sh
