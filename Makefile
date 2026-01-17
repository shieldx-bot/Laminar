IMG_MAIN ?= shieldxbot/my-nimbus-main:v0.0.8
IMG_DEMO ?= shieldxbot/my-demo-go:v0.0.8




.PHONY: r
r:
  go run ./cmd/compute/main.go


.PHONY: proto
proto:
	protoc \
		--proto_path=. \
		--go_out=. \
		--go-grpc_out=. \
		api/proto/laminar.proto


.PHONY: docker-build-banlancer
docker-build-balancer:
	docker build -t laminar-balancer:latest .