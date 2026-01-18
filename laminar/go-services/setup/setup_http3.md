OK, log bạn đưa **rất chuẩn**, mình phân tích đúng lỗi và **setup lại từ đầu – theo thứ tự KHÔNG gãy**.
Bạn đang gặp **2 lỗi nền tảng**, không liên quan mạng hay HTTP/3 cả.

---

# 🚨 VẤN ĐỀ GỐC (ROOT CAUSE)

## ❌ Lỗi 1: Không có quyền root

```text
E: Unable to acquire the dpkg frontend lock, are you root?
```

👉 Bạn đang chạy bằng **user thường**, nhưng:

* `apt`
* `make install`
* ghi `/usr/local`
  → **bắt buộc root**

---

## ❌ Lỗi 2: Thiếu tool build cơ bản

```text
Can't locate FindBin.pm
-bash: make: command not found
```

👉 Do:

* `build-essential` **chưa cài**
* `perl` **chưa đầy đủ**
* `make` **chưa tồn tại**

---

# ✅ GIẢI PHÁP: SETUP LẠI TỪ ĐẦU (ĐÚNG CHUẨN)

## 🔥 BƯỚC 0 – VÀO QUYỀN ROOT (QUAN TRỌNG NHẤT)

Chạy **MỘT trong hai**:

### Cách A (khuyên dùng)

```bash
sudo -i
```

### Cách B

```bash
su -
```

👉 Sau đó prompt phải là:

```
root@instance-xxxx:~#
```

---

## 🔥 BƯỚC 1 – CÀI TOÀN BỘ TOOL CẦN THIẾT (MỘT LẦN DUY NHẤT)

```bash
apt update
apt install -y \
build-essential \
perl \
cmake \
git \
curl \
wget \
libpcre3 \
libpcre3-dev \
zlib1g \
zlib1g-dev
```

### Kiểm tra:

```bash
make -v
perl -v
gcc --version
```

👉 Nếu **3 lệnh này chạy được** → OK

---

## 🔥 BƯỚC 2 – BUILD OPENSSL 3 (CÁCH AN TOÀN)

```bash
cd /usr/local/src
wget https://github.com/openssl/openssl/releases/download/openssl-3.0.13/openssl-3.0.13.tar.gz
tar xzf openssl-3.0.13.tar.gz
cd openssl-3.0.13
```

### Configure & build

```bash
./Configure linux-x86_64
make -j$(nproc)
make install
```

### Verify

```bash
/usr/local/bin/openssl version
```

👉 Phải thấy:

```
OpenSSL 3.0.13
```

---

## 🔥 BƯỚC 3 – BUILD NGINX CÓ HTTP/3

```bash
cd /usr/local/src
wget https://nginx.org/download/nginx-1.25.4.tar.gz
tar xzf nginx-1.25.4.tar.gz
cd nginx-1.25.4
```

### Configure đúng chuẩn HTTP/3

```bash
./configure \
--with-http_ssl_module \
--with-http_v2_module \
--with-http_v3_module \
--with-openssl=/usr/local/src/openssl-3.0.13
```

### Build

```bash
make -j$(nproc)
make install
```

---

## 🔥 BƯỚC 4 – KIỂM TRA NGINX CÓ HTTP/3 CHƯA

```bash
/usr/local/nginx/sbin/nginx -V
```

Phải thấy:

```
--with-http_v3_module
```

---

## 🔥 BƯỚC 5 – MỞ UDP 443 (QUIC)

```bash
ufw allow 443/tcp
ufw allow 443/udp

---

# ✅ CÀI TOOL TEST HTTP/2 + HTTP/3 (LAPTOP/CLIENT)

**HTTP/2 (h2load):**

```bash
sudo apt update
sudo apt install -y nghttp2-client
h2load --version
```

> Lưu ý: lệnh đúng là `h2load` (không phải `htload`). `htload` sẽ gợi ý package khác (không liên quan).

**HTTP/3 (QUIC):** hiện tại Debian/Ubuntu thường **không có** một binary tên `h3load` trong apt.

Thay vào đó, bộ `ngtcp2` có sẵn client HTTP/3 mẫu là `bsslclient` (build từ source). Tool này đủ để:
- Xác nhận server có mở UDP/443 và HTTP/3 chạy OK.
- Bắn nhiều request (nhiều stream) trên 1 kết nối QUIC với `-n/--nstreams`.

## Build nghttp3 + ngtcp2 + BoringSSL (khuyến nghị)

```bash
sudo apt update
sudo apt install -y \
  git build-essential autoconf automake libtool pkg-config \
  cmake ninja-build \
  libssl-dev libnghttp2-dev libev-dev \
  libcunit1-dev

