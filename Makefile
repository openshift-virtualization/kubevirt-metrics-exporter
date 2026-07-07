IMAGE ?= quay.io/openshift-virtualization/kubevirt-metrics-exporter
TAG ?= latest

.PHONY: generate build test image push deploy deploy-kubernetes deploy-manifest deploy-manifest-kubernetes undeploy undeploy-kubernetes setup-test-e2e test-e2e cleanup-test-e2e clean lint fmt

generate:
	go generate ./pkg/ebpf/...

build: generate
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
		go build -ldflags="-s -w" -o bin/kubevirt-metrics-exporter ./cmd/exporter/

test:
	go test -v -count=1 ./...

image:
	podman build -f Containerfile -t $(IMAGE):$(TAG) .

push: image
	podman push $(IMAGE):$(TAG)

deploy:
	cd deploy/openshift && kustomize edit set image quay.io/openshift-virtualization/kubevirt-metrics-exporter=$(IMAGE):$(TAG)
	oc apply -k deploy/openshift/

deploy-kubernetes:
	cd deploy/kubernetes && kustomize edit set image quay.io/openshift-virtualization/kubevirt-metrics-exporter=$(IMAGE):$(TAG)
	kubectl apply -k deploy/kubernetes/

deploy-manifest:
	@cd deploy/openshift && kustomize edit set image quay.io/openshift-virtualization/kubevirt-metrics-exporter=$(IMAGE):$(TAG)
	@kustomize build deploy/openshift/

deploy-manifest-kubernetes:
	@cd deploy/kubernetes && kustomize edit set image quay.io/openshift-virtualization/kubevirt-metrics-exporter=$(IMAGE):$(TAG)
	@kustomize build deploy/kubernetes/

undeploy:
	oc delete -k deploy/openshift/ --ignore-not-found

undeploy-kubernetes:
	kubectl delete -k deploy/kubernetes/ --ignore-not-found

setup-test-e2e:
	hack/e2e-setup.sh

test-e2e:
	go test ./test/e2e/... -v -ginkgo.v -tags=e2e -timeout 15m

cleanup-test-e2e:
	hack/e2e-teardown.sh

clean:
	rm -rf bin/
	rm -f pkg/ebpf/*.o

lint:
	golangci-lint run ./...

fmt:
	gofmt -s -w .
