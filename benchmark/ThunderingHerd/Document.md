
## 1. Kịch bản "Thundering Herd" (Bão Cache)
### Mô tả: Giả lập 10.000 users cùng lúc truy cập vào một bài viết Hot (cùng Query SQL) 
ngay khi Cache vừa hết hạn.
Mục tiêu kiểm tra: Cơ chế SingleFlight (Request Coalescing).
### Kết quả mong đợi:
Baseline (Cũ): 10.000 queries bắn xuống Database. -> DB CPU 100%, Response Time tăng vọt > 5s.
Laminar: Chỉ 1 query xuống Database. 9.999 request còn lại chờ và nhận kết quả tức thì.
 Database "im lìm" như chưa từng có cuộc tấn công.



 