# Kỷ Yếu Kỹ Thuật Hệ Thống Phân Tán: 12 Kịch Bản Lỗi Cốt Lõi Của FlashPass

> **Tác giả:** Đội ngũ Kỹ sư Hệ thống Phân tán FlashPass  
> **Chủ đề:** Tính Nhất Quán (Consistency), Bất Biến (Invariants) & Khả Năng Kháng Lỗi Trong Flash Sale Đồng Thời Cao  
> **Bộ Kiểm Thử Tự Động:** `backend/internal/api/edge_case_*_test.go` (Vượt qua 100% 12/12 Modular Chaos & Integration Tests)  

🌐 **Ngôn ngữ / Language:** [English](./DISTRIBUTED_EDGE_CASES.md) • **Tiếng Việt**

---

## 📌 Các Bất Biến Hệ Thống Cốt Lõi (System Invariants)

Trong các hệ thống phân tán chịu tải siêu lớn (> 20.000 RPS), tính chất ACID truyền thống không thể áp dụng đồng thời xuyên suốt qua các ranh giới mạng riêng biệt (Redis $\leftrightarrow$ RabbitMQ $\leftrightarrow$ PostgreSQL) mà không làm suy sụp thông lượng.

FlashPass thiết lập 3 bất biến toán học bất khả xâm phạm:
1. **Bất Biến Bán Âm (Overselling Invariant):**
   $$\text{Orders}_{\text{PAID}} + \text{Orders}_{\text{PENDING\_PAYMENT}} \le \text{TotalTickets}$$
   Dưới bất kỳ sự cố mạng hoặc sập nút nào, tổng số đơn hàng đang hoạt động không bao giờ được vượt quá số lượng vé ban đầu.
2. **Bất Biến Idempotent (Idempotency Invariant):**
   $$\forall k, \quad f(k) = f(f(k))$$
   Mọi yêu cầu gửi cùng một `idempotency_key` đều chỉ tạo ra đúng 1 hiệu ứng phụ duy nhất, loại bỏ việc trừ vé hoặc trừ tiền trùng lặp.
3. **Bất Biến Từ Chối Nhanh (Fail-Fast Liveness Invariant):**
   Các yêu cầu khi đã hết vé phải bị từ chối ngay trong RAM với thời gian phản hồi:
   $$\text{Latency}_{\text{SoldOut}} < 2\text{ms}$$

---

## Kịch Bản 1: Redis Master Sập Đột Ngột & Khoảng Hở Replication Bất Đồng Bộ

### 1. Cơ Chế Lỗi Trong Môi Trường Phân Tán
Redis sử dụng cơ chế nhân bản bất đồng bộ (Asynchronous Replication) giữa Master và Replica để tối ưu hóa hiệu năng ghi. Khi Master nhận lệnh trừ vé qua script Lua, nó phản hồi HTTP `202 Accepted` cho Khách hàng A và đưa lệnh trừ vé vào bộ đệm replication stream. Nếu phần cứng Master bị sập đột ngột trước khi các gói tin kịp bay qua mạng sang Replica:
- Redis Sentinel phát hiện Master mất liên lạc và phát lệnh thăng cấp Replica thành Master mới (`REPLICAOF NO ONE`).
- Tuy nhiên, Replica này chưa nhận được lệnh trừ vé của Khách hàng A, nên số lượng vé trên RAM của nó vẫn còn nguyên.
- Khách hàng B kết nối vào Master mới và mua thành công đúng chiếc vé đó.
- **Hậu quả:** Tỷ lệ bán âm thực tế trong sản xuất vượt ngưỡng $> 100\%$.

### 2. Giải Pháp Kỹ Thuật Của FlashPass
1. **Tùy Chọn Đồng Bộ `WAIT 1`:** Với các phân khúc vé VIP giá trị cao, động cơ booking hỗ trợ tùy chọn đợi ít nhất 1 Replica xác nhận ghi trước khi trả về 202:
   ```go
   rdb.Wait(ctx, 1, 500*time.Millisecond)
   ```
