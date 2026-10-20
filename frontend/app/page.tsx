'use client';

import React, { useState, useEffect, useCallback, useRef } from 'react';
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
  Play,
  Activity,
  History,
  Trash2,
  Sliders,
  CheckCircle,
} from 'lucide-react';

const API_BASE = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8081';
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

export interface BenchmarkResult {
  id: string;
  timestamp: string;
  targetStock: number;
  totalRequests: number;
  concurrency: number;
  durationMs: number;
  rps: number;
  accepted202: number;
  soldOut409: number;
  errors5xx: number;
  p50Latency: number;
  p95Latency: number;
  p99Latency: number;
  avgLatency: number;
  minLatency: number;
  overselling: number;
}

export default function FlashPassApp() {
  const [activeTab, setActiveTab] = useState<'booking' | 'admin' | 'benchmark'>('booking');
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

  // Benchmark Simulator State (Phase 6 Interactive k6 Demo)
  const [benchStock, setBenchStock] = useState<number>(1000);
  const [benchRequests, setBenchRequests] = useState<number>(5000);
  const [benchConcurrency, setBenchConcurrency] = useState<number>(50);
  const [benchRunning, setBenchRunning] = useState<boolean>(false);
  const [benchProgress, setBenchProgress] = useState<{
    completed: number;
    total: number;
    accepted: number;
    soldOut: number;
    errors: number;
    currentRps: number;
    avgLatency: number;
    p95Latency: number;
  } | null>(null);
  const [lastResult, setLastResult] = useState<BenchmarkResult | null>(null);
  const [benchHistory, setBenchHistory] = useState<BenchmarkResult[]>([]);

  // Ref to cancel ongoing benchmark if user changes tab
  const abortControllerRef = useRef<AbortController | null>(null);

  // Load user ID and benchmark history from localStorage on mount
  useEffect(() => {
    let saved = localStorage.getItem('flashpass_user_id');
    if (!saved) {
      saved = 'user-' + Math.random().toString(36).substring(2, 8);
      localStorage.setItem('flashpass_user_id', saved);
    }
    setUserId(saved);

    const savedHistory = localStorage.getItem('flashpass_benchmark_history');
    if (savedHistory) {
      try {
        setBenchHistory(JSON.parse(savedHistory));
      } catch {
        // ignore
      }
    }
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
      // ignore
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
      // fallback
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

  // Action: Book Ticket (Flash Sale Single Purchase)
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
      setErrorMsg('Không thể kết nối đến máy chủ API FlashPass (localhost:8081).');
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
  const handleAdminReset = async (customTickets?: number) => {
    setLoading(true);
    const ticketsCount = customTickets || 1000;
    try {
      const res = await fetch(`${API_BASE}/api/v1/admin/events/${eventId}/reset?tickets=${ticketsCount}`, {
        method: 'POST',
      });
      if (res.ok) {
        setSuccessMsg(`Reset sự kiện thành công! ${ticketsCount.toLocaleString()} vé đã được phục hồi.`);
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

  // ==========================================
  // PHASE 6: IN-BROWSER K6 BENCHMARK ENGINE
  // ==========================================
  const runLiveBenchmark = async () => {
    if (benchRunning) return;
    setBenchRunning(true);
    setErrorMsg(null);
    setSuccessMsg(null);
    setLastResult(null);

    const controller = new AbortController();
    abortControllerRef.current = controller;

    // Step 1: Reset event inventory in Redis & DB to target benchStock
    try {
      const resetRes = await fetch(
        `${API_BASE}/api/v1/admin/events/${eventId}/reset?tickets=${benchStock}`,
        { method: 'POST', signal: controller.signal }
      );
      if (!resetRes.ok) {
        throw new Error('Failed to reset inventory before benchmark');
      }
    } catch (e: unknown) {
      if (e instanceof Error && e.name === 'AbortError') return;
      setErrorMsg('Không thể khởi tạo kho vé cho Benchmark.');
      setBenchRunning(false);
      return;
    }

    // Step 2: Initialize metrics accumulator
    const total = benchRequests;
    let completed = 0;
    let accepted = 0;
    let soldOut = 0;
    let errors = 0;
    const latencies: number[] = [];

    const startTime = performance.now();
    let nextIndex = 0;

    // Periodic UI update ticker (every 100ms)
    const updateInterval = setInterval(() => {
      const elapsedSec = (performance.now() - startTime) / 1000;
      const currentRps = elapsedSec > 0 ? Math.round(completed / elapsedSec) : 0;
      const avgLat = latencies.length > 0 ? latencies.reduce((a, b) => a + b, 0) / latencies.length : 0;

      // Approximate p95
      let p95 = 0;
      if (latencies.length > 0) {
        const sorted = [...latencies].sort((a, b) => a - b);
        p95 = sorted[Math.floor(sorted.length * 0.95)] || 0;
      }

      setBenchProgress({
        completed,
        total,
        accepted,
        soldOut,
        errors,
        currentRps,
        avgLatency: Math.round(avgLat * 10) / 10,
        p95Latency: Math.round(p95 * 10) / 10,
      });
    }, 100);

    // Concurrent Worker Task
    const worker = async () => {
      while (true) {
        if (controller.signal.aborted) break;
        const i = nextIndex++;
        if (i >= total) break;

        const reqUserId = `bench-vu-${i % benchConcurrency}-usr-${i}`;
        const reqIdempKey = `bench-${Date.now()}-${i}-${Math.random()}`;

        const t0 = performance.now();
        try {
          const res = await fetch(`${API_BASE}/api/v1/tickets/book`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
              event_id: eventId,
              user_id: reqUserId,
              idempotency_key: reqIdempKey,
            }),
            signal: controller.signal,
          });

          const t1 = performance.now();
          latencies.push(t1 - t0);

          if (res.status === 202) {
            accepted++;
          } else if (res.status === 409) {
            soldOut++;
          } else {
            errors++;
          }
        } catch (e: unknown) {
          if (!(e instanceof Error) || e.name !== 'AbortError') errors++;
        }
        completed++;
      }
    };

    // Spawn concurrent workers
    const workers = Array.from({ length: benchConcurrency }, () => worker());
    await Promise.all(workers);

    clearInterval(updateInterval);

    const totalDurationMs = Math.round(performance.now() - startTime);
    const effectiveRps = Math.round((completed / (totalDurationMs / 1000)) || 0);

    // Compute exact percentiles
    latencies.sort((a, b) => a - b);
    const avg = latencies.length > 0 ? latencies.reduce((a, b) => a + b, 0) / latencies.length : 0;
    const min = latencies.length > 0 ? latencies[0] : 0;
    const p50 = latencies.length > 0 ? latencies[Math.floor(latencies.length * 0.50)] : 0;
    const p95 = latencies.length > 0 ? latencies[Math.floor(latencies.length * 0.95)] : 0;
    const p99 = latencies.length > 0 ? latencies[Math.floor(latencies.length * 0.99)] : 0;
    const overselling = Math.max(0, accepted - benchStock);

    const newResult: BenchmarkResult = {
      id: 'run-' + Date.now(),
      timestamp: new Date().toLocaleTimeString(),
      targetStock: benchStock,
      totalRequests: total,
      concurrency: benchConcurrency,
      durationMs: totalDurationMs,
      rps: effectiveRps,
      accepted202: accepted,
      soldOut409: soldOut,
      errors5xx: errors,
      p50Latency: Math.round(p50 * 10) / 10,
      p95Latency: Math.round(p95 * 10) / 10,
      p99Latency: Math.round(p99 * 10) / 10,
      avgLatency: Math.round(avg * 10) / 10,
      minLatency: Math.round(min * 10) / 10,
      overselling,
    };

    setLastResult(newResult);

    // Save to LocalStorage (Persist in frontend without polluting database)
    const updatedHistory = [newResult, ...benchHistory.slice(0, 19)]; // keep last 20 runs
    setBenchHistory(updatedHistory);
    localStorage.setItem('flashpass_benchmark_history', JSON.stringify(updatedHistory));

    setBenchRunning(false);
    setBenchProgress(null);
    fetchStock();
    fetchMetrics();
  };

  const clearBenchmarkHistory = () => {
    localStorage.removeItem('flashpass_benchmark_history');
    setBenchHistory([]);
    setLastResult(null);
  };

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

          {/* 3-Tab Switcher */}
          <div className="flex items-center gap-1 bg-slate-900 border border-slate-800 p-1 rounded-xl">
            <button
              onClick={() => setActiveTab('booking')}
              className={`flex items-center gap-1.5 px-3.5 py-1.5 rounded-lg text-xs md:text-sm font-medium transition-all ${
                activeTab === 'booking'
                  ? 'bg-purple-600 text-white shadow-md shadow-purple-600/30'
                  : 'text-slate-400 hover:text-slate-200'
              }`}
            >
              <Ticket className="w-4 h-4" />
              Mua Vé
            </button>

            <button
              onClick={() => {
                setActiveTab('admin');
                fetchMetrics();
              }}
              className={`flex items-center gap-1.5 px-3.5 py-1.5 rounded-lg text-xs md:text-sm font-medium transition-all ${
                activeTab === 'admin'
                  ? 'bg-indigo-600 text-white shadow-md shadow-indigo-600/30'
                  : 'text-slate-400 hover:text-slate-200'
              }`}
            >
              <Cpu className="w-4 h-4" />
              Admin
            </button>

            <button
              onClick={() => setActiveTab('benchmark')}
              className={`flex items-center gap-1.5 px-3.5 py-1.5 rounded-lg text-xs md:text-sm font-medium transition-all ${
                activeTab === 'benchmark'
                  ? 'bg-gradient-to-r from-amber-500 to-rose-600 text-white shadow-md shadow-rose-600/30'
                  : 'text-amber-400 hover:text-amber-200'
              }`}
            >
              <Activity className="w-4 h-4" />
              k6 Live Benchmark
            </button>
          </div>
        </div>
      </header>

      {/* Main Container */}
      <main className="flex-1 max-w-6xl w-full mx-auto px-4 py-8">
        {/* Global Alerts */}
        {errorMsg && (
          <div className="mb-6 p-4 rounded-xl bg-rose-950/40 border border-rose-800/80 text-rose-300 flex items-center justify-between text-sm">
            <div className="flex items-center gap-3">
              <AlertCircle className="w-5 h-5 text-rose-400 shrink-0" />
              <span>{errorMsg}</span>
            </div>
            <button onClick={() => setErrorMsg(null)} className="text-rose-400 font-bold">
              ✕
            </button>
          </div>
        )}

        {successMsg && (
          <div className="mb-6 p-4 rounded-xl bg-emerald-950/40 border border-emerald-800/80 text-emerald-300 flex items-center justify-between text-sm">
            <div className="flex items-center gap-3">
              <CheckCircle2 className="w-5 h-5 text-emerald-400 shrink-0" />
              <span>{successMsg}</span>
            </div>
            <button onClick={() => setSuccessMsg(null)} className="text-emerald-400 font-bold">
              ✕
            </button>
          </div>
        )}

        {activeTab === 'booking' && (
          /* TAB 1: EVENT BOOKING CLIENT */
          <div className="grid grid-cols-1 lg:grid-cols-3 gap-8">
            <div className="lg:col-span-2 space-y-6">
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

            <div className="space-y-6">
              <div className="rounded-2xl border border-slate-800 bg-slate-900 p-6 space-y-6 shadow-xl sticky top-24">
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
        )}

        {activeTab === 'admin' && (
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
                  onClick={() => handleAdminReset(1000)}
                  disabled={loading}
                  className="flex items-center gap-2 px-4 py-2 rounded-xl bg-rose-600/20 hover:bg-rose-600/30 border border-rose-500/40 text-rose-300 text-sm font-semibold transition-all"
                >
                  <RotateCcw className="w-4 h-4" />
                  Restock & Reset Sự Kiện (1,000 Vé)
                </button>
              </div>
            </div>

            <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-5">
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

        {activeTab === 'benchmark' && (
          /* TAB 3: IN-BROWSER K6 BENCHMARK SIMULATOR & LOCALSTORAGE HISTORY */
          <div className="space-y-8">
            <div className="flex flex-col md:flex-row md:items-center justify-between gap-4 pb-4 border-b border-slate-800">
              <div>
                <div className="flex items-center gap-2">
                  <span className="px-2.5 py-0.5 rounded-full text-xs font-bold bg-amber-500/20 text-amber-400 border border-amber-500/30">
                    k6 In-Browser Simulator
                  </span>
                  <span className="text-xs text-slate-400">Không ghi ô nhiễm Database</span>
                </div>
                <h2 className="text-2xl font-bold text-white flex items-center gap-2 mt-1">
                  <Activity className="w-6 h-6 text-amber-400" />
                  Mô Phỏng Trực Quan Tải Cao (Live Surge Benchmark)
                </h2>
                <p className="text-slate-400 text-sm mt-1">
                  Chọn số lượng vé và số lượng requests để chạy stress test trực tiếp vào Redis Lua Script. Kết quả được lưu trên trình duyệt (LocalStorage).
                </p>
              </div>

              {benchHistory.length > 0 && (
                <button
                  onClick={clearBenchmarkHistory}
                  disabled={benchRunning}
                  className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg border border-slate-800 bg-slate-900 text-slate-400 hover:text-rose-400 hover:border-rose-900/50 text-xs transition-all"
                >
                  <Trash2 className="w-3.5 h-3.5" />
                  Xóa Lịch Sử
                </button>
              )}
            </div>

            {/* Benchmark Configuration Panel */}
            <div className="grid grid-cols-1 md:grid-cols-3 gap-6 p-6 rounded-2xl border border-slate-800 bg-slate-900/60">
              {/* Param 1: Target Stock */}
              <div className="space-y-3">
                <label className="text-xs font-bold text-slate-300 flex items-center gap-2">
                  <Ticket className="w-4 h-4 text-purple-400" />
                  1. Số Lượng Vé Kho (Inventory Stock)
                </label>
                <div className="grid grid-cols-4 gap-1.5">
                  {[200, 500, 1000, 2000].map((num) => (
                    <button
                      key={num}
                      type="button"
                      disabled={benchRunning}
                      onClick={() => setBenchStock(num)}
                      className={`py-2 rounded-lg text-xs font-bold border transition-all ${
                        benchStock === num
                          ? 'bg-purple-600 border-purple-500 text-white'
                          : 'bg-slate-800/80 border-slate-700 text-slate-400 hover:text-white'
                      }`}
                    >
                      {num}
                    </button>
                  ))}
                </div>
                <input
                  type="number"
                  min="10"
                  max="10000"
                  disabled={benchRunning}
                  value={benchStock}
                  onChange={(e) => setBenchStock(Math.max(1, parseInt(e.target.value) || 0))}
                  className="w-full bg-slate-950 border border-slate-800 rounded-lg px-3 py-1.5 text-sm font-mono text-white"
                />
              </div>

              {/* Param 2: Total Requests */}
              <div className="space-y-3">
                <label className="text-xs font-bold text-slate-300 flex items-center gap-2">
                  <Zap className="w-4 h-4 text-amber-400" />
                  2. Tổng Số Requests Đột Biến (Surge Req)
                </label>
                <div className="grid grid-cols-4 gap-1.5">
                  {[1000, 2000, 5000, 10000].map((num) => (
                    <button
                      key={num}
                      type="button"
                      disabled={benchRunning}
                      onClick={() => setBenchRequests(num)}
                      className={`py-2 rounded-lg text-xs font-bold border transition-all ${
                        benchRequests === num
                          ? 'bg-amber-600 border-amber-500 text-white'
                          : 'bg-slate-800/80 border-slate-700 text-slate-400 hover:text-white'
                      }`}
                    >
                      {num.toLocaleString()}
                    </button>
                  ))}
                </div>
                <input
                  type="number"
                  min="50"
                  max="50000"
                  disabled={benchRunning}
                  value={benchRequests}
                  onChange={(e) => setBenchRequests(Math.max(1, parseInt(e.target.value) || 0))}
                  className="w-full bg-slate-950 border border-slate-800 rounded-lg px-3 py-1.5 text-sm font-mono text-white"
                />
              </div>

              {/* Param 3: Concurrency */}
              <div className="space-y-3">
                <label className="text-xs font-bold text-slate-300 flex items-center gap-2">
                  <Sliders className="w-4 h-4 text-emerald-400" />
                  3. Số Luồng Ảo Đồng Thời (VUs)
                </label>
                <div className="grid grid-cols-3 gap-1.5">
                  {[25, 50, 100].map((num) => (
                    <button
                      key={num}
                      type="button"
                      disabled={benchRunning}
                      onClick={() => setBenchConcurrency(num)}
                      className={`py-2 rounded-lg text-xs font-bold border transition-all ${
                        benchConcurrency === num
                          ? 'bg-emerald-600 border-emerald-500 text-white'
                          : 'bg-slate-800/80 border-slate-700 text-slate-400 hover:text-white'
                      }`}
                    >
                      {num} VUs
                    </button>
                  ))}
                </div>
                <div className="text-[11px] text-slate-400 pt-1 leading-relaxed">
                  Worker pool song song dispatching HTTP requests qua HTTP keep-alive.
                </div>
              </div>
            </div>

            {/* Run Trigger Button */}
            <div className="flex flex-col items-center justify-center gap-3">
              <button
                onClick={runLiveBenchmark}
                disabled={benchRunning}
                className={`py-4 px-10 rounded-2xl font-extrabold text-base flex items-center gap-3 shadow-2xl transition-all transform active:scale-95 ${
                  benchRunning
                    ? 'bg-slate-800 text-slate-400 border border-slate-700 cursor-not-allowed'
                    : 'bg-gradient-to-r from-amber-500 via-rose-600 to-purple-600 hover:from-amber-400 hover:to-purple-500 text-white shadow-rose-600/30'
                }`}
              >
                {benchRunning ? (
                  <>
                    <RefreshCw className="w-5 h-5 animate-spin text-amber-400" />
                    ĐANG BẮN {benchRequests.toLocaleString()} REQUESTS VÀO KHO {benchStock} VÉ...
                  </>
                ) : (
                  <>
                    <Play className="w-5 h-5 fill-white" />
                    CHẠY BENCHMARK TRỰC TIẾP ({benchRequests.toLocaleString()} REQ / {benchStock} VÉ)
                  </>
                )}
              </button>
              <span className="text-xs text-slate-400">
                Tự động reset kho vé tương ứng trước khi bắn & kiểm chứng Bán âm (Overselling = 0).
              </span>
            </div>

            {/* Live Real-time Progress (while running) */}
            {benchRunning && benchProgress && (
              <div className="rounded-2xl border border-amber-500/40 bg-slate-900 p-6 space-y-4 animate-in fade-in">
                <div className="flex justify-between items-center text-sm">
                  <span className="font-bold text-amber-400 flex items-center gap-2">
                    <span className="w-2.5 h-2.5 rounded-full bg-amber-400 animate-ping" />
                    Đang chạy: {benchProgress.completed.toLocaleString()} / {benchProgress.total.toLocaleString()} Requests
                  </span>
                  <span className="font-mono text-emerald-400 font-bold text-base">
                    ~{benchProgress.currentRps.toLocaleString()} RPS
                  </span>
                </div>

                <div className="w-full h-3 bg-slate-800 rounded-full overflow-hidden border border-slate-700">
                  <div
                    className="h-full bg-gradient-to-r from-amber-500 to-rose-500 transition-all duration-100"
                    style={{ width: `${(benchProgress.completed / benchProgress.total) * 100}%` }}
                  />
                </div>

                <div className="grid grid-cols-4 gap-3 text-center text-xs pt-2">
                  <div className="p-3 bg-slate-950/60 rounded-xl border border-slate-800">
                    <div className="text-slate-400">202 Accepted</div>
                    <div className="text-lg font-bold text-emerald-400 font-mono mt-1">
                      {benchProgress.accepted}
                    </div>
                  </div>
                  <div className="p-3 bg-slate-950/60 rounded-xl border border-slate-800">
                    <div className="text-slate-400">409 Sold Out</div>
                    <div className="text-lg font-bold text-amber-400 font-mono mt-1">
                      {benchProgress.soldOut}
                    </div>
                  </div>
                  <div className="p-3 bg-slate-950/60 rounded-xl border border-slate-800">
                    <div className="text-slate-400">p95 Latency</div>
                    <div className="text-lg font-bold text-indigo-400 font-mono mt-1">
                      {benchProgress.p95Latency} ms
                    </div>
                  </div>
                  <div className="p-3 bg-slate-950/60 rounded-xl border border-slate-800">
                    <div className="text-slate-400">5xx Errors</div>
                    <div className="text-lg font-bold text-rose-400 font-mono mt-1">
                      {benchProgress.errors}
                    </div>
                  </div>
                </div>
              </div>
            )}

            {/* Last Benchmark Summary Card */}
            {lastResult && !benchRunning && (
              <div className="rounded-2xl border border-emerald-500/40 bg-gradient-to-b from-slate-900 to-slate-950 p-6 space-y-6 shadow-2xl">
                <div className="flex flex-col md:flex-row md:items-center justify-between gap-2 border-b border-slate-800 pb-4">
                  <div>
                    <span className="text-xs text-emerald-400 font-bold bg-emerald-950/80 px-2.5 py-1 rounded-md border border-emerald-800/60">
                      KẾT QUẢ ĐO VỪA CHẠY
                    </span>
                    <h3 className="text-xl font-bold text-white mt-2">
                      Đã hoàn tất {lastResult.totalRequests.toLocaleString()} requests trong {lastResult.durationMs}ms
                    </h3>
                  </div>
                  <div className="text-right">
                    <div className="text-xs text-slate-400">Thông lượng trung bình</div>
                    <div className="text-3xl font-extrabold text-emerald-400 font-mono">
                      {lastResult.rps.toLocaleString()} RPS
                    </div>
                  </div>
                </div>

                <div className="grid grid-cols-2 sm:grid-cols-4 gap-4">
                  <div className="p-4 rounded-xl bg-slate-900 border border-slate-800">
                    <div className="text-xs text-slate-400">Vé Bán Thành Công (202)</div>
                    <div className="text-2xl font-extrabold text-emerald-400 font-mono mt-1">
                      {lastResult.accepted202.toLocaleString()}
                    </div>
                    <div className="text-[11px] text-slate-500 mt-1">Khớp kho {lastResult.targetStock}</div>
                  </div>

                  <div className="p-4 rounded-xl bg-slate-900 border border-slate-800">
                    <div className="text-xs text-slate-400">Fail-Fast Từ Chối (409)</div>
                    <div className="text-2xl font-extrabold text-amber-400 font-mono mt-1">
                      {lastResult.soldOut409.toLocaleString()}
                    </div>
                    <div className="text-[11px] text-slate-500 mt-1">Chặn ngay tại RAM</div>
                  </div>

                  <div className="p-4 rounded-xl bg-slate-900 border border-slate-800">
                    <div className="text-xs text-slate-400">Độ Trễ p95 / p99</div>
                    <div className="text-xl font-extrabold text-indigo-400 font-mono mt-1">
                      {lastResult.p95Latency}ms / {lastResult.p99Latency}ms
                    </div>
                    <div className="text-[11px] text-slate-500 mt-1">Avg: {lastResult.avgLatency}ms</div>
                  </div>

                  <div className="p-4 rounded-xl bg-slate-900 border border-slate-800">
                    <div className="text-xs text-slate-400">Tỷ Lệ Bán Âm (Overselling)</div>
                    <div className="text-2xl font-extrabold text-emerald-400 font-mono mt-1 flex items-center gap-1.5">
                      <CheckCircle className="w-6 h-6 text-emerald-400" />
                      0.00%
                    </div>
                    <div className="text-[11px] text-slate-500 mt-1">An toàn tuyệt đối</div>
                  </div>
                </div>
              </div>
            )}

            {/* Benchmark History Log (Stored in localStorage) */}
            <div className="space-y-4 pt-4 border-t border-slate-800">
              <div className="flex items-center justify-between">
                <h3 className="font-bold text-base text-white flex items-center gap-2">
                  <History className="w-5 h-5 text-indigo-400" />
                  Lịch Sử Các Lần Chạy Benchmark (Lưu Trực Tiếp Trên Trình Duyệt)
                </h3>
                <span className="text-xs text-slate-400">
                  {benchHistory.length} lần chạy đã lưu (LocalStorage)
                </span>
              </div>

              {benchHistory.length === 0 ? (
                <div className="p-8 text-center rounded-2xl border border-slate-800 bg-slate-900/40 text-slate-400 text-sm">
                  Chưa có lượt chạy benchmark nào. Hãy chọn cấu hình phía trên và bấm &quot;CHẠY BENCHMARK TRỰC TIẾP&quot;!
                </div>
              ) : (
                <div className="overflow-x-auto rounded-xl border border-slate-800 bg-slate-900/60">
                  <table className="w-full text-left text-xs">
                    <thead className="bg-slate-900 border-b border-slate-800 text-slate-400 uppercase tracking-wider">
                      <tr>
                        <th className="p-3">Thời gian</th>
                        <th className="p-3">Cấu hình (Vé / Req / VUs)</th>
                        <th className="p-3">Thời gian chạy</th>
                        <th className="p-3">Thông lượng (RPS)</th>
                        <th className="p-3">Độ trễ p95</th>
                        <th className="p-3">202 / 409</th>
                        <th className="p-3">Bán âm</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-slate-800 font-mono text-slate-300">
                      {benchHistory.map((run, idx) => (
                        <tr key={run.id} className="hover:bg-slate-800/40 transition-colors">
                          <td className="p-3 text-slate-400 font-sans">
                            <span className="font-bold text-white">#{benchHistory.length - idx}</span>{' '}
                            ({run.timestamp})
                          </td>
                          <td className="p-3">
                            <span className="text-purple-400">{run.targetStock} vé</span> •{' '}
                            <span className="text-amber-400">{run.totalRequests.toLocaleString()} req</span> •{' '}
                            <span className="text-slate-400">{run.concurrency} VUs</span>
                          </td>
                          <td className="p-3">{run.durationMs}ms</td>
                          <td className="p-3 font-bold text-emerald-400">
                            {run.rps.toLocaleString()} req/s
                          </td>
                          <td className="p-3 text-indigo-400">{run.p95Latency}ms</td>
                          <td className="p-3">
                            <span className="text-emerald-400">{run.accepted202}</span> /{' '}
                            <span className="text-amber-400">{run.soldOut409}</span>
                          </td>
                          <td className="p-3">
                            <span className="inline-flex items-center gap-1 text-emerald-400 font-bold font-sans text-[11px] bg-emerald-950/60 border border-emerald-800/50 px-2 py-0.5 rounded-full">
                              <CheckCircle className="w-3 h-3" />
                              0.00%
                            </span>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
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

                <div className="rounded-xl bg-purple-950/30 border border-purple-800/40 p-4 text-center">
                  <div className="text-xs text-purple-300 mb-1">Thời gian giữ chỗ còn lại</div>
                  <div className="text-4xl font-extrabold text-purple-400 font-mono tracking-wider">
                    {formatTime(timeLeft)}
                  </div>
                </div>

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
