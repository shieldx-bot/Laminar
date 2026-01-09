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