2. **Chốt Chặn Thép Tại PostgreSQL:** Cơ sở dữ liệu duy trì một ràng buộc kiểm tra số lượng đơn:
   $$\sum \text{Orders}_{\text{PAID}} \le \text{TotalTickets}$$
   Nếu xảy ra sự cố failover khiến đơn hàng thứ $N+1$ lọt vào hàng đợi, câu lệnh kiểm tra `SELECT ... FOR UPDATE` trong Worker sẽ phát hiện kho vé đã đầy và hủy đơn, bảo vệ tuyệt đối không xuất vé thừa ra đời thực.
3. **Kiểm chứng:** `TestEdgeCase1_RedisFailover` tạm dừng container Master bằng lệnh `docker pause`, kích hoạt Sentinel đảo Master và chứng minh không có đơn thứ 2 nào được lưu vào DB.

---

## Kịch Bản 2: Worker Hủy Vé Đua Với Webhook Ngân Hàng Đến Muộn (Auto-Refund)

### 1. Cơ Chế Lỗi Trong Môi Trường Phân Tán
Khách hàng A giữ vé với thời hạn thanh toán 10 phút:
- **T = 10m00.000s:** Hết hạn 10 phút. Worker hẹn giờ kích hoạt: Đánh dấu đơn A là `EXPIRED` và hoàn trả vé vào RAM: `INCRBY stock 1`.
- **T = 10m00.100s:** Khách hàng B vào mua chiếc vé vừa được giải phóng và thanh toán thành công.
- **T = 10m01.000s:** Webhook từ cổng thanh toán của Khách A mới truyền tới máy chủ (do khách hàng nhập mã OTP ngân hàng trễ hoặc nghẽn mạng ngân hàng).
- **Hậu quả:** Nếu máy chủ thanh toán nhắm mắt cập nhật đơn A thành `PAID`, cả Khách A và Khách B đều cầm trong tay hóa đơn đã trả tiền cho cùng một số ghế.

### 2. Giải Pháp Kỹ Thuật Của FlashPass
FlashPass xây dựng **Máy Trạng Thái Lạc Quan (Optimistic State Machine)** kết hợp **Pipeline Tự Động Hoàn Tiền (Auto-Refund)**:
1. Endpoint `POST /api/v1/orders/:id/pay` được bọc trong khóa phân tán.
2. Kiểm tra trạng thái đơn: Nếu đơn đã chuyển sang `EXPIRED` hoặc đã quá hạn:
   - Hệ thống **từ chối giao vé**. Khách B vẫn là chủ sở hữu hợp pháp duy nhất của vé.
   - Ghi nhận yêu cầu hoàn tiền vào bảng `refund_orders` với trạng thái `REFUND_PENDING`.
   - Đẩy sự kiện `order.refund` vào hàng đợi RabbitMQ `flashpass.order.refund.queue`.
   - Phản hồi HTTP `200 OK` cho cổng thanh toán với thông điệp:
     ```json
     { "status": "REFUND_PENDING", "message": "Order expired. Ticket was released. Automatic refund initiated." }
     ```
3. Worker hoàn tiền tiêu thụ hàng đợi, gọi API hoàn tiền của ngân hàng (hỗ trợ lũy thừa lùi Exponential Backoff khi mạng chập chờn) và cập nhật trạng thái đơn thành `REFUNDED`.
4. **Kiểm chứng:** `TestEdgeCase2_TimeoutRaceCondition`.

---

## Kịch Bản 3: Lỗi Ghi Hai Nơi (Ghost Inventory) & Cơ Chế Tự Phục Hồi

### 1. Cơ Chế Lỗi Trong Môi Trường Phân Tán
Trong mô hình ghi 2 bước bất đồng bộ:
- Bước 1: Trừ kho trên RAM Redis từ 100 xuống 99.
- Bước 2: Bắn tin nhắn `order.create` sang RabbitMQ.
Nếu máy chủ API bị sập (hết bộ nhớ OOM, đứt cáp mạng, panic) ngay giữa Bước 1 và Bước 2: Vé trên Redis đã bị trừ, nhưng RabbitMQ không có tin nhắn $\to$ PostgreSQL không bao giờ nhận được đơn hàng.
- **Hậu quả (Tồn Kho Ảo - Ghost Inventory):**
  $$\text{Redis Stock} + \text{Đơn DB} < \text{Tổng Vé}$$
  Vé bị bốc hơi bí ẩn, sự kiện báo "Hết Vé" dù thực tế vẫn còn vé chưa ai mua.

