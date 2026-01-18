# Baseline: Centralized Round-Robin

Mục tiêu: baseline để so sánh với Laminar theo mô hình **centralized synchronous request dispatch** (1 load balancer + pool backend).

Thư mục:
- `backend/`: Express + Postgres, nhận `QuerySQL` và query DB
- `load-balancer/`: Express, giữ **danh sách backend** và chọn theo **round-robin** để forward request
- `client/`: Vite+React, đo **tail latency** + **backend distribution** giống client Laminar (p10..p99, sent/expected/received/lost)

## Yêu cầu
- Node.js **>= 18** (để dùng `fetch` built-in trong load balancer)
- Một Postgres DB có bảng `users` (để query `SELECT * FROM users WHERE id = ...`)

## 1) Run backend (có thể chạy nhiều instance)

Cài deps:

```bash
cd baseline/round-robin/backend
npm install
```

Set env (xem `.env.example`):
- `DATABASE_URL` (bắt buộc)
- `PORT`, `INSTANCE_ID` (tuỳ chọn)

Chạy 3 instance ví dụ:

```bash
# terminal 1
DATABASE_URL="..." PORT=8081 INSTANCE_ID=b1 npm start

# terminal 2
DATABASE_URL="..." PORT=8082 INSTANCE_ID=b2 npm start

# terminal 3
DATABASE_URL="..." PORT=8083 INSTANCE_ID=b3 npm start
```

Healthcheck:
- `GET http://localhost:8081/api/health`

## 2) Run load balancer (round-robin)

```bash
cd baseline/round-robin/load-balancer
npm install
BACKENDS="http://localhost:8081,http://localhost:8082,http://localhost:8083" PORT=8090 npm start
```

Healthcheck:
- `GET http://localhost:8090/api/health`

## 3) Run client (metrics giống Laminar)

```bash
cd baseline/round-robin/client
npm install
VITE_LB_URL="http://localhost:8090" VITE_TOTAL_REQUESTS=1000 \
npm run dev
```

Mở browser theo URL Vite in ra, bấm **Test Requests**.

Metrics được in ra trong DevTools Console:
- Tail latency stats: `avg/min/p10..p99/max` + `sent/expected/received/lost`
- Backend distribution: `attempts` và `primary` (với round-robin thì giống nhau vì 1 request = 1 attempt)

## Notes
- Load balancer trả thêm field `ChosenBackend` để client thống kê distribution.
- Danh sách backend nằm ở **load balancer** qua env `BACKENDS` và `GET /api/health`.
- Timeout phía client: `VITE_TIMEOUT_MS` (mặc định 20000ms)
- Timeout forward phía load balancer: `FORWARD_TIMEOUT_MS` (mặc định 20000ms)