mkdir -p ~/src && cd ~/src

git clone https://github.com/ngtcp2/nghttp3
cd nghttp3
git submodule update --init --recursive
autoreconf -fi
./configure --prefix=$HOME/.local
make -j$(nproc)
make install

cd ~/src
git clone https://github.com/ngtcp2/ngtcp2
cd ngtcp2
git submodule update --init --recursive
autoreconf -fi
export PKG_CONFIG_PATH="$HOME/.local/lib/pkgconfig:$PKG_CONFIG_PATH"

# NOTE: h3load cần QUIC-TLS. OpenSSL hệ thống thường KHÔNG có QUIC API.
# Cách ổn định nhất là build với BoringSSL.

cd ~/src
git clone https://boringssl.googlesource.com/boringssl
cd boringssl
cmake -B build -G Ninja
ninja -C build

cd ~/src/ngtcp2
CXX=/usr/bin/g++-13 \
  BORINGSSL_CFLAGS="-I$HOME/src/boringssl/include" \
  BORINGSSL_LIBS="-L$HOME/src/boringssl/build -lssl -lcrypto" \
  ./configure --prefix=$HOME/.local --with-libnghttp3 --with-boringssl
make -j$(nproc)
make install

## Chạy test HTTP/3 nhanh (smoke)

Binary nằm ở `~/src/ngtcp2/examples/bsslclient` (ngtcp2 không install nó vào PATH).

Ví dụ gọi GET `/fast` qua QUIC:

```bash
cd ~/src/ngtcp2/examples
./bsslclient --timeout=5s -n 1 shieldx.dev 443 https://shieldx.dev/fast
```

Nếu bạn thấy retry/PTO nhiều và kết thúc bằng `ERR_IDLE_CLOSE` thì gần như chắc chắn UDP/443 đang bị chặn (VPS firewall/security-group) hoặc Nginx chưa bật HTTP/3.
```
```

---

 

---

# ✅ NGINX CONFIG: BẬT HTTP/2 + HTTP/3 (QUIC) CHO DOMAIN

Giả sử backend HTTP của bạn đang chạy ở VPS tại `127.0.0.1:8082` (ví dụ chạy Go proxy), và bạn có cert Let's Encrypt tại:
- `/etc/letsencrypt/live/your-domain.com/fullchain.pem`
- `/etc/letsencrypt/live/your-domain.com/privkey.pem`

Tạo file config (ví dụ):

`/etc/nginx/conf.d/your-domain.com.conf`

```nginx
# Redirect HTTP -> HTTPS
server {
  listen 80;
  server_name your-domain.com;
  return 301 https://$host$request_uri;
}

server {
  # HTTP/2 over TCP
  listen 443 ssl http2;
  # HTTP/3 over QUIC (UDP)
  listen 443 quic reuseport;

  server_name your-domain.com;

  ssl_certificate     /etc/letsencrypt/live/your-domain.com/fullchain.pem;
  ssl_certificate_key /etc/letsencrypt/live/your-domain.com/privkey.pem;

  # QUIC/HTTP3 recommended
  ssl_protocols TLSv1.3;
  ssl_prefer_server_ciphers off;

  # Advertise HTTP/3 to clients
  add_header alt-svc 'h3=":443"; ma=86400';
  add_header x-quic 'h3';

  location / {
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_pass http://127.0.0.1:8082;
  }
}
```

Kiểm tra và reload:

```bash
nginx -t
systemctl reload nginx
```

Verify nhanh từ laptop:

```bash
curl -I --http2 https://shieldx.dev/fast
curl -I --http3 https://your-domain.com/fast
```

