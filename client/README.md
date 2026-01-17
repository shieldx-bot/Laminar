
npm install --save-dev grpc-tools protoc-gen-grpc-web



cd ~/Documents/Laminar/client
npx grpc_tools_node_protoc \
  --proto_path=src/proto \
  --js_out=import_style=commonjs,binary:src/generated \
  --grpc-web_out=import_style=typescript,mode=grpcwebtext:src/generated \
  src/proto/*.proto