### 2. Giải Pháp Kỹ Thuật Của FlashPass
Hệ thống triển khai dịch vụ kiểm toán kho **Stock Auditor (`ReconcileService`)**:
1. Tính toán lượng tồn kho lý thuyết chuẩn xác:
   $$\text{ExpectedStock} = \text{TotalTickets} - (\text{Orders}_{\text{PENDING}} + \text{Orders}_{\text{PAID}})$$
2. So sánh với tồn kho thực tế trên Redis:
   $$\Delta = \text{ExpectedStock} - \text{RedisStock}$$
3. **Cơ Chế Kẹp 2 Chiều (Two-Way Clamping):**
   - Nếu $\Delta > 0$ (Phát hiện tồn kho ảo): Tự động cộng bù $\Delta$ vào Redis stock, giải cứu các vé bị kẹt.
   - Nếu $\Delta < 0$ (Thừa kho): Giảm trừ Redis stock xuống để chặn đứng nguy cơ bán âm.
4. **Cơ Chế Bắt Đáy Hit-Zero:** Khi kho chạm 0, kích hoạt khóa `SETNX 5s` kiểm toán tức thì để xóa bỏ hoàn toàn tình trạng báo hết vé giả trong những giây cuối cùng.
5. **Dọn Khóa Người Dùng Mồ Côi:** Xóa khóa `user_booked` nếu phát hiện người dùng bị trừ lượt nhưng DB không có đơn, cho phép họ đặt lại bình thường.
6. **Kiểm chứng:** `TestEdgeCase3_DualWriteGhostInventory`.

---

## Kịch Bản 4: Phân Vùng Shard Trong Redis Cluster & Hash Tag Co-Location

### 1. Cơ Chế Lỗi Trong Môi Trường Phân Tán
Trong Redis Cluster, dữ liệu được chia làm 16.384 hash slot:
$$\text{Slot} = \text{CRC16}(\text{key}) \pmod{16384}$$
Khi script Lua cần thao tác đồng thời trên 2 khóa: Khóa tồn kho (`flashpass:stock:evt-100`) và Khóa người dùng (`flashpass:user_booked:evt-100:user-5`), Redis sẽ băm 2 chuỗi này ra 2 slot CRC16 hoàn toàn khác nhau.
Nếu 2 slot này thuộc 2 node máy chủ vật lý khác nhau trong cụm, Redis sẽ lập tức ném lỗi chí mạng:
```
CROSSSLOT Keys in request don't hash to the same slot
```

### 2. Giải Pháp Kỹ Thuật Của FlashPass
FlashPass chuẩn hóa toàn bộ các khóa sử dụng cú pháp **Hash Tag `{...}`**:
- Khóa tồn kho: `flashpass:{evt-100}:stock`
- Khóa người dùng: `flashpass:{evt-100}:user_booked:{user-5}`
- Khóa kiểm toán: `flashpass:{evt-100}:audit_lock`
- Khóa thông tin sự kiện: `flashpass:{evt-100}:meta`

Do Redis Cluster chỉ thực hiện hàm băm CRC16 trên phần văn bản nằm bên trong cặp dấu ngoặc nhọn `{...}`, toàn bộ các khóa của sự kiện `evt-100` đều có cùng giá trị hash `CRC16("evt-100") \pmod{16384}`, bảo đảm 100% các khóa luôn nằm trên cùng một shard duy nhất, triệt tiêu hoàn toàn lỗi `CROSSSLOT`.
- **Kiểm chứng:** `TestEdgeCase4_CrossSlotHashTag` kiểm tra sự đồng nhất về Slot ID của tất cả các khóa liên quan.

---

## Kịch Bản 5: Thư Rác (Poison Pill) & Hàng Đợi Thư Chết (DLQ)

