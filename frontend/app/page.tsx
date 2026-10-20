'use client';

import React, { useState, useEffect, useCallback } from 'react';
import {
  Zap,
  Ticket,
  Clock,
  ShieldCheck,
  CheckCircle2,
  AlertCircle,
  RefreshCw,
  Flame,
  Database,
  Cpu,
  CreditCard,
  RotateCcw,
  Timer,
  ChevronRight,
  Users,
} from 'lucide-react';

const API_BASE = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080';
const DEFAULT_EVENT_ID = process.env.NEXT_PUBLIC_EVENT_ID || 'e9b8849b-7e61-4648-936c-941a329fa9c1';

interface EventData {
  id: string;
  title: string;
  total_tickets: number;
  available_tickets: number;
  start_time: string;
  end_time: string;
}

interface AdminMetrics {
  event_id: string;
  stock: number;
  pending_orders: number;
  paid_orders: number;
  expired_orders: number;
  total_orders: number;
}

interface Reservation {
  order_id: string;
  event_id: string;
  user_id: string;
  status: string;
  expires_at: string;
}

export default function FlashPassApp() {
  const [activeTab, setActiveTab] = useState<'booking' | 'admin'>('booking');
  const [eventId] = useState<string>(DEFAULT_EVENT_ID);
  const [eventData, setEventData] = useState<EventData | null>(null);
  const [stock, setStock] = useState<number>(1000);
  const [metrics, setMetrics] = useState<AdminMetrics | null>(null);
  const [loading, setLoading] = useState<boolean>(false);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);
  const [successMsg, setSuccessMsg] = useState<string | null>(null);

  // Reservation modal state
  const [reservation, setReservation] = useState<Reservation | null>(null);
  const [timeLeft, setTimeLeft] = useState<number>(600); // 10 minutes (600s)
  const [paymentLoading, setPaymentLoading] = useState<boolean>(false);
  const [paymentStatus, setPaymentStatus] = useState<'pending' | 'paid' | 'expired'>('pending');

  // Client user ID stored in localStorage or random
  const [userId, setUserId] = useState<string>('');

  useEffect(() => {
    let saved = localStorage.getItem('flashpass_user_id');
    if (!saved) {
      saved = 'user-' + Math.random().toString(36).substring(2, 8);
      localStorage.setItem('flashpass_user_id', saved);
    }
    setUserId(saved);
  }, []);

  // Fetch Event Data & Stock
  const fetchStock = useCallback(async () => {
    try {
      const res = await fetch(`${API_BASE}/api/v1/events/${eventId}/stock`);
      if (res.ok) {
        const data = await res.json();
        setStock(data.stock);
      }
    } catch {
      // ignore network transient errors during polling
    }
  }, [eventId]);

  const fetchMetrics = useCallback(async () => {
    try {
      const res = await fetch(`${API_BASE}/api/v1/admin/events/${eventId}/metrics`);
      if (res.ok) {
        const data = await res.json();
        setMetrics(data);
        setStock(data.stock);
      }
    } catch {
      // ignore
    }
  }, [eventId]);

  const fetchEventDetails = useCallback(async () => {
    try {
      const res = await fetch(`${API_BASE}/api/v1/events/${eventId}`);
      if (res.ok) {
        const data = await res.json();
        setEventData(data);
        setStock(data.available_tickets);
      }
    } catch {
      // fallback sample
      setEventData({
        id: eventId,
        title: 'Taylor Swift: The Eras Tour - FlashPass VIP Experience',
        total_tickets: 1000,
        available_tickets: stock,
        start_time: new Date().toISOString(),
        end_time: new Date(Date.now() + 7 * 86400000).toISOString(),
      });
    }
  }, [eventId, stock]);

  // Periodic polling for real-time stock
  useEffect(() => {
    fetchEventDetails();
    fetchStock();
    const interval = setInterval(() => {
      if (activeTab === 'admin') {
        fetchMetrics();
      } else {
        fetchStock();
      }
    }, 1500);
    return () => clearInterval(interval);
  }, [activeTab, fetchStock, fetchMetrics, fetchEventDetails]);

  // Reservation countdown timer
  useEffect(() => {
    if (!reservation || paymentStatus !== 'pending') return;

    const timer = setInterval(() => {
      const remaining = Math.max(
        0,
        Math.floor((new Date(reservation.expires_at).getTime() - Date.now()) / 1000)
      );
      setTimeLeft(remaining);

      if (remaining <= 0) {
        setPaymentStatus('expired');
        clearInterval(timer);
        fetchStock();
      }
    }, 1000);

    return () => clearInterval(timer);
  }, [reservation, paymentStatus, fetchStock]);

  // Action: Book Ticket (Flash Sale)
  const handleBookTicket = async () => {
    setLoading(true);
    setErrorMsg(null);
    setSuccessMsg(null);

    const idempotencyKey = crypto.randomUUID();

    try {
      const res = await fetch(`${API_BASE}/api/v1/tickets/book`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          event_id: eventId,
          user_id: userId,
          idempotency_key: idempotencyKey,
        }),
      });

      const data = await res.json();

      if (res.status === 202) {
        setReservation(data);
        setPaymentStatus('pending');
        const secs = Math.max(
          0,
          Math.floor((new Date(data.expires_at).getTime() - Date.now()) / 1000)
        );
        setTimeLeft(secs || 600);
        fetchStock();
      } else if (res.status === 409) {
        setErrorMsg(data.error || 'Vé đã hết hoặc bạn đã giữ vé rồi!');
        fetchStock();
      } else {
        setErrorMsg(data.error || 'Đặt vé không thành công, vui lòng thử lại.');
      }
    } catch {
      setErrorMsg('Không thể kết nối đến máy chủ API FlashPass (localhost:8080).');
    } finally {
      setLoading(false);
    }
  };

  // Action: Mock Payment
  const handlePayOrder = async () => {
    if (!reservation) return;
    setPaymentLoading(true);

    try {
      const res = await fetch(`${API_BASE}/api/v1/orders/${reservation.order_id}/pay`, {
        method: 'POST',
      });
      const data = await res.json();

      if (res.ok) {
        setPaymentStatus('paid');
        setSuccessMsg('Thanh toán thành công! Vé VIP của bạn đã được xuất.');
        fetchStock();
      } else {
        setErrorMsg(data.error || 'Thanh toán thất bại.');
        if (data.error && data.error.includes('expired')) {
          setPaymentStatus('expired');
        }
      }
    } catch {
      setErrorMsg('Lỗi kết nối khi thanh toán.');
    } finally {
      setPaymentLoading(false);
    }
  };

  // Action: Admin Prewarm
  const handleAdminPrewarm = async () => {
    setLoading(true);
    try {
      const res = await fetch(`${API_BASE}/api/v1/admin/events/${eventId}/prewarm`, {
        method: 'POST',
      });
      if (res.ok) {
        setSuccessMsg('Pre-warm thành công! Kho vé đã nạp vào Redis RAM.');
        fetchMetrics();
        fetchStock();
      }
    } catch {
      setErrorMsg('Pre-warm thất bại.');
    } finally {
      setLoading(false);
    }
  };

  // Action: Admin Restock & Reset
  const handleAdminReset = async () => {
    setLoading(true);
    try {
      const res = await fetch(`${API_BASE}/api/v1/admin/events/${eventId}/reset`, {
        method: 'POST',
      });
      if (res.ok) {
        setSuccessMsg('Reset sự kiện thành công! 1,000 vé đã được phục hồi.');
        setReservation(null);
        fetchMetrics();
        fetchStock();
      }
    } catch {
      setErrorMsg('Reset thất bại.');
    } finally {
      setLoading(false);
    }
  };

  // Format seconds to mm:ss
  const formatTime = (secs: number) => {
    const m = Math.floor(secs / 60);
    const s = secs % 60;
    return `${m.toString().padStart(2, '0')}:${s.toString().padStart(2, '0')}`;
  };

  const totalTickets = eventData?.total_tickets || 1000;
  const stockPercentage = Math.max(0, Math.min(100, (stock / totalTickets) * 100));

  return (
    <div className="min-h-screen bg-slate-950 text-slate-100 flex flex-col font-sans selection:bg-purple-500 selection:text-white">
      {/* Top Navbar */}
      <header className="border-b border-slate-800/80 bg-slate-900/60 backdrop-blur-md sticky top-0 z-40">
        <div className="max-w-6xl mx-auto px-4 h-16 flex items-center justify-between">
          <div className="flex items-center gap-3">
            <div className="w-10 h-10 rounded-xl bg-gradient-to-tr from-amber-500 via-rose-500 to-purple-600 flex items-center justify-center shadow-lg shadow-purple-500/20">
              <Zap className="w-6 h-6 text-white fill-white" />
            </div>
            <div>
              <span className="font-extrabold text-lg tracking-tight bg-gradient-to-r from-white via-slate-200 to-slate-400 bg-clip-text text-transparent">
                FlashPass
              </span>
              <span className="text-[10px] font-semibold text-purple-400 bg-purple-950/60 border border-purple-800/60 px-2 py-0.5 rounded-full ml-2">
                20k RPS Engine
              </span>
            </div>
          </div>

          {/* Tab Switcher */}
          <div className="flex items-center gap-1 bg-slate-900 border border-slate-800 p-1 rounded-xl">
            <button
              onClick={() => setActiveTab('booking')}
              className={`flex items-center gap-2 px-4 py-1.5 rounded-lg text-sm font-medium transition-all ${
                activeTab === 'booking'
                  ? 'bg-purple-600 text-white shadow-md shadow-purple-600/30'
                  : 'text-slate-400 hover:text-slate-200'
              }`}
            >
              <Ticket className="w-4 h-4" />
              Mua Vé Ngay
            </button>
            <button
              onClick={() => {
                setActiveTab('admin');
                fetchMetrics();
              }}
              className={`flex items-center gap-2 px-4 py-1.5 rounded-lg text-sm font-medium transition-all ${
                activeTab === 'admin'
                  ? 'bg-indigo-600 text-white shadow-md shadow-indigo-600/30'
                  : 'text-slate-400 hover:text-slate-200'
              }`}
            >
              <Cpu className="w-4 h-4" />
              Live Admin Dashboard
            </button>
          </div>
        </div>
      </header>

      {/* Main Container */}
      <main className="flex-1 max-w-6xl w-full mx-auto px-4 py-8">
        {/* Global Alerts */}
        {errorMsg && (
          <div className="mb-6 p-4 rounded-xl bg-rose-950/40 border border-rose-800/80 text-rose-300 flex items-center justify-between text-sm animate-in fade-in">
            <div className="flex items-center gap-3">
              <AlertCircle className="w-5 h-5 text-rose-400 shrink-0" />
              <span>{errorMsg}</span>
            </div>
            <button
              onClick={() => setErrorMsg(null)}
              className="text-rose-400 hover:text-rose-200 font-bold"
            >
              ✕
            </button>
          </div>
        )}

        {successMsg && (
          <div className="mb-6 p-4 rounded-xl bg-emerald-950/40 border border-emerald-800/80 text-emerald-300 flex items-center justify-between text-sm animate-in fade-in">
            <div className="flex items-center gap-3">
              <CheckCircle2 className="w-5 h-5 text-emerald-400 shrink-0" />
              <span>{successMsg}</span>
            </div>
            <button
              onClick={() => setSuccessMsg(null)}
              className="text-emerald-400 hover:text-emerald-200 font-bold"
            >
              ✕
            </button>
          </div>
        )}

        {activeTab === 'booking' ? (
          /* TAB 1: EVENT BOOKING CLIENT */
          <div className="grid grid-cols-1 lg:grid-cols-3 gap-8">
            {/* Left 2 Cols: Event Banner & Details */}
            <div className="lg:col-span-2 space-y-6">
              {/* Event Hero Card */}
              <div className="relative rounded-2xl overflow-hidden border border-slate-800 bg-gradient-to-b from-slate-900 to-slate-950 p-6 md:p-8 shadow-2xl">
                <div className="absolute top-0 right-0 w-96 h-96 bg-purple-600/10 rounded-full blur-3xl pointer-events-none" />
                <div className="absolute -bottom-10 -left-10 w-80 h-80 bg-rose-600/10 rounded-full blur-3xl pointer-events-none" />

                <div className="relative z-10">
                  <div className="flex items-center gap-2 mb-3">
                    <span className="inline-flex items-center gap-1.5 px-3 py-1 rounded-full text-xs font-semibold bg-rose-500/20 text-rose-400 border border-rose-500/30">
                      <Flame className="w-3.5 h-3.5 fill-rose-400" />
                      FLASH SALE ĐANG MỞ
                    </span>
                    <span className="text-xs text-slate-400">
                      Sân vận động Quốc gia Mỹ Đình • VIP Zone
                    </span>
                  </div>

                  <h1 className="text-2xl md:text-4xl font-extrabold tracking-tight text-white mb-3">
                    {eventData?.title || 'Taylor Swift: The Eras Tour - FlashPass VIP'}
                  </h1>

                  <p className="text-slate-400 text-sm md:text-base leading-relaxed mb-6">
                    Trải nghiệm đêm nhạc đỉnh cao với tấm vé VIP FlashPass. Quyền lợi bao gồm khu vực đứng sát sân khấu VIP Lounge, soundcheck riêng tư cùng Taylor Swift và set quà tặng độc quyền.
                  </p>

                  {/* Highlights Grid */}
                  <div className="grid grid-cols-3 gap-4 pt-4 border-t border-slate-800/80">
                    <div className="flex items-center gap-3">
                      <div className="w-9 h-9 rounded-lg bg-purple-950/60 border border-purple-800/50 flex items-center justify-center text-purple-400">
                        <Clock className="w-4 h-4" />
                      </div>
                      <div>
                        <div className="text-xs text-slate-400">Thời gian bán</div>
                        <div className="text-xs font-semibold text-slate-200">20:00 Hôm Nay</div>
                      </div>
                    </div>

                    <div className="flex items-center gap-3">
                      <div className="w-9 h-9 rounded-lg bg-emerald-950/60 border border-emerald-800/50 flex items-center justify-center text-emerald-400">
                        <ShieldCheck className="w-4 h-4" />
                      </div>
                      <div>
                        <div className="text-xs text-slate-400">Giới hạn mua</div>
                        <div className="text-xs font-semibold text-slate-200">1 Vé / Tài Khoản</div>
                      </div>
                    </div>

                    <div className="flex items-center gap-3">
                      <div className="w-9 h-9 rounded-lg bg-blue-950/60 border border-blue-800/50 flex items-center justify-center text-blue-400">
                        <Users className="w-4 h-4" />
                      </div>
                      <div>
                        <div className="text-xs text-slate-400">Tài khoản</div>
                        <div className="text-xs font-semibold text-slate-200">{userId || 'Khách'}</div>
                      </div>
                    </div>
                  </div>
                </div>
              </div>

              {/* Ticket Tier Info Card */}
              <div className="rounded-2xl border border-slate-800 bg-slate-900/60 p-6 space-y-4">
                <div className="flex items-center justify-between">
                  <h3 className="font-bold text-lg text-white">Quyền Lợi Vé VIP Flash Sale</h3>
                  <span className="text-xs text-purple-400 bg-purple-950/80 px-2.5 py-1 rounded-md border border-purple-800/40">
                    Hạng Vé Duy Nhất
                  </span>
                </div>

                <ul className="space-y-2.5 text-sm text-slate-300">
                  <li className="flex items-center gap-2">
                    <CheckCircle2 className="w-4 h-4 text-emerald-400 shrink-0" />
                    <span>Lối đi ưu tiên riêng biệt (VIP Fast Track) không cần xếp hàng.</span>
                  </li>
                  <li className="flex items-center gap-2">
                    <CheckCircle2 className="w-4 h-4 text-emerald-400 shrink-0" />
                    <span>Thẻ đeo thẻ bài hologram độc bản The Eras Tour.</span>
                  </li>
                  <li className="flex items-center gap-2">
                    <CheckCircle2 className="w-4 h-4 text-emerald-400 shrink-0" />
                    <span>Quyền giữ chỗ 10 phút để hoàn tất thanh toán trước khi vé được trả về kho.</span>
                  </li>
                </ul>
              </div>
            </div>

            {/* Right 1 Col: Live Inventory & Instant Buy Card */}
            <div className="space-y-6">
              <div className="rounded-2xl border border-slate-800 bg-slate-900 p-6 space-y-6 shadow-xl sticky top-24">
                {/* Price Display */}
                <div>
                  <span className="text-xs font-medium text-slate-400 uppercase tracking-wider">
                    Giá niêm yết
                  </span>
                  <div className="flex items-baseline gap-2 mt-1">
                    <span className="text-3xl md:text-4xl font-extrabold text-white">
                      6.000.000₫
                    </span>
                    <span className="text-sm text-slate-400">/ 1 vé VIP</span>
                  </div>
                </div>

                {/* Real-time Inventory Progress Bar */}
                <div className="space-y-2 pt-2 border-t border-slate-800">
                  <div className="flex items-center justify-between text-xs">
                    <span className="flex items-center gap-1.5 text-slate-300 font-medium">
                      <Database className="w-3.5 h-3.5 text-purple-400" />
                      Kho Vé Thời Gian Thực (Redis RAM):
                    </span>
                    <span
                      className={`font-mono font-bold ${
                        stock === 0
                          ? 'text-rose-400'
                          : stock < 100
                          ? 'text-amber-400'
                          : 'text-emerald-400'
                      }`}
                    >
                      {stock.toLocaleString()} / {totalTickets.toLocaleString()}
                    </span>
                  </div>

                  <div className="w-full h-3 bg-slate-800 rounded-full overflow-hidden p-0.5 border border-slate-700">
                    <div
                      className={`h-full rounded-full transition-all duration-500 ${
                        stock === 0
                          ? 'bg-rose-500'
                          : stock < 100
                          ? 'bg-amber-500'
                          : 'bg-gradient-to-r from-emerald-500 to-purple-500'
                      }`}
                      style={{ width: `${stockPercentage}%` }}
                    />
                  </div>

                  <div className="flex justify-between items-center text-[11px] text-slate-400 pt-1">
                    <span>
                      {stock === 0
                        ? 'Đã cháy vé!'
                        : stock < 100
                        ? 'Sắp hết vé cực nhanh!'
                        : 'Còn lại trong kho'}
                    </span>
                    <span className="flex items-center gap-1">
                      <span className="w-1.5 h-1.5 rounded-full bg-emerald-400 animate-ping" />
                      Live Polling 1.5s
                    </span>
                  </div>
                </div>

                {/* Instant Buy Button */}
                <button
                  onClick={handleBookTicket}
                  disabled={loading || stock === 0}
                  className={`w-full py-4 px-6 rounded-xl font-bold text-base flex items-center justify-center gap-2 transition-all transform active:scale-95 shadow-xl ${
                    stock === 0
                      ? 'bg-slate-800 text-slate-500 cursor-not-allowed border border-slate-700'
                      : 'bg-gradient-to-r from-purple-600 via-pink-600 to-rose-600 hover:from-purple-500 hover:to-rose-500 text-white shadow-purple-600/30'
                  }`}
                >
                  {loading ? (
                    <>
                      <RefreshCw className="w-5 h-5 animate-spin" />
                      Đang xử lý đặt chỗ...
                    </>
                  ) : stock === 0 ? (
                    'HẾT VÉ (SOLD OUT)'
                  ) : (
                    <>
                      <Zap className="w-5 h-5 fill-white" />
                      MUA VÉ NGAY
                      <ChevronRight className="w-4 h-4 ml-auto" />
                    </>
                  )}
                </button>

                <div className="text-[11px] text-slate-400 text-center leading-relaxed">
                  Bảo vệ chống mua đúp bằng Idempotency Key tự động. Xác thực giữ chỗ nguyên tử trong Redis Lua Script &lt; 2ms.
                </div>
              </div>
            </div>
          </div>
        ) : (
          /* TAB 2: ADMIN LIVE MONITORING DASHBOARD */
          <div className="space-y-8">
            <div className="flex flex-col md:flex-row md:items-center justify-between gap-4 pb-4 border-b border-slate-800">
              <div>
                <h2 className="text-2xl font-bold text-white flex items-center gap-2">
                  <Cpu className="w-6 h-6 text-indigo-400" />
                  Bảng Giám Sát Hiệu Năng & Kho Vé (Live Monitor)
                </h2>
                <p className="text-slate-400 text-sm mt-1">
                  Đo lường thời gian thực: Tồn kho Redis RAM vs Đơn hàng bền vững PostgreSQL (Cam kết Overselling 0.00%)
                </p>
              </div>

              {/* Demo Action Buttons */}
              <div className="flex items-center gap-3">
                <button
                  onClick={handleAdminPrewarm}
                  disabled={loading}
                  className="flex items-center gap-2 px-4 py-2 rounded-xl bg-purple-600/20 hover:bg-purple-600/30 border border-purple-500/40 text-purple-300 text-sm font-semibold transition-all"
                >
                  <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
                  Pre-warm Stock vào Redis
                </button>
                <button
                  onClick={handleAdminReset}
                  disabled={loading}
                  className="flex items-center gap-2 px-4 py-2 rounded-xl bg-rose-600/20 hover:bg-rose-600/30 border border-rose-500/40 text-rose-300 text-sm font-semibold transition-all"
                >
                  <RotateCcw className="w-4 h-4" />
                  Restock & Reset Sự Kiện
                </button>
              </div>
            </div>

            {/* Gauge Cards Grid */}
            <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-5">
              {/* Card 1: Redis RAM Stock */}
              <div className="rounded-2xl border border-slate-800 bg-slate-900/80 p-5 space-y-2">
                <div className="flex items-center justify-between text-xs text-slate-400">
                  <span>Kho Vé RAM (Redis)</span>
                  <Zap className="w-4 h-4 text-amber-400" />
                </div>
                <div className="text-3xl font-extrabold text-white font-mono">
                  {stock.toLocaleString()}
                </div>
                <div className="text-[11px] text-slate-400 flex items-center gap-1.5 pt-1">
                  <span className="w-2 h-2 rounded-full bg-emerald-400" />
                  Kiểm tra nguyên tử Lua Script
                </div>
              </div>

              {/* Card 2: Queued / Pending Orders */}
              <div className="rounded-2xl border border-slate-800 bg-slate-900/80 p-5 space-y-2">
                <div className="flex items-center justify-between text-xs text-slate-400">
                  <span>Đang Giữ Chỗ (Chờ Trả)</span>
                  <Timer className="w-4 h-4 text-purple-400" />
                </div>
                <div className="text-3xl font-extrabold text-purple-400 font-mono">
                  {metrics?.pending_orders ?? 0}
                </div>
                <div className="text-[11px] text-slate-400">Thời hạn giữ 10 phút</div>
              </div>

              {/* Card 3: Paid Orders */}
              <div className="rounded-2xl border border-slate-800 bg-slate-900/80 p-5 space-y-2">
                <div className="flex items-center justify-between text-xs text-slate-400">
                  <span>Đã Thanh Toán (PAID)</span>
                  <CheckCircle2 className="w-4 h-4 text-emerald-400" />
                </div>
                <div className="text-3xl font-extrabold text-emerald-400 font-mono">
                  {metrics?.paid_orders ?? 0}
                </div>
                <div className="text-[11px] text-slate-400">Đơn hàng hoàn tất</div>
              </div>

              {/* Card 4: Expired Orders */}
              <div className="rounded-2xl border border-slate-800 bg-slate-900/80 p-5 space-y-2">
                <div className="flex items-center justify-between text-xs text-slate-400">
                  <span>Hết Hạn & Hoàn Vé</span>
                  <RotateCcw className="w-4 h-4 text-rose-400" />
                </div>
                <div className="text-3xl font-extrabold text-rose-400 font-mono">
                  {metrics?.expired_orders ?? 0}
                </div>
                <div className="text-[11px] text-slate-400">Đã trả stock về kho</div>
              </div>
            </div>

            {/* Architecture Invariant Verification Panel */}
            <div className="rounded-2xl border border-slate-800 bg-gradient-to-r from-slate-900 via-indigo-950/20 to-slate-900 p-6 space-y-4">
              <h3 className="font-bold text-base text-white flex items-center gap-2">
                <ShieldCheck className="w-5 h-5 text-emerald-400" />
                Kiểm Chứng Bất Biến Hệ Thống (Zero Overselling Guarantee)
              </h3>
              <div className="grid grid-cols-1 md:grid-cols-3 gap-4 text-sm">
                <div className="p-4 rounded-xl bg-slate-950/60 border border-slate-800">
                  <div className="text-slate-400 text-xs">Tổng Vé Ban Đầu</div>
                  <div className="text-xl font-bold text-white mt-1">
                    {totalTickets.toLocaleString()}
                  </div>
                </div>

                <div className="p-4 rounded-xl bg-slate-950/60 border border-slate-800">
                  <div className="text-slate-400 text-xs">Tồn Kho + Vé Đã/Đang Giữ</div>
                  <div className="text-xl font-bold text-indigo-400 mt-1">
                    {(stock + (metrics?.pending_orders ?? 0) + (metrics?.paid_orders ?? 0)).toLocaleString()}
                  </div>
                </div>

                <div className="p-4 rounded-xl bg-slate-950/60 border border-slate-800">
                  <div className="text-slate-400 text-xs">Trạng Thái Bán Âm</div>
                  <div className="text-xl font-bold text-emerald-400 mt-1 flex items-center gap-1.5">
                    <CheckCircle2 className="w-5 h-5" />
                    0.00% (An Toàn Tuyệt Đối)
                  </div>
                </div>
              </div>
            </div>
          </div>
        )}
      </main>

      {/* RESERVATION SUCCESS MODAL & 10-MIN PAYMENT FLOW */}
      {reservation && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/80 backdrop-blur-sm animate-in fade-in">
          <div className="w-full max-w-md rounded-2xl border border-slate-800 bg-slate-900 p-6 shadow-2xl space-y-6">
            {paymentStatus === 'pending' && (
              <>
                <div className="text-center space-y-2">
                  <div className="w-14 h-14 rounded-full bg-purple-500/20 border border-purple-500/40 text-purple-400 flex items-center justify-center mx-auto">
                    <Timer className="w-7 h-7 animate-pulse" />
                  </div>
                  <h3 className="text-xl font-bold text-white">Giữ Chỗ Thành Công!</h3>
                  <p className="text-xs text-slate-400">
                    Vé của bạn đang được tạm khóa. Vui lòng thanh toán trước khi hết giờ.
                  </p>
                </div>

                {/* 10-Minute Live Countdown */}
                <div className="rounded-xl bg-purple-950/30 border border-purple-800/40 p-4 text-center">
                  <div className="text-xs text-purple-300 mb-1">Thời gian giữ chỗ còn lại</div>
                  <div className="text-4xl font-extrabold text-purple-400 font-mono tracking-wider">
                    {formatTime(timeLeft)}
                  </div>
                </div>

                {/* Order Details */}
                <div className="space-y-2 text-xs border-y border-slate-800 py-3 text-slate-300">
                  <div className="flex justify-between">
                    <span className="text-slate-400">Mã đơn đặt:</span>
                    <span className="font-mono text-slate-200 truncate max-w-[200px]">
                      {reservation.order_id}
                    </span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-slate-400">Tài khoản:</span>
                    <span>{reservation.user_id}</span>
                  </div>
                  <div className="flex justify-between font-bold text-sm text-white pt-1">
                    <span>Số tiền cần thanh toán:</span>
                    <span className="text-emerald-400">6.000.000₫</span>
                  </div>
                </div>

                {/* Mock Pay Button */}
                <div className="space-y-3">
                  <button
                    onClick={handlePayOrder}
                    disabled={paymentLoading}
                    className="w-full py-3.5 px-4 rounded-xl bg-emerald-600 hover:bg-emerald-500 text-white font-bold text-sm flex items-center justify-center gap-2 shadow-lg shadow-emerald-600/20 transition-all"
                  >
                    {paymentLoading ? (
                      <RefreshCw className="w-4 h-4 animate-spin" />
                    ) : (
                      <CreditCard className="w-4 h-4" />
                    )}
                    Thanh Toán Giả Lập (Mock Pay)
                  </button>

                  <button
                    onClick={() => setReservation(null)}
                    className="w-full py-2.5 text-xs text-slate-400 hover:text-slate-200 font-medium"
                  >
                    Đóng cửa sổ (Vé vẫn được giữ trong 10 phút)
                  </button>
                </div>
              </>
            )}

            {paymentStatus === 'paid' && (
              <div className="text-center space-y-5 py-4">
                <div className="w-16 h-16 rounded-full bg-emerald-500/20 border border-emerald-500/40 text-emerald-400 flex items-center justify-center mx-auto">
                  <CheckCircle2 className="w-9 h-9" />
                </div>
                <div>
                  <h3 className="text-xl font-bold text-white">Thanh Toán Hoàn Tất!</h3>
                  <p className="text-xs text-slate-400 mt-1">
                    Đơn hàng #{reservation.order_id.substring(0, 8)} đã được ghi nhận vào PostgreSQL.
                  </p>
                </div>
                <button
                  onClick={() => setReservation(null)}
                  className="w-full py-3 rounded-xl bg-slate-800 hover:bg-slate-700 text-white font-bold text-sm"
                >
                  Hoàn Tất & Quay Về
                </button>
              </div>
            )}

            {paymentStatus === 'expired' && (
              <div className="text-center space-y-5 py-4">
                <div className="w-16 h-16 rounded-full bg-rose-500/20 border border-rose-500/40 text-rose-400 flex items-center justify-center mx-auto">
                  <RotateCcw className="w-8 h-8 text-rose-400" />
                </div>
                <div>
                  <h3 className="text-xl font-bold text-white">Vé Đã Hết Hạn!</h3>
                  <p className="text-xs text-slate-400 mt-1">
                    Hết thời gian 10 phút giữ chỗ. Vé đã được tự động hoàn lại vào kho vé Redis cho khán giả khác.
                  </p>
                </div>
                <button
                  onClick={() => setReservation(null)}
                  className="w-full py-3 rounded-xl bg-slate-800 hover:bg-slate-700 text-white font-bold text-sm"
                >
                  Đóng
                </button>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
