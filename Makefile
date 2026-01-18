IMG_CLIENT ?= shieldxbot/laminar-client:v0.0.1
 



 


.PHONY: proto
proto:
	protoc \
		--proto_path=. \
		--go_out=. \
		--go-grpc_out=. \
		api/proto/laminar.proto


.PHONY: docker-build-client 
docker-build-client:
	docker build -t $(IMG_CLIENT)  -f ./client/Dockerfile ./client
 	docker push $(IMG_CLIENT)
 