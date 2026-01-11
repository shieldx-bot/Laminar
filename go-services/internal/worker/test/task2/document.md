2. Thử nghiệm Hiệu quả Tài nguyên: Unbounded Concurrency vs. Worker Pool
Thử nghiệm này chứng minh khả năng quản lý bộ nhớ và ổn định hệ thống, dựa trên bài học từ Shopify.
• Mục tiêu: Chứng minh Worker Pool ngăn chặn lỗi "Out of Memory" (OOM) và giảm áp lực lên Garbage Collector (GC) so với việc tạo Goroutine vô hạn.
• Thiết lập:
    ◦ Mô hình đối chứng: Tạo một Goroutine mới cho mỗi request đến (Unbounded).
    ◦ Mô hình Laminar: Sử dụng số lượng Worker cố định (ví dụ: bằng số core CPU).
    ◦ Tải giả lập: Gửi một lượng lớn request đồng thời (ví dụ: 50.000 concurrent requests).
• Chỉ số đo lường:
    ◦ Memory Footprint (RAM): Lượng RAM tiêu thụ đỉnh.
    ◦ GC Pause Time: Thời gian dừng của bộ thu gom rác.
• Kết quả kỳ vọng: Dữ liệu từ Shopify cho thấy mô hình Worker Pool có thể giảm 85% lượng RAM tiêu thụ và giảm 90% thời gian GC pause so với mô hình Unbounded. Bạn cần số liệu thực tế từ hệ thống của mình để đối chiếu với con số này.