### 1. Cơ Chế Lỗi Trong Môi Trường Phân Tán
Một tin nhắn bị lỗi cú pháp JSON, chứa ký tự null không hợp lệ, hoặc vi phạm khóa ngoại UUID lạ được gửi vào hàng đợi `flashpass.order.create.queue`.
Khi Worker xử lý thất bại:
- Nếu Worker gọi `BasicNack(requeue=true)`, RabbitMQ sẽ lập tức nhả lại tin nhắn đó về đầu hàng đợi.
- Worker lại nhận lại chính tin nhắn lỗi đó và lại crash tiếp. Quá trình lặp vô tận tạo thành vòng xoáy tử thần (100% CPU), khiến hàng ngàn đơn hàng hợp lệ phía sau bị phong tỏa.

### 2. Giải Pháp Kỹ Thuật Của FlashPass
1. **Kiến Trúc Dead Letter Exchange (DLX):**
   - Khai báo Exchange thư chết: `flashpass.dlx`.
   - Hàng đợi lưu thư chết: `flashpass.order.dlq`.
   - Cấu hình hàng đợi chính kèm cờ: `x-dead-letter-exchange: flashpass.dlx`.
2. **Quy Tắc Cách Ly Sau 2 Lần Thử:**
   - Worker đếm số lần thất bại qua header của tin nhắn.
   - Đến lần thử thứ 2 không thành công, Worker gọi `BasicReject(requeue=false)`, buộc RabbitMQ đẩy tin nhắn sang hàng đợi DLQ để đội ngũ kỹ sư điều tra sau.
3. **Tự Động Hoàn Lại Kho Tồn:** Khi một tin nhắn bị cách ly vào DLQ, FlashPass tự động gọi `INCRBY` bù lại 1 vé vào Redis stock, không để thất thoát kho vé.
4. **Kiểm chứng:** `TestEdgeCase5_RabbitMQDeadLetterQueue`.

---

## Kịch Bản 6: Bão Truy Cập Cache (Cache Stampede) & SingleFlight Coalescing

### 1. Cơ Chế Lỗi Trong Môi Trường Phân Tán
Vào đúng thời điểm mở bán, 10.000 người dùng mỗi giây cùng gọi API `GET /api/v1/events/:id`.
Khi khóa cache `flashpass:{id}:meta` trên Redis vừa hết hạn (hoặc chưa kịp nạp):
- Toàn bộ 10.000 requests cùng nhận thấy cache miss.
- Toàn bộ 10.000 luồng đồng loạt gửi truy vấn `SELECT * FROM events` vào PostgreSQL.
- **Hậu quả:** Connection Pool của database (thường giới hạn 50 kết nối) bị cạn kiệt ngay lập tức, CPU database vọt lên 100%, kéo sập toàn bộ các dịch vụ phụ thuộc.

### 2. Giải Pháp Kỹ Thuật Của FlashPass
FlashPass tích hợp thư viện **`singleflight.Group`** của Go:
```go
val, err, _ := s.singleFlightGroup.Do(cacheKey, func() (interface{}, error) {
    // Chỉ có ĐÚNG 1 goroutine duy nhất được phép chọc xuống Database
    event, err := s.eventRepo.GetByID(ctx, eventID)
    if err != nil {
        return nil, err
    }
    // Ghi lại kết quả vào cache Redis
    _ = s.stockCache.SetEventMeta(ctx, eventIDStr, event, 10*time.Minute)
    return event, nil
})
```
- Dù có 10.000 requests ập đến cùng một thời điểm, `singleflight` sẽ gom chúng lại và chỉ cho phép **đúng 1 truy vấn** gửi xuống PostgreSQL.
- 9.999 requests còn lại sẽ chờ kết quả từ truy vấn đó và cùng nhận dữ liệu mà không gây áp lực lên database.
- **Kiểm chứng:** `TestEdgeCase6_CacheStampedeSingleFlight` bắn 100 truy vấn đồng thời và khẳng định số lần query DB chỉ đúng bằng 1.

---

## Kịch Bản 7: Webhook Thanh Toán Trùng Lặp Cùng Lúc & Khóa Phân Tán

