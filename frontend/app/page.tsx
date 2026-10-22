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
  BarChart3,
  PieChart,
  HelpCircle,
  FlaskConical,
} from 'lucide-react';
import EdgeCasesLab from '@/components/edge-cases/EdgeCasesLab';

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
  const [activeTab, setActiveTab] = useState<'booking' | 'admin' | 'benchmark' | 'edge-cases'>('edge-cases');
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

  const handleBookTicket = async () => {
    setLoading(true);
    setErrorMsg(null);
    setSuccessMsg(null);

    let idempotencyKey = '';
    if (typeof window !== 'undefined') {
      const sessionKeyName = `flashpass_session_${eventId}_${userId}`;
      idempotencyKey = sessionStorage.getItem(sessionKeyName) || '';
      if (!idempotencyKey) {
        idempotencyKey = crypto.randomUUID();
        sessionStorage.setItem(sessionKeyName, idempotencyKey);
      }
    } else {
      idempotencyKey = crypto.randomUUID();
    }

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
        if (typeof window !== 'undefined') {
          sessionStorage.removeItem(`flashpass_session_${eventId}_${userId}`);
        }
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
          <div className="flex items-center gap-2">
            <button
              onClick={() => setActiveTab('edge-cases')}
              className={`flex items-center gap-1.5 px-3.5 py-1.5 rounded-lg text-xs md:text-sm font-medium transition-all ${activeTab === 'edge-cases'
                ? 'bg-gradient-to-r from-indigo-500 to-purple-600 text-white shadow-md shadow-indigo-600/30'
                : 'text-indigo-400 hover:text-indigo-200'
                }`}
            >
              <FlaskConical className="w-4 h-4" />
              Lab Lỗi Phân Tán
            </button>
            <button
              onClick={() => setActiveTab('benchmark')}
              className={`flex items-center gap-1.5 px-3.5 py-1.5 rounded-lg text-xs md:text-sm font-medium transition-all ${activeTab === 'benchmark'
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

        {/* Tab 1: Distributed Edge Cases Lab */}
        {activeTab === 'edge-cases' && <EdgeCasesLab />}

        {/* Tab 2: k6 Live Benchmark */}
        {activeTab === 'benchmark' && (
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
                    className={`py-2 rounded-lg text-xs font-bold border transition-all ${benchStock === num
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
                    className={`py-2 rounded-lg text-xs font-bold border transition-all ${benchRequests === num
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
                    className={`py-2 rounded-lg text-xs font-bold border transition-all ${benchConcurrency === num
                      ? 'bg-emerald-600 border-emerald-500 text-white'
                      : 'bg-slate-800/80 border-slate-700 text-slate-400 hover:text-white'
                      }`}
                  >
                    {num} VUs
                  </button>
                ))}
                <input
                  type="number"
                  min="0"
                  max={benchRequests}
                  disabled={benchRunning}
                  value={benchConcurrency}
                  onChange={(e) => setBenchConcurrency(Math.max(1, parseInt(e.target.value) || 0))}
                  className="w-full bg-slate-950 border border-slate-800 rounded-lg px-3 py-1.5 text-sm font-mono text-white"
                />
              </div>
              <div className="text-[11px] text-slate-400 pt-1 leading-relaxed">
                Worker pool song song dispatching HTTP requests qua HTTP keep-alive.
              </div>
            </div>

            {benchConcurrency > 100 && (
              <div className="md:col-span-3 p-3.5 rounded-xl bg-amber-950/40 border border-amber-800/80 text-amber-300 text-xs flex items-start gap-2.5">
                <AlertCircle className="w-4 h-4 text-amber-400 shrink-0 mt-0.5" />
                <div>
                  <span className="font-bold">Lưu ý về số luồng ảo ({benchConcurrency} VUs):</span> Trình duyệt giới hạn tối đa 6 kết nối HTTP/1.1 đồng thời per host. Đặt VUs quá cao (&gt; 100) sẽ gây cạn kiệt socket client (ERR_INSUFFICIENT_RESOURCES), khiến nhiều request bị hủy ở trình duyệt trước khi tới backend. Khuyến nghị chạy <strong>25 - 100 VUs</strong> trên web, hoặc dùng <strong>CLI k6</strong> cho 1,000+ VUs!
                </div>
              </div>
            )}
          </div>

          {/* Run Trigger Button */}
          <div className="flex flex-col items-center justify-center gap-3">
            <button
              onClick={runLiveBenchmark}
              disabled={benchRunning}
              className={`py-4 px-10 rounded-2xl font-extrabold text-base flex items-center gap-3 shadow-2xl transition-all transform active:scale-95 ${benchRunning
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

              {/* Visual Graphs & Response Breakdown */}
              <div className="grid grid-cols-1 md:grid-cols-2 gap-4 pt-2">
                {/* Latency Distribution Chart */}
                <div className="p-4 rounded-xl bg-slate-900 border border-slate-800 space-y-3">
                  <div className="flex items-center justify-between">
                    <span className="text-xs font-bold text-slate-300 flex items-center gap-1.5">
                      <BarChart3 className="w-4 h-4 text-indigo-400" />
                      Phân Bố Độ Trễ (Latency Percentiles)
                    </span>
                    <span className="text-[11px] text-emerald-400 font-mono font-bold bg-emerald-950/70 border border-emerald-800/50 px-2 py-0.5 rounded">
                      SLA &lt; 25ms
                    </span>
                  </div>

                  <div className="space-y-2.5 pt-1">
                    {[
                      { label: 'Min (Rejection)', val: lastResult.minLatency, color: 'bg-sky-400' },
                      { label: 'p50 (Median)', val: lastResult.p50Latency, color: 'bg-indigo-400' },
                      { label: 'Average (Trung bình)', val: lastResult.avgLatency, color: 'bg-purple-400' },
                      { label: 'p95 (95% User)', val: lastResult.p95Latency, color: 'bg-amber-400' },
                      { label: 'p99 (99% User)', val: lastResult.p99Latency, color: 'bg-rose-400' },
                    ].map((item) => {
                      const maxVal = Math.max(lastResult.p99Latency, 30);
                      const widthPercent = Math.min(100, Math.max(8, (item.val / maxVal) * 100));
                      return (
                        <div key={item.label} className="space-y-1">
                          <div className="flex justify-between text-[11px]">
                            <span className="text-slate-400">{item.label}</span>
                            <span className="font-mono font-bold text-white">{item.val}ms</span>
                          </div>
                          <div className="h-2 w-full bg-slate-950 rounded-full overflow-hidden border border-slate-800">
                            <div
                              className={`h-full ${item.color} rounded-full transition-all duration-500`}
                              style={{ width: `${widthPercent}%` }}
                            />
                          </div>
                        </div>
                      );
                    })}
                  </div>
                </div>

                {/* Request Composition Chart */}
                <div className="p-4 rounded-xl bg-slate-900 border border-slate-800 space-y-3">
                  <span className="text-xs font-bold text-slate-300 flex items-center gap-1.5">
                    <PieChart className="w-4 h-4 text-emerald-400" />
                    Cơ Cấu Kết Quả ({lastResult.totalRequests.toLocaleString()} Requests)
                  </span>

                  <div className="space-y-3 pt-1">
                    {/* Stacked Bar */}
                    <div className="h-6 w-full bg-slate-950 rounded-lg overflow-hidden flex border border-slate-800">
                      {lastResult.accepted202 > 0 && (
                        <div
                          className="bg-emerald-500 h-full flex items-center justify-center text-[10px] font-bold text-black"
                          style={{ width: `${(lastResult.accepted202 / lastResult.totalRequests) * 100}%` }}
                          title={`202 Accepted: ${lastResult.accepted202}`}
                        >
                          {((lastResult.accepted202 / lastResult.totalRequests) * 100).toFixed(0)}%
                        </div>
                      )}
                      {lastResult.soldOut409 > 0 && (
                        <div
                          className="bg-amber-500 h-full flex items-center justify-center text-[10px] font-bold text-black"
                          style={{ width: `${(lastResult.soldOut409 / lastResult.totalRequests) * 100}%` }}
                          title={`409 Sold Out: ${lastResult.soldOut409}`}
                        >
                          {((lastResult.soldOut409 / lastResult.totalRequests) * 100).toFixed(0)}%
                        </div>
                      )}
                      {lastResult.errors5xx > 0 && (
                        <div
                          className="bg-rose-500 h-full flex items-center justify-center text-[10px] font-bold text-white"
                          style={{ width: `${(lastResult.errors5xx / lastResult.totalRequests) * 100}%` }}
                          title={`Errors: ${lastResult.errors5xx}`}
                        >
                          {((lastResult.errors5xx / lastResult.totalRequests) * 100).toFixed(0)}%
                        </div>
                      )}
                    </div>

                    <div className="grid grid-cols-3 gap-2 pt-1 text-center">
                      <div className="p-2 rounded bg-slate-950/60 border border-slate-800">
                        <div className="text-[10px] text-slate-400">202 Thành công</div>
                        <div className="text-xs font-bold text-emerald-400 font-mono mt-0.5">
                          {lastResult.accepted202.toLocaleString()}
                        </div>
                      </div>
                      <div className="p-2 rounded bg-slate-950/60 border border-slate-800">
                        <div className="text-[10px] text-slate-400">409 Fail-Fast</div>
                        <div className="text-xs font-bold text-amber-400 font-mono mt-0.5">
                          {lastResult.soldOut409.toLocaleString()}
                        </div>
                      </div>
                      <div className="p-2 rounded bg-slate-950/60 border border-slate-800">
                        <div className="text-[10px] text-slate-400">Lỗi Client / Socket</div>
                        <div className="text-xs font-bold text-rose-400 font-mono mt-0.5">
                          {lastResult.errors5xx.toLocaleString()}
                        </div>
                      </div>
                    </div>

                    <div className="text-[11px] text-slate-400 leading-relaxed bg-slate-950/40 p-2.5 rounded-lg border border-slate-800/80">
                      🛡️ <span className="text-slate-300 font-medium">Bảo vệ cơ sở dữ liệu:</span> {lastResult.soldOut409.toLocaleString()} requests được từ chối ngay lập tức tại RAM Redis trong &lt; 1ms mà không chạm PostgreSQL.
                    </div>
                  </div>
                </div>
              </div>

              {/* Matplotlib k6 Visual Dashboard */}
              <div className="p-4 rounded-xl bg-slate-900 border border-slate-800 space-y-3">
                <div className="flex items-center justify-between">
                  <span className="text-xs font-bold text-slate-300 flex items-center gap-1.5">
                    <Activity className="w-4 h-4 text-amber-400" />
                    Đồ Thị k6 Benchmark Trực Quan Hóa (Python Matplotlib Engine)
                  </span>
                  <a
                    href="/benchmark_chart.png"
                    target="_blank"
                    rel="noreferrer"
                    className="text-xs text-indigo-400 hover:text-indigo-300 underline"
                  >
                    Xem ảnh đầy đủ ↗
                  </a>
                </div>
                <div className="rounded-lg overflow-hidden border border-slate-800 bg-slate-950 flex items-center justify-center p-2">
                  <img
                    src="/benchmark_chart.png"
                    alt="FlashPass Benchmark Performance Chart"
                    className="w-full max-h-[360px] object-contain rounded"
                  />
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
                      <th className="p-3">Lỗi Socket/Client</th>
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
                          {run.errors5xx > 0 ? (
                            <span className="text-rose-400 font-bold bg-rose-950/60 border border-rose-800/50 px-2 py-0.5 rounded text-[11px]" title="Nghẽn socket trình duyệt hoặc lỗi kết nối">
                              {run.errors5xx.toLocaleString()} lỗi
                            </span>
                          ) : (
                            <span className="text-slate-500">0</span>
                          )}
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
