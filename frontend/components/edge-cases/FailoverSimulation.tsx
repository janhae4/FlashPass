'use client';

import React, { useState, useEffect, useRef } from 'react';
import {
  Server,
  Power,
  Play,
  RotateCcw,
  ShieldCheck,
  Flame,
  Terminal,
  User,
  CheckCircle2,
  AlertTriangle,
  Radio,
  ChevronLeft,
  ChevronRight,
  Pause,
  Database,
} from 'lucide-react';

const API_BASE = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8081';

interface LogEntry {
  id: string;
  time: string;
  source: 'CLIENT' | 'MASTER' | 'REPLICA' | 'SENTINEL' | 'API' | 'AUDIT';
  text: string;
  type: 'info' | 'success' | 'warn' | 'error';
}

export default function FailoverSimulation() {
  const [currentStep, setCurrentStep] = useState(0);
  const [mitigation, setMitigation] = useState(false);
  const [masterAlive, setMasterAlive] = useState(true);
  const [realMasterActive, setRealMasterActive] = useState<boolean | null>(null);
  const [replicaRole, setReplicaRole] = useState<'replica' | 'master'>('replica');
  const [masterStock, setMasterStock] = useState(1);
  const [replicaStock, setReplicaStock] = useState(1);
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [killingMaster, setKillingMaster] = useState(false);
  const [loadingStep, setLoadingStep] = useState(false);
  const [isAutoPlaying, setIsAutoPlaying] = useState(false);
  const [auditResult, setAuditResult] = useState<{
    total_orders: number;
    orders: Array<{ id: string; user_id: string; status: string; created_at: string }>;
    overselling: boolean;
  } | null>(null);

  const logEndRef = useRef<HTMLDivElement>(null);
  const autoPlayRef = useRef<NodeJS.Timeout | null>(null);

  const addLog = (source: LogEntry['source'], text: string, type: LogEntry['type'] = 'info') => {
    const time = new Date().toLocaleTimeString('vi-VN', { hour12: false });
    setLogs((prev) => [...prev, { id: Math.random().toString(), time, source, text, type }]);
  };

  const fetchMasterStatus = async () => {
    try {
      const res = await fetch(`${API_BASE}/api/v1/admin/redis/master/status`);
      if (res.ok) {
        const data = await res.json();
        setRealMasterActive(data.active);
      }
    } catch {
      // Ignore network errors in status poll
    }
  };

  useEffect(() => {
    fetchMasterStatus();
    const interval = setInterval(fetchMasterStatus, 3000);
    return () => clearInterval(interval);
  }, []);

  useEffect(() => {
    logEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [logs]);

  // Set simulation state driven by REAL BACKEND APIS
  const applyStepState = async (step: number) => {
    setCurrentStep(step);
    setLoadingStep(true);

    try {
      if (step === 0) {
        addLog('API', 'POST /api/v1/admin/failover/init -> Gọi API khởi tạo môi trường...', 'info');
        const res = await fetch(`${API_BASE}/api/v1/admin/failover/init`, { method: 'POST' });
        const data = await res.json();
        setMasterStock(data.master_stock ?? 1);
        setReplicaStock(data.replica_stock ?? 1);
        setMasterAlive(true);
        setRealMasterActive(true);
        setReplicaRole('replica');
        setAuditResult(null);
        addLog('API', `✓ Khởi tạo xong: Master = ${data.master_stock} vé, Replica = ${data.replica_stock} vé (Replication UP)`, 'success');
      } else if (step === 1) {
        addLog('CLIENT', `POST /api/v1/admin/failover/book-master?wait=${mitigation} -> Khách A gửi request...`, 'info');
        const res = await fetch(`${API_BASE}/api/v1/admin/failover/book-master?wait=${mitigation}`, { method: 'POST' });
        const data = await res.json();
        setMasterStock(data.master_stock ?? 0);
        setReplicaStock(data.replica_stock ?? (mitigation ? 0 : 1));
        addLog('MASTER', `✓ Lua DECRBY stock 1 -> ${data.master_stock} (Order: ${data.order_id?.slice(0, 8)}...)`, 'success');
        addLog('CLIENT', '✓ Khách A nhận 202 Accepted (Đã đẩy vào RabbitMQ)', 'success');
      } else if (step === 2) {
        if (mitigation) {
          addLog('MASTER', 'Cơ chế WAIT 1 (Đồng bộ): Replica đã nhận gói sync thành công (stock = 0)', 'success');
          setReplicaStock(0);
        } else {
          addLog('MASTER', 'Replication bất đồng bộ: Gói sync đang bay qua mạng. Replica vẫn giữ stock = 1 cũ!', 'warn');
          setReplicaStock(1);
        }
      } else if (step === 3) {
        addLog('API', 'POST /api/v1/admin/failover/crash-master -> docker pause flashpass_redis...', 'warn');
        const res = await fetch(`${API_BASE}/api/v1/admin/failover/crash-master`, { method: 'POST' });
        const data = await res.json();
        setMasterAlive(false);
        setRealMasterActive(false);
        addLog('MASTER', `[FATAL] ${data.message} (Cảng 6379 bị đóng băng)`, 'error');
        if (!mitigation) {
          addLog('API', 'Gói tin replication bị đứt đoạn giữa chừng, Replica chưa nhận được lệnh trừ vé!', 'warn');
        }
      } else if (step === 4) {
        addLog('API', 'POST /api/v1/admin/failover/promote-replica -> REPLICAOF NO ONE...', 'warn');
        const res = await fetch(`${API_BASE}/api/v1/admin/failover/promote-replica`, { method: 'POST' });
        const data = await res.json();
        setReplicaRole('master');
        setReplicaStock(data.replica_stock ?? (mitigation ? 0 : 1));
        addLog('SENTINEL', `✓ Sentinel đã bầu Replica (:6380) thành Master mới! (Role: ${data.replica_role})`, 'success');
      } else if (step === 5) {
        addLog('CLIENT', 'POST /api/v1/admin/failover/book-replica -> Khách B gửi request mua vé...', 'info');
        const res = await fetch(`${API_BASE}/api/v1/admin/failover/book-replica`, { method: 'POST' });
        const data = await res.json();

        if (res.status === 202) {
          setReplicaStock(data.replica_stock ?? 0);
          addLog('REPLICA', `Master mới: Thấy còn 1 vé cũ -> Trừ vé tiếp! (Order: ${data.order_id?.slice(0, 8)}...)`, 'error');
          addLog('CLIENT', 'Khách B nhận vé 202 Accepted thành công!', 'error');
        } else {
          setReplicaStock(0);
          addLog('REPLICA', 'Master mới: Thấy kho = 0 -> Trả về 409 Sold Out!', 'success');
          addLog('CLIENT', 'Khách B nhận 409 Hết Vé (An toàn)', 'success');
        }

        // Audit DB directly after worker has written to PostgreSQL
        setTimeout(async () => {
          try {
            const auditRes = await fetch(`${API_BASE}/api/v1/admin/failover/audit`);
            if (auditRes.ok) {
              const audit = await auditRes.json();
              setAuditResult(audit);
              if (audit.overselling) {
                addLog('AUDIT', `🚨 KIỂM TOÁN DATABASE: Có ${audit.total_orders} đơn trong PostgreSQL cho 1 vé -> BÁN ÂM 101% THẬT TRÊN DB!`, 'error');
              } else {
                addLog('AUDIT', `✓ KIỂM TOÁN DATABASE: Có đúng ${audit.total_orders} đơn trong PostgreSQL -> 0.00% BÁN ÂM!`, 'success');
              }
            }
          } catch {
            // Ignore audit fetch err
          }
        }, 800);
      }
    } catch (e: unknown) {
      const err = e instanceof Error ? e.message : 'Unknown error';
      addLog('API', `Lỗi mạng khi gọi API: ${err}`, 'error');
    } finally {
      setLoadingStep(false);
    }
  };

  const handleNextStep = () => {
    if (currentStep < 5 && !loadingStep) {
      applyStepState(currentStep + 1);
    }
  };

  const handlePrevStep = () => {
    if (currentStep > 0 && !loadingStep) {
      applyStepState(currentStep - 1);
    }
  };

  const stopAutoPlay = () => {
    if (autoPlayRef.current) {
      clearInterval(autoPlayRef.current);
      autoPlayRef.current = null;
    }
    setIsAutoPlaying(false);
  };

  const handleToggleAutoPlay = () => {
    if (isAutoPlaying) {
      stopAutoPlay();
      return;
    }

    setIsAutoPlaying(true);
    let step = currentStep >= 5 ? 0 : currentStep;
    applyStepState(step);

    autoPlayRef.current = setInterval(() => {
      step++;
      if (step <= 5) {
        applyStepState(step);
      } else {
        stopAutoPlay();
      }
    }, 2200);
  };

  const resetAll = () => {
    stopAutoPlay();
    setLogs([]);
    applyStepState(0);
  };

  useEffect(() => {
    resetAll();
    return () => stopAutoPlay();
  }, [mitigation]);

  // Toggle Master via real backend API (/api/v1/admin/redis/master/toggle)
  const handleToggleMaster = async () => {
    setKillingMaster(true);
    addLog('API', 'POST /api/v1/admin/redis/master/toggle -> Gửi lệnh đảo trạng thái Master...', 'warn');
    try {
      const res = await fetch(`${API_BASE}/api/v1/admin/redis/master/toggle`, {
        method: 'POST',
      });
      const data = await res.json();
      if (res.ok) {
        setRealMasterActive(data.active);
        setMasterAlive(data.active);
        addLog('API', `Backend phản hồi: ${data.message}`, 'success');
      } else {
        addLog('API', `Lỗi: ${data.error}`, 'error');
      }
    } catch {
      // Local fallback if API is not yet available
      const nextState = !masterAlive;
      setMasterAlive(nextState);
      setRealMasterActive(nextState);
      addLog('API', `Mô phỏng cục bộ: Master ${nextState ? 'ONLINE' : 'PAUSED'}`, 'warn');
      if (!nextState && currentStep < 3) applyStepState(3);
    } finally {
      setKillingMaster(false);
    }
  };

  const phaseNames = [
    'Khởi tạo (Master: 1, Replica: 1)',
    'Khách A mua vé cuối (Master = 0)',
    mitigation ? 'Đồng bộ WAIT 1 sang Replica' : 'Sync bất đồng bộ (chưa tới nơi)',
    'Master bị sập nguồn / Crash!',
    'Sentinel bầu Replica -> Master mới',
    'Khách B mua vé & Đối chiếu kết quả',
  ];

  return (
    <div className="space-y-6">
      {/* Header bar */}
      <div className="flex flex-col md:flex-row md:items-center justify-between gap-4 p-5 rounded-2xl border border-rose-500/30 bg-gradient-to-r from-rose-950/40 via-slate-900 to-slate-900">
        <div>
          <div className="flex items-center gap-2">
            <span className="px-2.5 py-0.5 rounded-full text-xs font-bold bg-rose-500/20 text-rose-400 border border-rose-500/40">
              Edge Case 1: Failover Animation
            </span>
            <span className="text-xs text-slate-400">Trực quan hóa luồng dữ liệu & sập Master</span>
          </div>
          <h3 className="text-xl font-bold text-white mt-1">
            Mô Phỏng Trực Tiếp Master Crash & Bầu Cử Sentinel
          </h3>
          <p className="text-slate-400 text-xs mt-1">
            Dùng 2 nút mũi tên &larr; &rarr; để chạy từng bước, hoặc bấm nút Toggle để bật/tắt container Master thực tế.
          </p>
        </div>

        {/* Action controls */}
        <div className="flex items-center gap-2 flex-wrap">
          {/* Live Status Badge */}
          <div className="flex items-center gap-1.5 px-3 py-1.5 rounded-xl border border-slate-800 bg-slate-950 text-xs font-mono">
            <span className="text-slate-400 text-[11px]">Trạng Thái Thực Tế:</span>
            {realMasterActive === null ? (
              <span className="text-slate-500 text-[11px]">Đang check...</span>
            ) : realMasterActive ? (
              <span className="flex items-center gap-1 text-emerald-400 font-bold text-[11px]">
                <span className="w-2 h-2 rounded-full bg-emerald-400 animate-ping" />
                ONLINE (6379)
              </span>
            ) : (
              <span className="flex items-center gap-1 text-rose-400 font-bold text-[11px]">
                <span className="w-2 h-2 rounded-full bg-rose-500" />
                PAUSED (TẮT)
              </span>
            )}
          </div>

          <button
            onClick={() => setMitigation(!mitigation)}
            className={`px-3.5 py-2 rounded-xl text-xs font-bold border transition-all flex items-center gap-1.5 ${
              mitigation
                ? 'bg-emerald-600 border-emerald-500 text-white shadow-lg shadow-emerald-600/30'
                : 'bg-slate-800 border-slate-700 text-slate-300 hover:text-white'
            }`}
          >
            <ShieldCheck className="w-4 h-4" />
            {mitigation ? 'WAIT 1: ĐÃ BẬT' : 'Bật WAIT 1 (Đồng bộ)'}
          </button>

          {/* Step Back button */}
          <button
            onClick={handlePrevStep}
            disabled={currentStep <= 0 || isAutoPlaying}
            className="p-2 rounded-xl bg-slate-800 hover:bg-slate-700 disabled:opacity-30 text-white text-xs font-bold flex items-center transition-all border border-slate-700"
            title="Bước trước đó"
          >
            <ChevronLeft className="w-4 h-4" />
          </button>

          {/* Current Step Indicator Badge */}
          <span className="px-3 py-1.5 rounded-xl bg-slate-950 border border-slate-800 font-mono text-xs text-indigo-400 font-bold min-w-[70px] text-center">
            {currentStep}/5
          </span>

          {/* Step Forward button */}
          <button
            onClick={handleNextStep}
            disabled={currentStep >= 5 || isAutoPlaying}
            className="p-2 rounded-xl bg-slate-800 hover:bg-slate-700 disabled:opacity-30 text-white text-xs font-bold flex items-center transition-all border border-slate-700"
            title="Bước tiếp theo"
          >
            <ChevronRight className="w-4 h-4" />
          </button>

          {/* Auto Play / Pause */}
          <button
            onClick={handleToggleAutoPlay}
            className={`px-3.5 py-2 rounded-xl text-xs font-bold flex items-center gap-1.5 shadow-md transition-all ${
              isAutoPlaying
                ? 'bg-amber-600 hover:bg-amber-500 text-white shadow-amber-600/30'
                : 'bg-indigo-600 hover:bg-indigo-500 text-white shadow-indigo-600/30'
            }`}
          >
            {isAutoPlaying ? (
              <>
                <Pause className="w-3.5 h-3.5 fill-current" /> Tạm dừng
              </>
            ) : (
              <>
                <Play className="w-3.5 h-3.5 fill-current" /> Tự động chạy
              </>
            )}
          </button>

          {/* Real Master Toggle API Button */}
          <button
            onClick={handleToggleMaster}
            disabled={killingMaster}
            className={`px-3.5 py-2 rounded-xl text-xs font-bold flex items-center gap-1.5 shadow-md transition-all ${
              realMasterActive === false
                ? 'bg-emerald-600 hover:bg-emerald-500 text-white shadow-emerald-600/30'
                : 'bg-rose-600 hover:bg-rose-500 text-white shadow-rose-600/30'
            }`}
            title="Gửi POST /api/v1/admin/redis/master/toggle"
          >
            <Power className="w-3.5 h-3.5" />
            {killingMaster
              ? 'Đang gọi API...'
              : realMasterActive === false
              ? 'Bật lại Master (Unpause)'
              : 'Tắt Master (Pause)'}
          </button>

          {/* Reset button */}
          <button
            onClick={resetAll}
            className="p-2 rounded-xl bg-slate-800 hover:bg-slate-700 text-slate-400 hover:text-white transition-all border border-slate-700"
            title="Reset"
          >
            <RotateCcw className="w-4 h-4" />
          </button>
        </div>
      </div>

      {/* Main Grid: Left Animation Canvas (3 cols) | Right Terminal Log (2 cols) */}
      <div className="grid grid-cols-1 lg:grid-cols-5 gap-6">
        {/* Left: Animation Canvas */}
        <div className="lg:col-span-3 p-6 rounded-2xl border border-slate-800 bg-slate-950 flex flex-col justify-between min-h-[440px] relative overflow-hidden">
          {/* Background Grid Accent */}
          <div className="absolute inset-0 bg-[radial-gradient(#1e293b_1px,transparent_1px)] [background-size:16px_16px] opacity-40 pointer-events-none" />

          {/* Top: Current Step Title Banner */}
          <div className="z-10 pb-2 border-b border-slate-800/80 flex items-center justify-between text-xs">
            <span className="text-slate-400">
              Trạng thái:{' '}
              <strong className="text-indigo-400 font-mono font-bold">
                Bước {currentStep}: {phaseNames[currentStep]}
              </strong>
            </span>
            <span className="font-mono text-[11px] text-slate-500">
              {currentStep === 5 ? (mitigation ? 'AN TOÀN' : 'BÁN ÂM') : 'ĐANG CHẠY'}
            </span>
          </div>

          {/* Clients & Sentinel Row */}
          <div className="flex justify-between items-center z-10 pt-4">
            {/* User A */}
            <div
              className={`p-3.5 rounded-xl border transition-all ${
                currentStep === 1
                  ? 'border-indigo-500 bg-indigo-950/60 shadow-lg shadow-indigo-500/30 scale-105'
                  : 'border-slate-800 bg-slate-900/80'
              }`}
            >
              <div className="flex items-center gap-2">
                <div className="p-2 rounded-lg bg-indigo-600 text-white">
                  <User className="w-4 h-4" />
                </div>
                <div>
                  <div className="text-xs font-bold text-white">Khách Hàng A</div>
                  <div className="text-[10px] text-slate-400">Mua vé lúc T=0s</div>
                </div>
              </div>
              {currentStep === 1 && (
                <div className="mt-2 text-[10px] font-mono text-indigo-300 flex items-center gap-1">
                  <span className="w-2 h-2 rounded-full bg-indigo-400 animate-ping" />
                  Đang gửi request...
                </div>
              )}
            </div>

            {/* Sentinel in Center */}
            <div
              className={`p-3 rounded-xl border text-center transition-all ${
                currentStep === 4
                  ? 'border-amber-500 bg-amber-950/60 shadow-lg shadow-amber-500/30 animate-pulse'
                  : 'border-slate-800 bg-slate-900/80'
              }`}
            >
              <div className="flex items-center gap-1.5 text-xs font-bold text-amber-400 justify-center">
                <Radio className="w-3.5 h-3.5" /> Redis Sentinel
              </div>
              <div className="text-[10px] text-slate-400 mt-0.5">Giám sát Failover (26379)</div>
              {currentStep === 4 && (
                <div className="text-[10px] font-mono text-amber-300 mt-1 font-bold">
                  Bầu Replica -&gt; Master!
                </div>
              )}
            </div>

            {/* User B */}
            <div
              className={`p-3.5 rounded-xl border transition-all ${
                currentStep === 5
                  ? 'border-emerald-500 bg-emerald-950/60 shadow-lg shadow-emerald-500/30 scale-105'
                  : 'border-slate-800 bg-slate-900/80'
              }`}
            >
              <div className="flex items-center gap-2">
                <div className="p-2 rounded-lg bg-emerald-600 text-white">
                  <User className="w-4 h-4" />
                </div>
                <div>
                  <div className="text-xs font-bold text-white">Khách Hàng B</div>
                  <div className="text-[10px] text-slate-400">Mua vé lúc T=2s</div>
                </div>
              </div>
              {currentStep === 5 && (
                <div className="mt-2 text-[10px] font-mono text-emerald-300 flex items-center gap-1">
                  <span className="w-2 h-2 rounded-full bg-emerald-400 animate-ping" />
                  Gửi request tới Master mới...
                </div>
              )}
            </div>
          </div>

          {/* Animated SVG Traffic Data Flow */}
          <div className="my-6 relative h-28 flex items-center justify-center z-10">
            <svg className="w-full h-full" viewBox="0 0 400 100" fill="none">
              {/* Path A -> Master */}
              <path
                d="M 60 10 C 60 60, 100 80, 100 90"
                stroke={currentStep === 1 ? '#6366f1' : '#334155'}
                strokeWidth="2"
                strokeDasharray={currentStep === 1 ? '4 4' : 'none'}
                className={currentStep === 1 ? 'animate-pulse' : ''}
              />

              {/* Path Master -> Replica (Replication stream) */}
              <path
                d="M 140 90 L 260 90"
                stroke={
                  !masterAlive
                    ? '#ef4444'
                    : currentStep === 2
                    ? mitigation
                      ? '#10b981'
                      : '#f59e0b'
                    : '#334155'
                }
                strokeWidth={currentStep === 2 ? '3' : '2'}
                strokeDasharray={currentStep === 2 ? '6 4' : 'none'}
              />

              {/* Path B -> Replica */}
              <path
                d="M 340 10 C 340 60, 300 80, 300 90"
                stroke={currentStep === 5 ? '#10b981' : '#334155'}
                strokeWidth="2"
                strokeDasharray={currentStep === 5 ? '4 4' : 'none'}
                className={currentStep === 5 ? 'animate-pulse' : ''}
              />

              {/* Moving Pulse Dot for User A */}
              {currentStep === 1 && (
                <circle cx="80" cy="50" r="4" fill="#6366f1">
                  <animate attributeName="cy" values="10;90" dur="1s" repeatCount="indefinite" />
                  <animate attributeName="cx" values="60;100" dur="1s" repeatCount="indefinite" />
                </circle>
              )}

              {/* Moving Pulse Dot for Replication */}
              {currentStep === 2 && masterAlive && (
                <circle cx="200" cy="90" r="4" fill={mitigation ? '#10b981' : '#f59e0b'}>
                  <animate attributeName="cx" values="140;260" dur="0.8s" repeatCount="indefinite" />
                </circle>
              )}

              {/* Moving Pulse Dot for User B */}
              {currentStep === 5 && (
                <circle cx="320" cy="50" r="4" fill="#10b981">
                  <animate attributeName="cy" values="10;90" dur="1s" repeatCount="indefinite" />
                  <animate attributeName="cx" values="340;300" dur="1s" repeatCount="indefinite" />
                </circle>
              )}
            </svg>

            {/* Cut line overlay if master crashed during sync */}
            {currentStep >= 3 && !mitigation && (
              <div className="absolute inset-0 flex items-center justify-center">
                <span className="px-2.5 py-1 rounded bg-rose-950/90 border border-rose-600 text-rose-400 font-mono text-[10px] font-bold flex items-center gap-1 animate-bounce">
                  <Flame className="w-3 h-3" /> GÓI SYNC BỊ ĐỨT ĐOẠN!
                </span>
              </div>
            )}
          </div>

          {/* Bottom: Redis Master & Replica */}
          <div className="grid grid-cols-2 gap-6 z-10">
            {/* Redis Master */}
            <div
              className={`p-4 rounded-xl border transition-all ${
                !masterAlive
                  ? 'border-rose-700 bg-rose-950/40 opacity-70'
                  : 'border-indigo-500/80 bg-slate-900 shadow-md shadow-indigo-500/20'
              }`}
            >
              <div className="flex items-center justify-between">
                <span className="font-bold text-xs text-white flex items-center gap-1.5">
                  <Server className="w-4 h-4 text-indigo-400" />
                  Redis Master (6379)
                </span>
                <span
                  className={`text-[9px] font-bold px-2 py-0.5 rounded-full ${
                    !masterAlive ? 'bg-rose-950 text-rose-400 border border-rose-700' : 'bg-emerald-950 text-emerald-400'
                  }`}
                >
                  {!masterAlive ? 'CRASHED' : 'ACTIVE'}
                </span>
              </div>

              <div className="mt-3 flex items-baseline justify-between">
                <span className="text-[11px] text-slate-400">Kho vé RAM:</span>
                <span className="font-mono font-extrabold text-lg text-white">
                  {masterStock} vé
                </span>
              </div>
            </div>

            {/* Redis Replica */}
            <div
              className={`p-4 rounded-xl border transition-all ${
                replicaRole === 'master'
                  ? 'border-emerald-500 bg-emerald-950/30 shadow-md shadow-emerald-500/20'
                  : 'border-slate-800 bg-slate-900'
              }`}
            >
              <div className="flex items-center justify-between">
                <span className="font-bold text-xs text-white flex items-center gap-1.5">
                  <Server className="w-4 h-4 text-amber-400" />
                  {replicaRole === 'master' ? 'Redis Master Mới (6380)' : 'Redis Replica (6380)'}
                </span>
                <span
                  className={`text-[9px] font-bold px-2 py-0.5 rounded-full ${
                    replicaRole === 'master'
                      ? 'bg-emerald-950 text-emerald-400 border border-emerald-700'
                      : 'bg-slate-800 text-slate-400'
                  }`}
                >
                  {replicaRole.toUpperCase()}
                </span>
              </div>

              <div className="mt-3 flex items-baseline justify-between">
                <span className="text-[11px] text-slate-400">Kho vé RAM:</span>
                <span
                  className={`font-mono font-extrabold text-lg ${
                    replicaStock === 0 && !mitigation && currentStep === 5
                      ? 'text-rose-400'
                      : 'text-white'
                  }`}
                >
                  {replicaStock} vé
                </span>
              </div>
            </div>
          </div>

          {/* Outcome Banner */}
          {currentStep === 5 && (
            <div
              className={`mt-4 p-3 rounded-xl border text-xs flex items-center gap-2 animate-in fade-in ${
                mitigation
                  ? 'bg-emerald-950/40 border-emerald-700 text-emerald-300'
                  : 'bg-rose-950/50 border-rose-700 text-rose-300'
              }`}
            >
              {mitigation ? (
                <>
                  <CheckCircle2 className="w-4 h-4 text-emerald-400 shrink-0" />
                  <span>
                    <strong>0.00% Bán Âm:</strong> Nhờ WAIT 1, Replica đã nhận tồn kho = 0 trước khi Master sập. Khách B bị từ chối an toàn!
                  </span>
                </>
              ) : (
                <>
                  <AlertTriangle className="w-4 h-4 text-rose-400 shrink-0" />
                  <span>
                    <strong>BÁN ÂM 101%:</strong> Master sập trước khi sync. Replica thăng hạng mang stock = 1 cũ, bán tiếp cho Khách B!
                  </span>
                </>
              )}
            </div>
          )}
        </div>

        {/* Right: Live Terminal Log Console */}
        <div className="lg:col-span-2 rounded-2xl border border-slate-800 bg-slate-950 p-4 flex flex-col h-[440px]">
          <div className="flex items-center justify-between pb-3 border-b border-slate-800">
            <span className="text-xs font-bold text-slate-200 flex items-center gap-1.5">
              <Terminal className="w-4 h-4 text-indigo-400" />
              Log Tổng Hệ Thống (Real-time Console)
            </span>
            <span className="text-[10px] text-slate-500 font-mono">{logs.length} sự kiện</span>
          </div>

          {/* Logs stream */}
          <div className="flex-1 overflow-y-auto space-y-1.5 py-3 font-mono text-[11px] pr-1">
            {logs.length === 0 ? (
              <div className="text-slate-600 text-center py-12 text-xs font-sans">
                Chưa có sự kiện nào. Bấm nút mũi tên hoặc &quot;Tự động chạy&quot;.
              </div>
            ) : (
              logs.map((log) => {
                const badgeColor =
                  log.source === 'MASTER'
                    ? 'text-indigo-400'
                    : log.source === 'REPLICA'
                    ? 'text-amber-400'
                    : log.source === 'SENTINEL'
                    ? 'text-purple-400'
                    : log.source === 'CLIENT'
                    ? 'text-sky-400'
                    : log.source === 'AUDIT'
                    ? 'text-emerald-400'
                    : 'text-slate-400';

                const textColor =
                  log.type === 'error'
                    ? 'text-rose-400 font-semibold'
                    : log.type === 'success'
                    ? 'text-emerald-400'
                    : log.type === 'warn'
                    ? 'text-amber-300'
                    : 'text-slate-300';

                return (
                  <div key={log.id} className="leading-relaxed hover:bg-slate-900/50 px-1.5 py-0.5 rounded">
                    <span className="text-slate-600">[{log.time}]</span>{' '}
                    <span className={`font-bold ${badgeColor}`}>[{log.source}]</span>{' '}
                    <span className={textColor}>{log.text}</span>
                  </div>
                );
              })
            )}
            <div ref={logEndRef} />
          </div>

          {/* Console footer */}
          <div className="pt-2 border-t border-slate-900 flex justify-between items-center text-[10px] text-slate-500 font-mono">
            <span>API Target: /api/v1/admin/redis/master/toggle</span>
            <button
              onClick={() => setLogs([])}
              className="text-slate-400 hover:text-slate-200 underline"
            >
              Clear
            </button>
          </div>
        </div>
      </div>

      {/* Real PostgreSQL Database Audit Inspection Box */}
      {auditResult && (
        <div className="p-5 rounded-2xl border border-slate-800 bg-slate-950 space-y-4 animate-in fade-in slide-in-from-bottom-2 duration-300">
          <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 pb-3 border-b border-slate-800">
            <div className="flex items-center gap-2">
              <div className="p-2 rounded-lg bg-indigo-600/20 text-indigo-400 border border-indigo-500/30">
                <Database className="w-4 h-4" />
              </div>
              <div>
                <h4 className="text-sm font-bold text-white flex items-center gap-2">
                  Kiểm Toán Thực Tế: PostgreSQL 16 (Table: orders)
                  {auditResult.overselling ? (
                    <span className="px-2 py-0.5 rounded text-[10px] font-bold bg-rose-950 text-rose-400 border border-rose-600 animate-pulse">
                      PHÁT HIỆN BÁN ÂM 101%
                    </span>
                  ) : (
                    <span className="px-2 py-0.5 rounded text-[10px] font-bold bg-emerald-950 text-emerald-400 border border-emerald-600">
                      BẢO TOÀN DỮ LIỆU (0.00% BÁN ÂM)
                    </span>
                  )}
                </h4>
                <p className="text-xs text-slate-400">
                  Dữ liệu được SELECT trực tiếp từ cơ sở dữ liệu PostgreSQL 16 thật sau khi chạy kịch bản failover
                </p>
              </div>
            </div>

            <div className="flex items-center gap-3 text-xs font-mono">
              <span className="text-slate-400">
                Tổng đơn ghi nhận:{' '}
                <strong className={auditResult.overselling ? 'text-rose-400 font-bold' : 'text-emerald-400 font-bold'}>
                  {auditResult.total_orders} đơn
                </strong>{' '}
                (Kho gốc: 1 vé)
              </span>
            </div>
          </div>

          {/* Orders Table */}
          <div className="overflow-x-auto rounded-xl border border-slate-800/80">
            <table className="w-full text-left text-xs font-mono">
              <thead className="bg-slate-900/90 text-slate-400 border-b border-slate-800">
                <tr>
                  <th className="py-2.5 px-3">Order ID (UUID)</th>
                  <th className="py-2.5 px-3">User ID</th>
                  <th className="py-2.5 px-3">Trạng Thái</th>
                  <th className="py-2.5 px-3">Thời Điểm Tạo (PostgreSQL)</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-800/60 bg-slate-950">
                {auditResult.orders.map((ord) => (
                  <tr key={ord.id} className="hover:bg-slate-900/40">
                    <td className="py-2.5 px-3 text-indigo-400 font-bold">{ord.id}</td>
                    <td className="py-2.5 px-3">
                      <span
                        className={`px-2 py-0.5 rounded text-[10px] font-bold ${
                          ord.user_id === 'user-A-failover'
                            ? 'bg-indigo-950 text-indigo-300 border border-indigo-700'
                            : 'bg-emerald-950 text-emerald-300 border border-emerald-700'
                        }`}
                      >
                        {ord.user_id}
                      </span>
                    </td>
                    <td className="py-2.5 px-3">
                      <span className="text-amber-400 bg-amber-950/40 border border-amber-800/50 px-2 py-0.5 rounded text-[10px]">
                        {ord.status}
                      </span>
                    </td>
                    <td className="py-2.5 px-3 text-slate-400">
                      {new Date(ord.created_at).toLocaleTimeString('vi-VN')} ({new Date(ord.created_at).toISOString()})
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </div>
  );
}