### 1. Cơ Chế Lỗi Trong Môi Trường Phân Tán
Cổng thanh toán kích hoạt cơ chế retry tự động khi mạng phản hồi chậm:
- Webhook 1 tới Máy chủ A tại thời điểm $T = 0\text{ms}$.
- Webhook 2 tới Máy chủ B tại thời điểm $T = 2\text{ms}$.
- Cả 2 máy chủ cùng đọc cơ sở dữ liệu thấy đơn ở trạng thái `PENDING_PAYMENT`, cùng chạy logic xác nhận thanh toán, dẫn tới ghi nhận doanh thu hai lần hoặc sinh ra 2 mã hoàn tiền trùng lặp cho cùng một đơn hàng.

### 2. Giải Pháp Kỹ Thuật Của FlashPass
1. **Khóa Phân Tán Watchdog (`WatchdogLock`):**
   ```
   SET flashpass:lock:pay:{order_id} {owner_uuid} NX EX 5
   ```
   Chỉ luồng nào giành được khóa mới được tiến hành xử lý thanh toán. Luồng thua sẽ đợi 100ms để giao dịch của luồng thắng kịp commit vào database.
2. **Cơ Chế Bắt Trượt Idempotent:**
   Câu lệnh SQL cập nhật trạng thái có điều kiện:
   ```sql
   UPDATE orders SET status = 'PAID' WHERE id = $1 AND status = 'PENDING_PAYMENT'
   ```
   Nếu số dòng bị tác động bằng 0 (`RowsAffected == 0`), mã Go sẽ đọc lại trạng thái đơn. Nếu thấy đơn đã là `PAID`, hệ thống lập tức trả về HTTP `200 OK` ("Order already paid") thay vì báo lỗi 500, dập tắt ngay lập tức các đợt bắn phá webhook trùng lặp từ ngân hàng.
3. **Kiểm chứng:** `TestEdgeCase7_DoublePaymentWebhookConcurrency` bắn 5 webhook song song và chứng minh đơn chỉ cập nhật đúng 1 lần duy nhất.

---

## Kịch Bản 8: Đồng Hồ Phân Tán Bị Lệch Pha (NTP Clock Drift) & Cửa Sổ Ân Hạn

### 1. Cơ Chế Lỗi Trong Môi Trường Phân Tán
Trên hạ tầng đám mây phân tán giữa các cụm máy ảo (VM) hoặc Kubernetes nodes:
- Đồng hồ của máy chủ Worker chạy nhanh hơn đồng hồ của máy chủ API 3 giây do trôi dạt xung nhịp tinh thể hoặc độ trễ đồng bộ NTP.
- Khách hàng bấm thanh toán tại thời điểm $T = 9\text{m}58\text{s}$ (vẫn còn 2 giây trước khi hết hạn 10 phút).
- Nhưng máy chủ Worker (với đồng hồ chạy trước tới $T = 10\text{m}01\text{s}$) đã vội vã đánh dấu đơn hàng là hết hạn và thu hồi vé, dẫn tới tranh chấp pháp lý và khiếu nại gay gắt từ người mua.

### 2. Giải Pháp Kỹ Thuật Của FlashPass
1. **Cửa Sổ Ân Hạn ($\Delta = 5\text{s}$):**
   Mã xử lý thanh toán của FlashPass cho phép một khoảng đệm ân hạn 5 giây:
   $$\text{ThanhToanHopLe} \iff \text{HienTai} \le \text{ExpiresAt} + 5\text{s}$$
2. **Đồng Hồ Chuẩn Từ PostgreSQL:**
   Worker khi thực thi lệnh hủy đơn không được tin vào đồng hồ của chính tiến trình Go, mà bắt buộc phải dựa vào đồng hồ chuẩn của máy chủ cơ sở dữ liệu:
   ```sql
   UPDATE orders SET status = 'EXPIRED' 
   WHERE id = $1 AND status = 'PENDING_PAYMENT' AND expires_at <= NOW()
   ```
3. **Kiểm chứng:** `TestEdgeCase8_ClockDriftGraceWindow`.

---

## Kịch Bản 9: Bão Bot Spam Vé & Giới Hạn Tốc Độ Cửa Sổ Trượt (Sliding Window)

### 1. Cơ Chế Lỗi Trong Môi Trường Phân Tán
Các phe ôm vé (scalpers) sử dụng mạng bot tự động bắn hàng ngàn request mỗi giây vào API đặt vé nhằm vét sạch vé và làm tê liệt đường truyền của người dùng thật.

