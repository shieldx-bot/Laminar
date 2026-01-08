syntax = "proto3";

package laminar;

option go_package = "./pb";

// Service định nghĩa các phương thức giao tiếp chính
service LaminarGateway {
  // 1. Unary Call Dùng để test request đơn lẻ, đo độ trễ cơ bản Baseline P50/P99.
  rpc ProcessSingle WorkRequest returns WorkResponse;

  // 2. Server Streaming Test khả năng chịu tải khi Server trả về lượng dữ liệu lớn Thundering Herd response.
  rpc SubscribeToEvents EventSubscription returns stream WorkResponse;

  // 3. Bidirectional Streaming Đây là cốt lõi để test "Connection Coalescing" và HTTP/2 Multiplexing.
  // Client có thể gửi hàng nghìn request qua 1 connection duy nhất mà không bị Head-of-Line Blocking.
  rpc PipelineProcess stream WorkRequest returns stream WorkResponse;
}

message WorkRequest {
  // ID định danh request dùng để trace logs
  string request_id = 1;

  // Giả lập độ phức tạp tính toán Worker Pool Test
  // Gửi số này lên để server "ngủ" hoặc loop trong bao lâu -> Test Worker Pool bị starvation.
  int32 simulated_work_load_ms = 2;

  // Dữ liệu payload chính có thể nén trước khi gửi để test "Pre-compressed"
  bytes payload = 3;

  // Mức độ ưu tiên Test Adaptive Queueing LIFO vs FIFO
  // 0 Normal, 1 High Priority xử lý ngay
  int32 priority = 4;
  
  // Padding để test Cache Locality dựa trên source 1468
  // Thêm dữ liệu rác để object align với 64-byte cache line.
  bytes padding = 5; 
}

message WorkResponse {
  string request_id = 1;
  bool success = 2;
  
  // Thời gian xử lý thực tế tại Server để tính toán Overhead của hàng đợi
  int64 server_processing_time_ns = 3;

  // Dữ liệu trả về
  bytes result_payload = 4;
}

message EventSubscription {
  string topic = 1;
}