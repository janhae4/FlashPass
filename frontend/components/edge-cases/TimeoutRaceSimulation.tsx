'use client';

import React, { useState } from 'react';
import {
  Clock,
  AlertTriangle,
  CheckCircle2,
  Play,
  RotateCcw,
  ArrowRight,
  ShieldCheck,
  User,
} from 'lucide-react';

export default function TimeoutRaceSimulation() {
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
    }, 1800);
  };

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="p-5 rounded-2xl border border-amber-500/30 bg-gradient-to-r from-amber-950/40 via-slate-900 to-slate-900">
        <div className="flex flex-col md:flex-row md:items-center justify-between gap-4">
          <div>
            <div className="flex items-center gap-2">
              <span className="px-2.5 py-0.5 rounded-full text-xs font-bold bg-amber-500/20 text-amber-400 border border-amber-500/40">
                Case #2: Timeout Worker vs Late Webhook
              </span>
              <span className="text-xs text-slate-400">Rủi ro: 2 Khách Cùng Trả Tiền Cho 1 Vé</span>
            </div>
            <h3 className="text-xl font-bold text-white mt-1">
              Cuộc Đua Tốc Độ Giữa Worker Hủy Đơn Quá Hạn & Webhook Thanh Toán Về Trễ
            </h3>
            <p className="text-slate-400 text-xs mt-1 max-w-2xl">
              Khách A giữ vé 10 phút. Đến đúng 10m00s, Worker nhả vé về kho. 10m01s Webhook ngân hàng mới bắn về báo Khách A đã trừ tiền, trong khi vé vừa nhả đã bị Khách B mua mất!
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
                ? 'Giải pháp: Optimistic Lock + Auto Refund (BẬT)'
                : 'Bật Giải Pháp Khóa Trạng Thái & Hoàn Tiền'}
            </button>
            <span className="text-[11px] text-slate-500">
              {mitigationEnabled ? 'Tự động hoàn tiền nếu webhook trễ' : 'Xử lý ngây thơ (Có rủi ro đền tiền)'}
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
          Mốc thời gian:{' '}
          <span className="text-white font-bold">
            {step === 0 && 'T=00:00 (Khách A đang giữ vé)'}
            {step === 1 && 'T=10:00.000s (Hết hạn 10 phút, Worker nhả vé)'}
            {step === 2 && 'T=10:00.200s (Khách B chộp được vé vừa nhả)'}
            {step === 3 && 'T=10:01.000s (Webhook Ngân hàng báo Khách A trả tiền)'}
            {step === 4 && 'Kết quả đối chiếu cuối cùng'}
          </span>
        </div>
      </div>

      {/* Timeline Visual Cards */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
        {/* Khách A / Cổng Thanh Toán */}
        <div
          className={`p-5 rounded-2xl border transition-all ${
            step >= 3
              ? mitigationEnabled
                ? 'border-indigo-500/80 bg-slate-900'
                : 'border-rose-500/80 bg-rose-950/20'
              : 'border-slate-800 bg-slate-900/60'
          }`}
        >
          <div className="flex items-center justify-between pb-3 border-b border-slate-800">
            <span className="font-bold text-sm text-white flex items-center gap-2">
              <User className="w-4 h-4 text-purple-400" />
              Khách A (Đơn #001)
            </span>
            <span className="text-[10px] font-bold px-2 py-0.5 rounded-full bg-slate-800 text-slate-300">
              {step < 3 ? 'Chờ thanh toán' : 'Đã trừ tiền ngân hàng'}
            </span>
          </div>

          <div className="py-4 space-y-3 text-xs">
            <div className="flex justify-between">
              <span className="text-slate-400">Trạng thái đơn:</span>
              <span className="font-mono font-bold text-white">
                {step === 0 && 'PENDING (Còn 30s)'}
                {step >= 1 && step < 3 && 'EXPIRED (Đã hết hạn)'}
                {step >= 3 && (mitigationEnabled ? 'REFUNDED (Đã hoàn tiền)' : 'PAID (Thành công!)')}
              </span>
            </div>

            <div className="p-3 rounded-xl bg-slate-950/80 border border-slate-800 text-[11px] font-mono space-y-1">
              <div className="text-slate-500">{'// Nhật ký thanh toán:'}</div>
              {step >= 1 && <div className="text-amber-400">&gt; Quá hạn 10 phút.</div>}
              {step >= 3 && (
                <div className={mitigationEnabled ? 'text-indigo-400' : 'text-rose-400 font-bold'}>
                  {mitigationEnabled
                    ? '&gt; Webhook đến muộn. Đơn đã EXPIRED -> Tự động gọi API Refund trả lại tiền!'
                    : '&gt; Webhook báo ĐÃ TRẢ TIỀN! Cấp vé cho Khách A.'}
                </div>
              )}
            </div>
          </div>
        </div>

        {/* RabbitMQ Timeout Worker */}
        <div className="p-5 rounded-2xl border border-slate-800 bg-slate-900/80 space-y-3">
          <div className="flex items-center justify-between pb-3 border-b border-slate-800">
            <span className="font-bold text-sm text-white flex items-center gap-2">
              <Clock className="w-4 h-4 text-amber-400" />
              Timeout Worker
            </span>
            <span className="text-[10px] font-bold px-2 py-0.5 rounded-full bg-amber-950 text-amber-400 border border-amber-800">
              RabbitMQ Delayed
            </span>
          </div>

          <div className="py-2 space-y-3 text-xs">
            <div className="flex justify-between">
              <span className="text-slate-400">Kho Redis hiện tại:</span>
              <span className="font-mono text-lg font-bold text-white">
                {step === 0 ? '0 vé' : step === 1 ? '1 vé (Vừa nhả)' : '0 vé (Bị mua)'}
              </span>
            </div>

            <div className="p-3 rounded-xl bg-slate-950/80 border border-slate-800 text-[11px] font-mono space-y-1">
              <div className="text-slate-500">{'// Nhật ký Worker:'}</div>
              {step >= 1 && (
                <div className="text-amber-400">&gt; 10m00s: Delayed message nổ TTL.</div>
              )}
              {step >= 1 && (
                <div className="text-emerald-400">&gt; Gọi INCRBY stock 1 trả vé về kho!</div>
              )}
            </div>
          </div>
        </div>

        {/* Khách B */}
        <div
          className={`p-5 rounded-2xl border transition-all ${
            step >= 2 ? 'border-emerald-500/80 bg-slate-900' : 'border-slate-800 bg-slate-900/60'
          }`}
        >
          <div className="flex items-center justify-between pb-3 border-b border-slate-800">
            <span className="font-bold text-sm text-white flex items-center gap-2">
              <User className="w-4 h-4 text-emerald-400" />
              Khách B (Đơn #002)
            </span>
            <span className="text-[10px] font-bold px-2 py-0.5 rounded-full bg-slate-800 text-slate-300">
              {step >= 2 ? 'Đã giữ vé thành công' : 'Đang canh vé'}
            </span>
          </div>

          <div className="py-4 space-y-3 text-xs">
            <div className="flex justify-between">
              <span className="text-slate-400">Trạng thái vé Khách B:</span>
              <span className="font-mono font-bold text-emerald-400">
                {step < 2 ? 'Chờ kho có vé' : 'ĐÃ ĐẶT VÉ & THANH TOÁN XONG'}
              </span>
            </div>

            <div className="p-3 rounded-xl bg-slate-950/80 border border-slate-800 text-[11px] font-mono space-y-1">
              <div className="text-slate-500">{'// Nhật ký Khách B:'}</div>
              {step >= 2 && (
                <div className="text-emerald-400">
                  &gt; 10:00.200s: Thấy kho có 1 vé nhả ra -&gt; Lua DECRBY trừ thành công!
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
              : 'border-rose-500/60 bg-rose-950/30 text-rose-300'
          }`}
        >
          <div className="flex items-center gap-2 text-base font-bold">
            {mitigationEnabled ? (
              <>
                <CheckCircle2 className="w-5 h-5 text-emerald-400" />
                KẾT QUẢ: 1 VÉ - 1 CHỦ SỞ HỮU DUY NHẤT (ĐÃ HOÀN TIỀN CHO KHÁCH A)
              </>
            ) : (
              <>
                <AlertTriangle className="w-5 h-5 text-rose-400" />
                KẾT QUẢ: 2 KHÁCH HÀNG CÙNG BỊ TRỪ TIỀN CHO 1 CHIẾC VÉ!
              </>
            )}
          </div>

          <div className="text-xs space-y-1.5 leading-relaxed text-slate-300 font-sans">
            {mitigationEnabled ? (
              <p>
                Nhờ cơ chế <strong className="text-white">State Machine với Optimistic Lock</strong>: Khi Webhook thanh toán của Khách A về muộn, hệ thống phát hiện đơn hàng đã bị đánh dấu là <code className="text-amber-400 font-mono">EXPIRED</code> nên từ chối kích hoạt vé, đồng thời tự động kích hoạt API Hoàn Tiền (Refund) trả lại 100% tiền vào tài khoản cho Khách A. Khách B sở hữu vé hợp lệ.
              </p>
            ) : (
              <p>
                Hệ thống ngây thơ: Cứ thấy Webhook báo thanh toán thành công là chuyển đơn của Khách A thành <code className="text-rose-400 font-mono">PAID</code>, trong khi Khách B cũng đã thanh toán xong cho vé đó. Hai khách hàng cùng cầm vé tới cửa soát vé, gây tranh cãi và thiệt hại uy tín thương hiệu!
              </p>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