### 2. Giải Pháp Kỹ Thuật Của FlashPass
FlashPass tích hợp thuật toán **Sliding-Window Log** nguyên tử bằng Redis Lua:
```lua
-- KEYS[1]: flashpass:{identifier}:ratelimit
-- ARGV[1]: now_ms, ARGV[2]: window_ms, ARGV[3]: max_limit, ARGV[4]: member_id
local key = KEYS[1]
local clear_before = tonumber(ARGV[1]) - tonumber(ARGV[2])
redis.call('ZREMRANGEBYSCORE', key, '-inf', clear_before)
local current_requests = redis.call('ZCARD', key)
if current_requests < tonumber(ARGV[3]) then
    redis.call('ZADD', key, ARGV[1], ARGV[4])
    redis.call('PEXPIRE', key, ARGV[2])
    return 1 -- Hợp lệ
else
    return 0 -- Bị chặn
end
```
- Khắc phục hoàn toàn lỗ hổng dồn lưu lượng tại ranh giới của thuật toán Fixed-Window truyền thống.
- Chặn đứng các đợt tấn công ngay tại tầng HTTP Middleware, trả về mã HTTP `429 Too Many Requests` và header `Retry-After: 2`, bảo vệ an toàn tuyệt đối cho tầng Redis Lua, RabbitMQ và Database phía sau.
- **Kiểm chứng:** `TestEdgeCase9_SlidingWindowRateLimiter`.

---

## Kịch Bản 10: Đảo Ngược Thứ Tự Tin Nhắn (Out-of-Order) & Cắm Cờ Bia Mộ (Tombstone)

### 1. Cơ Chế Lỗi Trong Môi Trường Phân Tán
Trong môi trường bất đồng bộ:
- **Tiểu ca 10.1 (Thanh toán đến trước khi tạo đơn):** Webhook thanh toán tới quá nhanh trong khi Worker tạo đơn chưa kịp lưu bản ghi vào PostgreSQL. Nếu API thanh toán trả về `404 Not Found`, giao dịch thanh toán sẽ bị hủy nhầm.
- **Tiểu ca 10.2 (Hủy đơn trước khi tạo đơn):** Khách hàng ấn Hủy đơn ngay sau khi đặt; tin nhắn hủy đơn tới database trước tin nhắn tạo đơn. Nếu không có biện pháp phòng ngừa, tin nhắn tạo đơn tới sau sẽ "hồi sinh" đơn hàng đã bị hủy về lại trạng thái `PENDING_PAYMENT`.

### 2. Giải Pháp Kỹ Thuật Của FlashPass
1. **Debounce Settle Wait Cho Thanh Toán Sớm:** Nếu `GetByID` chưa tìm thấy đơn, API thanh toán tạm dừng 100ms và thử lại lần 2 để chờ giao dịch tạo đơn kịp commit.
2. **Kỹ Thuật Cắm Cờ Bia Mộ (Tombstone Pattern):**
   Khi nhận lệnh hủy cho một đơn chưa có trong DB, hệ thống tạo một bản ghi Bia Mộ với trạng thái `CANCELLED`.
   Hàm tạo đơn `orderRepo.Create` sử dụng cú pháp `ON CONFLICT` bảo toàn trạng thái:
   ```sql
   INSERT INTO orders (id, event_id, user_id, idempotency_key, status, expires_at)
   VALUES ($1, $2, $3, $4, $5, $6)
   ON CONFLICT (id) DO UPDATE 
   SET status = CASE 
       WHEN orders.status = 'CANCELLED' THEN 'CANCELLED'
       ELSE EXCLUDED.status 
   END;
   ```
   Nếu đơn đã bị cắm cờ `CANCELLED`, lệnh tạo đơn tuyệt đối không được phép ghi đè lại thành `PENDING_PAYMENT`.
3. **Kiểm chứng:** `TestEdgeCase10_OutOfOrderMessageProcessing`.

---

## Kịch Bản 11: Hiệu Ứng Domino Sập Database (Cascading Failure) & Cầu Dao Ngắt Mạch

