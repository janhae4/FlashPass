'use client';

import React, { useState } from 'react';
import {
  Ghost,
  CheckCircle2,
  Play,
  RotateCcw,
  ArrowRight,
  ShieldCheck,
  Zap,
  Flame,
  Database,
  Layers,
} from 'lucide-react';

export default function DualWriteSimulation() {
  const [step, setStep] = useState(0);
  const [mitigationEnabled, setMitigationEnabled] = useState(false);
  const [isRunning, setIsRunning] = useState(false);

  const nextStep = () => {
    if (step < 4) setStep(step + 1);
  };

  const resetSimulation = () => {
    setStep(0);
    setIsRunning(false);
  };

  const runAuto = () => {
    resetSimulation();
    setIsRunning(true);
    let cur = 0;
    const interval = setInterval(() => {
      cur++;
      setStep(cur);
      if (cur >= 4) {
        clearInterval(interval);
        setIsRunning(false);
      }
    }, 1700);
  };

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="p-5 rounded-2xl border border-purple-500/30 bg-gradient-to-r from-purple-950/40 via-slate-900 to-slate-900">
        <div className="flex flex-col md:flex-row md:items-center justify-between gap-4">
          <div>
            <div className="flex items-center gap-2">
              <span className="px-2.5 py-0.5 rounded-full text-xs font-bold bg-purple-500/20 text-purple-400 border border-purple-500/40">
                Case #3: Dual-Write Partial Failure
              </span>
              <span className="text-xs text-slate-400">Rủi ro: Vé Ma (Ghost Inventory) & Lệch Kho</span>
            </div>
            <h3 className="text-xl font-bold text-white mt-1">
              Redis Đã Trừ Vé Thành Công Nhưng Server Sập Trước Khi Kịp Bắn RabbitMQ
            </h3>
            <p className="text-slate-400 text-xs mt-1 max-w-2xl">
              Thao tác trừ kho trong Redis và đẩy đơn vào RabbitMQ là 2 hệ thống độc lập (Dual-Write). Nếu server bị crash đúng tích tắc giữa 2 thao tác này, vé trong Redis bị trừ mất nhưng không có đơn hàng nào được tạo!
            </p>
          </div>

          <div className="flex flex-col items-end gap-2 shrink-0">
            <button
              onClick={() => {
                setMitigationEnabled(!mitigationEnabled);
                resetSimulation();
              }}
              className={`px-4 py-2 rounded-xl text-xs font-bold border transition-all flex items-center gap-2 ${
                mitigationEnabled
                  ? 'bg-emerald-600 border-emerald-500 text-white shadow-lg shadow-emerald-600/30'
                  : 'bg-slate-800 border-slate-700 text-slate-300 hover:text-white'
              }`}
            >
              <ShieldCheck className="w-4 h-4" />
              {mitigationEnabled
                ? 'Giải pháp: Stock Auditor & Outbox (ĐÃ BẬT)'
                : 'Bật Giải Pháp Tự Động Bù Kho (Auditor)'}
            </button>
            <span className="text-[11px] text-slate-500">
              {mitigationEnabled ? 'Tự động kiểm toán đối chiếu kho' : 'Không có cơ chế tự phục hồi'}
            </span>
          </div>
        </div>
      </div>

      {/* Controls */}
      <div className="flex items-center justify-between p-4 rounded-xl border border-slate-800 bg-slate-900/60">
        <div className="flex items-center gap-2">
          <button
            onClick={nextStep}
            disabled={step >= 4 || isRunning}
            className="px-4 py-2 rounded-lg bg-indigo-600 hover:bg-indigo-500 disabled:bg-slate-800 disabled:text-slate-500 text-white font-bold text-xs flex items-center gap-1.5 transition-all"
          >
            Bước tiếp theo ({step}/4) <ArrowRight className="w-3.5 h-3.5" />
          </button>
          <button
            onClick={runAuto}
            disabled={isRunning}
            className="px-4 py-2 rounded-lg bg-slate-800 hover:bg-slate-700 disabled:opacity-50 text-slate-300 font-bold text-xs flex items-center gap-1.5 transition-all"
          >
            <Play className="w-3.5 h-3.5 fill-current" /> Tự động chạy
          </button>
          <button
            onClick={resetSimulation}
            className="px-3 py-2 rounded-lg bg-slate-800/60 hover:bg-slate-800 text-slate-400 text-xs flex items-center gap-1 transition-all"
          >
            <RotateCcw className="w-3.5 h-3.5" /> Reset
          </button>
        </div>

        <div className="text-xs font-mono text-slate-400">
          Trạng thái:{' '}
          <span className="text-white font-bold">
            {step === 0 && 'Kho ban đầu: 10 vé, 0 đơn'}
            {step === 1 && 'Khách gửi request mua vé'}
            {step === 2 && 'Redis Lua trừ kho: 10 -> 9 vé'}
            {step === 3 && 'CRASH! Server chết trước khi publish RabbitMQ'}
            {step === 4 && 'Kết quả kiểm toán đối chiếu'}
          </span>
        </div>
      </div>

      {/* Topology Stage */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
        {/* Redis Node */}
        <div className="p-5 rounded-2xl border border-slate-800 bg-slate-900/80 space-y-3">
          <div className="flex items-center justify-between pb-3 border-b border-slate-800">
            <span className="font-bold text-sm text-white flex items-center gap-2">
              <Zap className="w-4 h-4 text-emerald-400" />
              Redis (RAM Cache)
            </span>
            <span className="text-[10px] font-bold px-2 py-0.5 rounded-full bg-emerald-950 text-emerald-400 border border-emerald-800">
              Online
            </span>
          </div>

          <div className="py-2 space-y-2 text-xs">
            <div className="flex justify-between">
              <span className="text-slate-400">Kho vé còn lại:</span>
              <span className="font-mono text-xl font-bold text-white">
                {step < 2 ? '10 vé' : step === 4 && mitigationEnabled ? '10 vé (Đã bù kho)' : '9 vé'}
              </span>
            </div>

            <div className="p-3 rounded-xl bg-slate-950/80 border border-slate-800 text-[11px] font-mono space-y-1">
              <div className="text-slate-500">{'// Log Redis:'}</div>
              {step >= 2 && <div className="text-emerald-400">&gt; DECRBY stock 1 -&gt; 9 (Thành công)</div>}
              {step === 4 && mitigationEnabled && (
                <div className="text-indigo-400">&gt; Auditor: INCRBY stock 1 -&gt; Bù lại 10 vé!</div>
              )}
            </div>
          </div>
        </div>

        {/* Go Server Ingestion */}
        <div
          className={`p-5 rounded-2xl border transition-all ${
            step >= 3
              ? 'border-rose-600/80 bg-rose-950/20'
              : 'border-slate-800 bg-slate-900/80'
          }`}
        >
          <div className="flex items-center justify-between pb-3 border-b border-slate-800">
            <span className="font-bold text-sm text-white flex items-center gap-2">
              <Layers className="w-4 h-4 text-indigo-400" />
              Go Fiber API Gateway
            </span>
            <span
              className={`text-[10px] font-bold px-2 py-0.5 rounded-full ${
                step >= 3
                  ? 'bg-rose-950 text-rose-400 border border-rose-800'
                  : 'bg-emerald-950 text-emerald-400 border border-emerald-800'
              }`}
            >
              {step >= 3 ? 'KILLED / CRASH' : 'Online'}
            </span>
          </div>

          <div className="py-2 space-y-2 text-xs">
            <div className="flex justify-between">
              <span className="text-slate-400">Trạng thái tiến trình:</span>
              <span className="font-mono font-bold text-white">
                {step < 3 ? 'Đang thực thi request' : 'Chết đột ngột (OOM Killer)'}
              </span>
            </div>

            <div className="p-3 rounded-xl bg-slate-950/80 border border-slate-800 text-[11px] font-mono space-y-1">
              <div className="text-slate-500">{'// Nhật ký Gateway:'}</div>
              {step >= 2 && <div className="text-emerald-400">&gt; Bước 1: Trừ Redis xong.</div>}
              {step >= 3 && (
                <div className="text-rose-400 font-bold flex items-center gap-1">
                  <Flame className="w-3 h-3" />
                  &gt; SẬP NGUỒN trước khi kịp gọi amqp.Publish!
                </div>
              )}
            </div>
          </div>
        </div>

        {/* RabbitMQ & PostgreSQL */}
        <div className="p-5 rounded-2xl border border-slate-800 bg-slate-900/80 space-y-3">
          <div className="flex items-center justify-between pb-3 border-b border-slate-800">
            <span className="font-bold text-sm text-white flex items-center gap-2">
              <Database className="w-4 h-4 text-amber-400" />
              RabbitMQ & PostgreSQL
            </span>
            <span className="text-[10px] font-bold px-2 py-0.5 rounded-full bg-slate-800 text-slate-300">
              Persistent
            </span>
          </div>

          <div className="py-2 space-y-2 text-xs">
            <div className="flex justify-between">
              <span className="text-slate-400">Đơn hàng trong DB:</span>
              <span className="font-mono text-xl font-bold text-slate-400">
                0 đơn hàng
              </span>
            </div>

            <div className="p-3 rounded-xl bg-slate-950/80 border border-slate-800 text-[11px] font-mono space-y-1">
              <div className="text-slate-500">{'// Log Queue & DB:'}</div>
              <div className="text-slate-600">&gt; Hàng đợi RabbitMQ: 0 messages.</div>
              {step >= 3 && (
                <div className="text-amber-400">
                  &gt; Không có tin nhắn nào tới được hàng đợi!
                </div>
              )}
            </div>
          </div>
        </div>
      </div>

      {/* Outcome Verdict */}
      {step === 4 && (
        <div
          className={`p-6 rounded-2xl border animate-in fade-in space-y-3 ${
            mitigationEnabled
              ? 'border-emerald-500/50 bg-emerald-950/20 text-emerald-300'
              : 'border-purple-500/60 bg-purple-950/30 text-purple-300'
          }`}
        >
          <div className="flex items-center gap-2 text-base font-bold">
            {mitigationEnabled ? (
              <>
                <CheckCircle2 className="w-5 h-5 text-emerald-400" />
                KẾT QUẢ: TỰ ĐỘNG PHỤC HỒI KHO (AUDITOR RECONCILIATION ĐÃ BÙ 1 VÉ)
              </>
            ) : (
              <>
                <Ghost className="w-5 h-5 text-purple-400" />
                KẾT QUẢ: GHOST INVENTORY (VÉ MA) — KHO MẤT 1 VÉ NHƯNG KHÔNG CÓ DOANH THU!
              </>
            )}
          </div>

          <div className="text-xs space-y-1.5 leading-relaxed text-slate-300 font-sans">
            {mitigationEnabled ? (
              <p>
                Nhờ tiến trình <strong className="text-white">Kiểm toán kho tự động (Stock Auditor Cron)</strong>: Hệ thống định kỳ chạy công thức đối chiếu <code className="text-emerald-400 font-mono">Kho Redis (9) + Đơn trong DB (0) != Tổng vé sự kiện (10)</code>. Phát hiện bị lệch 1 vé do lỗi mạng, Auditor tự động gọi <code className="text-emerald-400 font-mono">INCRBY stock 1</code> bù lại kho thành 10 vé, bảo toàn 100% doanh thu!
              </p>
            ) : (
              <p>
                Kho trong Redis chỉ còn 9 vé, nhưng trong Database có đúng 0 đơn hàng. Người dùng nhận lỗi 500 nghĩ rằng mình chưa mua được vé. Ban tổ chức bị <strong className="text-purple-400">thất thoát 1 vé vĩnh viễn</strong> mà không bán được cho bất kỳ ai!
              </p>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
