IMAGE ?= ghcr.io/openshift-virtualization/kubevirt-storage-latency-exporter
TAG ?= latest

.PHONY: generate build test image push deploy deploy-kubernetes undeploy undeploy-kubernetes clean lint fmt

generate:
	go generate ./pkg/ebpf/...

build: generate
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
		go build -ldflags="-s -w" -o bin/kubevirt-storage-latency-exporter ./cmd/exporter/

test:
	go test -v -count=1 ./...

image:
	podman build -f Containerfile -t $(IMAGE):$(TAG) .

push: image
	podman push $(IMAGE):$(TAG)

deploy:
	cd deploy/openshift && kustomize edit set image ghcr.io/openshift-virtualization/kubevirt-storage-latency-exporter=$(IMAGE):$(TAG)
	oc apply -k deploy/openshift/

deploy-kubernetes:
	cd deploy/kubernetes && kustomize edit set image ghcr.io/openshift-virtualization/kubevirt-storage-latency-exporter=$(IMAGE):$(TAG)
	kubectl apply -k deploy/kubernetes/

undeploy:
	oc delete -k deploy/openshift/ --ignore-not-found

undeploy-kubernetes:
	kubectl delete -k deploy/kubernetes/ --ignore-not-found

clean:
	rm -rf bin/
	rm -f pkg/ebpf/*.o

lint:
	golangci-lint run ./...

fmt:
	gofmt -s -w .