### 1. Cơ Chế Lỗi Trong Môi Trường Phân Tán
Khi PostgreSQL bị nghẽn đĩa hoặc tranh chấp khóa bản ghi:
- Các goroutine gọi `pgxpool.Acquire()` bị kẹt lại.
- Connection Pool nhanh chóng cạn kiệt (50/50 kết nối bị chiếm dụng).
- Hàng ngàn kết nối HTTP mới đổ vào bị giữ lại trong RAM, dung lượng bộ nhớ tăng vọt cho đến khi hệ điều hành giết tiến trình (OOM Killer), làm sập cả các endpoint khỏe mạnh đang đọc từ Redis.

### 2. Giải Pháp Kỹ Thuật Của FlashPass
FlashPass trang bị **Cầu Dao Ngắt Mạch (Circuit Breaker)** luồng an toàn (`backend/internal/resilience/circuit_breaker.go`):
- **CLOSED (Bình thường):** Theo dõi số lần lỗi database liên tiếp.
- **OPEN (Ngắt mạch):** Khi ghi nhận 3 lần lỗi liên tiếp, cầu dao lập tức ngắt. Toàn bộ các request tiếp theo truy cập vào DB sẽ bị từ chối ngay trong **0 mili-giây** với mã HTTP `503 Service Unavailable`, không gửi thêm bất kỳ tải nào xuống DB.
- **HALF-OPEN (Thăm dò):** Sau 1.5 giây hồi phục, cầu dao hé mở cho đúng **1 request chim mồi (canary probe)** đi qua. Nếu chim mồi thành công $\to$ Cầu dao đóng lại trạng thái `CLOSED`. Nếu thất bại $\to$ Cầu dao tiếp tục ngắt mạch `OPEN` thêm một chu kỳ nữa.
- **Kiểm chứng:** `TestEdgeCase11_DatabaseCircuitBreaker`.

---

## Kịch Bản 12: Xung Đột Khóa Phân Tán (Split-Brain) & Chó Săn Gia Hạn (Watchdog)

### 1. Cơ Chế Lỗi Trong Môi Trường Phân Tán
Khóa phân tán thông thường dùng thời gian sống cố định (`SET lock uuid NX EX 5`).
Nếu một tác vụ thanh toán gặp mạng lag hoặc Garbage Collection tạm dừng mất 7 giây:
- Tại thời điểm $T = 5\text{s}$, Redis tự động xóa khóa vì hết hạn TTL.
- Luồng B tới thấy khóa trống, liền chiếm lấy khóa và thực thi logic thanh toán.
- Lúc này cả Luồng A và Luồng B cùng chạy đồng thời trong vùng tranh chấp (Chia cắt não - Split-Brain).
- Tại thời điểm $T = 7\text{s}$, Luồng A làm xong gọi `DEL lock`, vô tình xóa luôn khóa hợp pháp mà Luồng B đang nắm giữ!

### 2. Giải Pháp Kỹ Thuật Của FlashPass
FlashPass hiện thực hóa mẫu thiết kế **Redisson Watchdog Pattern** trong Go:
1. **Mã Sở Hữu Duy Nhất (Owner Token):** Mỗi lần lấy khóa sinh ra một chuỗi UUID riêng biệt.
2. **Goroutine Chó Săn Gia Hạn:** Một goroutine chạy ngầm tự động gửi lệnh Lua gia hạn khóa sau mỗi $\text{TTL}/3$:
   ```lua
   if redis.call('GET', KEYS[1]) == ARGV[1] then
       return redis.call('EXPIRE', KEYS[1], ARGV[2])
   else
       return 0
   end
   ```
3. **Giải Phóng Khóa Xác Minh Chính Chủ:** Chỉ luồng nào cầm đúng Token sở hữu mới được phép xóa khóa:
   ```lua
   if redis.call('GET', KEYS[1]) == ARGV[1] then
       return redis.call('DEL', KEYS[1])
   else
       return 0
   end
   ```
- Luồng B không bao giờ có thể chen vào khi Luồng A chưa xong việc, và các luồng trễ mạng không bao giờ có thể xóa nhầm khóa của nhau.
- **Kiểm chứng:** `TestEdgeCase12_DistributedLockWatchdog`.
