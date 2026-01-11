

File này được tối ưu hóa để bạn có thể đo lường được hiệu quả của việc **Gộp kết nối (Multiplexing)**, **Nén dữ liệu**, và **Xử lý bất đồng bộ**.

### File: `laminar.proto`

```protobuf
syntax = "proto3";

package laminar;

option go_package = "./pb";

// Service định nghĩa các phương thức giao tiếp chính
service LaminarGateway {
  // 1. Unary Call: Dùng để test request đơn lẻ, đo độ trễ cơ bản (Baseline P50/P99).
  rpc ProcessSingle (WorkRequest) returns (WorkResponse);

  // 2. Server Streaming: Test khả năng chịu tải khi Server trả về lượng dữ liệu lớn (Thundering Herd response).
  rpc SubscribeToEvents (EventSubscription) returns (stream WorkResponse);

  // 3. Bidirectional Streaming: Đây là cốt lõi để test "Connection Coalescing" và HTTP/2 Multiplexing.
  // Client có thể gửi hàng nghìn request qua 1 connection duy nhất mà không bị Head-of-Line Blocking.
  rpc PipelineProcess (stream WorkRequest) returns (stream WorkResponse);
}

message WorkRequest {
  // ID định danh request (dùng để trace logs)
  string request_id = 1;

  // Giả lập độ phức tạp tính toán (Worker Pool Test)
  // Gửi số này lên để server "ngủ" hoặc loop trong bao lâu -> Test Worker Pool bị starvation.
  int32 simulated_work_load_ms = 2;

  // Dữ liệu payload chính (có thể nén trước khi gửi để test "Pre-compressed")
  bytes payload = 3;

  // Mức độ ưu tiên (Test Adaptive Queueing: LIFO vs FIFO)
  // 0: Normal, 1: High Priority (xử lý ngay)
  int32 priority = 4;
  
  // Padding để test Cache Locality (dựa trên source 1468)
  // Thêm dữ liệu rác để object align với 64-byte cache line.
  bytes padding = 5; 
}

message WorkResponse {
  string request_id = 1;
  bool success = 2;
  
  // Thời gian xử lý thực tế tại Server (để tính toán Overhead của hàng đợi)
  int64 server_processing_time_ns = 3;

  // Dữ liệu trả về
  bytes result_payload = 4;
}

message EventSubscription {
  string topic = 1;
}
```

### Tại sao mẫu Proto này giúp bạn test hiệu quả?

Dựa trên các tài liệu đã phân tích, các trường (fields) trong file này được thiết kế để nhắm vào các kịch bản sau:

1.  **`rpc PipelineProcess (stream ...)`**:
    *   **Mục đích:** Test **Connection Coalescing**.
    *   **Lý do:** Tài liệu chỉ ra rằng gRPC sử dụng HTTP/2 multiplexing để gửi nhiều request trên một kết nối TCP duy nhất. Sử dụng hàm streaming này cho phép bạn bơm hàng nghìn `WorkRequest` vào một luồng mà không cần tạo mới kết nối, giúp bạn đo lường sự giảm tải của CPU khi không phải bắt tay TLS liên tục.

2.  **`simulated_work_load_ms`**:
    *   **Mục đích:** Test **Worker Pool** và **Tail Latency**.
    *   **Lý do:** Bạn có thể gửi các request với `simulated_work_load_ms` ngẫu nhiên (ví dụ: 1ms đến 100ms). Điều này mô phỏng các tác vụ không đồng đều, giúp bạn kiểm chứng xem chiến lược hàng đợi (FIFO/LIFO) của Worker Pool xử lý ra sao khi gặp các request "nặng" gây tắc nghẽn.

3.  **`payload` (bytes)**:
    *   **Mục đích:** Test **Pre-compression**.
    *   **Lý do:** Bạn có thể nén dữ liệu (dùng Gzip/Snappy) từ phía client trước khi gán vào trường `bytes` này. Điều này giúp kiểm chứng xem việc giảm kích thước gói tin có giảm độ trễ mạng như giả thuyết kiến trúc PMLC hay không.

4.  **`padding`**:
    *   **Mục đích:** Test **Cache Locality**.
    *   **Lý do:** Tài liệu gợi ý rằng việc chèn thêm dữ liệu (padding) để các object khớp với kích thước cache line (thường là 64 bytes) có thể tăng hiệu năng xử lý CPU bằng cách tránh hiện tượng "false sharing" giữa các luồng trong Worker Pool.

5.  **`priority`**:
    *   **Mục đích:** Test **Adaptive Queueing**.
    *   **Lý do:** Khi hệ thống quá tải (saturation), bạn có thể dùng trường này để yêu cầu Worker Pool ưu tiên xử lý các request mới (LIFO) hoặc các request quan trọng, từ đó đo lường mức giảm của độ trễ đuôi (P99).

### Hướng dẫn nhanh để sử dụng

1.  **Tạo mã nguồn:** Dùng `protoc` để sinh code cho Client (Go/Java) và Server (Go).
    ```bash
    protoc --go_out=. --go-grpc_out=. laminar.proto
    ```
2.  **Kịch bản Test:**
    *   **Client:** Tạo 1 connection duy nhất, gọi hàm `PipelineProcess`, và bắn 10.000 requests liên tục vào stream.
    *   **Server:** Cấu hình Worker Pool giới hạn (ví dụ: 50 workers).
    *   **Đo lường:** So sánh thời gian hoàn thành (Total Throughput) và thời gian phản hồi của 1% request chậm nhất (P99 Latency).