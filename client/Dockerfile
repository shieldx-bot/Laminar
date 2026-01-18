# --- TẦNG 1: "NHÀ MÁY" (BUILDER) ---
# Dùng image node 20 bản slim (nhỏ gọn) để làm "nhà máy"
FROM node:20-bookworm-slim AS builder
WORKDIR /app

# Copy file package trước để tận dụng cache
COPY package.json package-lock.json ./

# Cài đặt TẤT CẢ dependencies (bao gồm dev)
RUN --mount=type=cache,id=npm-cache,target=/root/.npm \
    npm ci

# Copy toàn bộ code nguồn và build
COPY . .
RUN npm run build
# -> Kết quả của tầng này là thư mục /app/dist

# --- TẦNG 2: "SẢN PHẨM" (PRODUCTION) ---
FROM nginx:1.27-alpine

# Xóa file cấu hình nginx mặc định
RUN rm /etc/nginx/conf.d/default.conf

# THÊM DÒNG NÀY:
# Copy file cấu hình mới của bạn vào
COPY default.conf /etc/nginx/conf.d/default.conf

# Sao chép SẢN PHẨM (thư mục dist) từ tầng "builder"
COPY --from=builder /app/dist /usr/share/nginx/html

EXPOSE 5173
CMD ["nginx", "-g", "daemon off;"]