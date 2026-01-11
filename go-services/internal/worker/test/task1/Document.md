1. Thử nghiệm "Khả năng Sống sót" (Survivability): FIFO vs. Adaptive LIFO
Đây là thử nghiệm quan trọng nhất để chứng minh giá trị của chiến lược hàng đợi thích ứng trong hệ thống của bạn.
• Mục tiêu: Chứng minh rằng khi hệ thống bị quá tải (Overload/Saturation), cơ chế LIFO giúp duy trì thông lượng hữu ích (Goodput) tốt hơn FIFO.
• Thiết lập (Setup):
    ◦ Mô hình đối chứng: Một Worker Pool sử dụng hàng đợi FIFO thuần túy.
    ◦ Mô hình Laminar: Worker Pool sử dụng Adaptive LIFO (chuyển sang LIFO khi hàng đợi > 80%).
    ◦ Tải giả lập: Tăng dần lượng request từ 1x lên đến 3x khả năng xử lý của server (Saturation Point).
• Chỉ số đo lường (Metrics):
    ◦ Goodput (Throughput hữu ích): Số lượng request được xử lý thành công trước khi client bị timeout.
    ◦ Error Rate: Tỷ lệ request bị lỗi hoặc timeout.
• Kết quả kỳ vọng: Dựa trên nghiên cứu, mô hình FIFO sẽ lãng phí tài nguyên để xử lý các request đã hết hạn (timeout), dẫn đến Goodput giảm mạnh. Ngược lại, LIFO sẽ ưu tiên request mới nhất, giữ cho Goodput ổn định ngay cả khi quá tải. Bạn có thể trích dẫn biểu đồ từ nguồn để so sánh với kết quả của mình.

============== Adaptive LIFO ========== 


--- START SCENARIO: 1. HOT CACHE (100% Hit) ---
Users (Concurrency): 50 | Reqs/User: 2000 | Total: 100000
Time Taken: 85.959575ms
RPS (Req/sec): 1163337.53
Success: 100000 | Errors: 0
Latency: Min=486ns | Avg=40.498µs | Max=1.320292ms
--------------------------------

--- START SCENARIO: 2. DISTRIBUTED (DB Load) ---
Users (Concurrency): 50 | Reqs/User: 100 | Total: 5000
Time Taken: 15.72847ms
RPS (Req/sec): 317894.87
Success: 5000 | Errors: 0
Latency: Min=4.386µs | Avg=110.791µs | Max=3.608418ms
--------------------------------
>> Thay đổi config MaxWorker thấp xuống để dễ kích hoạt Overload...

--- START SCENARIO: 3. OVERLOAD & FALLBACK ---
Users (Concurrency): 100 | Reqs/User: 50 | Total: 5000
Time Taken: 26.792026ms
RPS (Req/sec): 186622.69
Success: 5000 | Errors: 0
Latency: Min=4.41µs | Avg=374.223µs | Max=13.876813ms
--------------------------------

--- START SCENARIO: 4. REALISTIC PROD MIX (80/20 Rule) ---
Users: 50 | Reqs/User: 500 | Strategy: 80% Hot Keys / 20% Random Keys
Time Taken: 33.821739ms
RPS (Req/sec): 739169.56
Success: 25000 | Errors: 0
Latency: Min=1.56µs | Avg=63.412µs | Max=3.414202ms
--------------------------------