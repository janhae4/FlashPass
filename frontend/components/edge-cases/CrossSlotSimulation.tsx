'use client';

import React, { useState } from 'react';
import {
  AlertTriangle,
  CheckCircle2,
  Play,
  RotateCcw,
  ArrowRight,
  ShieldCheck,
  Tag,
  Server,
} from 'lucide-react';

export default function CrossSlotSimulation() {
  const [step, setStep] = useState(0);
  const [mitigationEnabled, setMitigationEnabled] = useState(false);
  const [isRunning, setIsRunning] = useState(false);

  const nextStep = () => {
    if (step < 3) setStep(step + 1);
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
      if (cur >= 3) {
        clearInterval(interval);
        setIsRunning(false);
      }
    }, 1700);
  };

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="p-5 rounded-2xl border border-sky-500/30 bg-gradient-to-r from-sky-950/40 via-slate-900 to-slate-900">
        <div className="flex flex-col md:flex-row md:items-center justify-between gap-4">
          <div>
            <div className="flex items-center gap-2">
              <span className="px-2.5 py-0.5 rounded-full text-xs font-bold bg-sky-500/20 text-sky-400 border border-sky-500/40">
                Case #4: Redis Cluster Cross-Slot Sharding
              </span>
              <span className="text-xs text-slate-400">Rủi ro: Lỗi CROSSSLOT & Mất Tính Nguyên Tử</span>
            </div>
            <h3 className="text-xl font-bold text-white mt-1">
              Phân Vùng Dữ Liệu Sharding Khi Bán Combo Vé Trên Nhiều Node Khác Nhau
            </h3>
            <p className="text-slate-400 text-xs mt-1 max-w-2xl">
              Trong Redis Cluster, dữ liệu chia làm 16,384 slots trên nhiều server. Nếu Lua Script cố trừ cùng lúc 2 key nằm ở 2 slot khác nhau (ví dụ: vé VIP và vé gửi xe), Redis sẽ ném lỗi <code>CROSSSLOT</code> và từ chối chạy!
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
                ? 'Giải pháp: Hash Tag {tag} Co-location (BẬT)'
                : 'Bật Kỹ Thuật Hash Tag {event_id}'}
            </button>
            <span className="text-[11px] text-slate-500">
              {mitigationEnabled ? 'Gom tất cả key về cùng 1 slot' : 'Để tên key tự do ngẫu nhiên'}
            </span>
          </div>
        </div>
      </div>

      {/* Controls */}
      <div className="flex items-center justify-between p-4 rounded-xl border border-slate-800 bg-slate-900/60">
        <div className="flex items-center gap-2">
          <button
            onClick={nextStep}
            disabled={step >= 3 || isRunning}
            className="px-4 py-2 rounded-lg bg-indigo-600 hover:bg-indigo-500 disabled:bg-slate-800 disabled:text-slate-500 text-white font-bold text-xs flex items-center gap-1.5 transition-all"
          >
            Bước tiếp theo ({step}/3) <ArrowRight className="w-3.5 h-3.5" />
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
            {step === 0 && 'Sẵn sàng kiểm tra tính toán Hash Slot'}
            {step === 1 && 'Khách đặt Combo (Vé + Gửi xe)'}
            {step === 2 && 'Redis Cluster tính toán CRC16'}
            {step === 3 && 'Kết quả thực thi Lua Script'}
          </span>
        </div>
      </div>

      {/* Visual Hash Slot Allocation */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
        {/* Item 1: Vé Khán Đài */}
        <div className="p-5 rounded-2xl border border-slate-800 bg-slate-900/80 space-y-3">
          <div className="flex items-center justify-between pb-3 border-b border-slate-800">
            <span className="font-bold text-sm text-white flex items-center gap-2">
              <Tag className="w-4 h-4 text-purple-400" />
              Tài nguyên 1: Vé Khán Đài VIP
            </span>
            <span className="text-xs font-mono text-purple-400 font-bold">
              {mitigationEnabled ? '{event_1}:ticket_vip' : 'ticket_vip:event_1'}
            </span>
          </div>

          <div className="p-3 rounded-xl bg-slate-950/80 border border-slate-800 space-y-2 text-xs">
            <div className="flex justify-between">
              <span className="text-slate-400">Thuật toán băm CRC16:</span>
              <span className="font-mono text-white">
                {step >= 2 ? (mitigationEnabled ? 'CRC16("{event_1}")' : 'CRC16("ticket_vip:event_1")') : '---'}
              </span>
            </div>
            <div className="flex justify-between items-center">
              <span className="text-slate-400">Phân bổ Hash Slot:</span>
              <span className="font-mono font-bold text-emerald-400 text-sm">
                {step >= 2 ? (mitigationEnabled ? 'Slot #4120 (Node 1)' : 'Slot #3210 (Node 1)') : '---'}
              </span>
            </div>
          </div>
        </div>

        {/* Item 2: Vé Gửi Xe */}
        <div className="p-5 rounded-2xl border border-slate-800 bg-slate-900/80 space-y-3">
          <div className="flex items-center justify-between pb-3 border-b border-slate-800">
            <span className="font-bold text-sm text-white flex items-center gap-2">
              <Tag className="w-4 h-4 text-amber-400" />
              Tài nguyên 2: Vé Giữ Xe Ô Tô
            </span>
            <span className="text-xs font-mono text-amber-400 font-bold">
              {mitigationEnabled ? '{event_1}:parking_pass' : 'parking_pass:event_1'}
            </span>
          </div>

          <div className="p-3 rounded-xl bg-slate-950/80 border border-slate-800 space-y-2 text-xs">
            <div className="flex justify-between">
              <span className="text-slate-400">Thuật toán băm CRC16:</span>
              <span className="font-mono text-white">
                {step >= 2 ? (mitigationEnabled ? 'CRC16("{event_1}")' : 'CRC16("parking_pass:event_1")') : '---'}
              </span>
            </div>
            <div className="flex justify-between items-center">
              <span className="text-slate-400">Phân bổ Hash Slot:</span>
              <span
                className={`font-mono font-bold text-sm ${
                  mitigationEnabled ? 'text-emerald-400' : 'text-rose-400'
                }`}
              >
                {step >= 2 ? (mitigationEnabled ? 'Slot #4120 (Node 1)' : 'Slot #12450 (Node 3)') : '---'}
              </span>
            </div>
          </div>
        </div>
      </div>

      {/* Cluster Nodes Mapping Bar */}
      {step >= 2 && (
        <div className="p-5 rounded-2xl border border-slate-800 bg-slate-900/60 space-y-3 animate-in fade-in">
          <div className="text-xs font-bold text-slate-300 flex items-center gap-2">
            <Server className="w-4 h-4 text-indigo-400" />
            Bản Đồ Phân Phối Của Redis Cluster (3 Shard Nodes):
          </div>

          <div className="grid grid-cols-3 gap-3 text-center text-xs">
            <div
              className={`p-3 rounded-xl border ${
                mitigationEnabled
                  ? 'border-emerald-500/80 bg-emerald-950/30 text-emerald-300'
                  : 'border-slate-800 bg-slate-950/60 text-slate-300'
              }`}
            >
              <div className="font-bold">Node 1 (Slots 0 - 5460)</div>
              <div className="font-mono text-[11px] mt-1 text-slate-400">
                {mitigationEnabled
                  ? 'Chứa CẢ HAI Key: VIP + Parking'
                  : 'Chỉ chứa: Vé VIP (Slot #3210)'}
              </div>
            </div>

            <div className="p-3 rounded-xl border border-slate-800 bg-slate-950/40 text-slate-500">
              <div className="font-bold">Node 2 (Slots 5461 - 10922)</div>
              <div className="font-mono text-[11px] mt-1">Không có key nào</div>
            </div>

            <div
              className={`p-3 rounded-xl border ${
                !mitigationEnabled
                  ? 'border-rose-500/80 bg-rose-950/30 text-rose-300'
                  : 'border-slate-800 bg-slate-950/40 text-slate-500'
              }`}
            >
              <div className="font-bold">Node 3 (Slots 10923 - 16383)</div>
              <div className="font-mono text-[11px] mt-1 text-slate-400">
                {!mitigationEnabled ? 'Chứa: Vé Gửi Xe (Slot #12450)' : 'Không có key nào'}
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Outcome Verdict */}
      {step === 3 && (
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
                KẾT QUẢ: LUA SCRIPT THỰC THI NGUYÊN TỬ THÀNH CÔNG TRÊN CÙNG 1 SLOT!
              </>
            ) : (
              <>
                <AlertTriangle className="w-5 h-5 text-rose-400" />
                KẾT QUẢ: REDIS TUNG LỖI CROSSSLOT — TOÀN BỘ GIAO DỊCH BỊ ĐỔ VỠ!
              </>
            )}
          </div>

          <div className="text-xs space-y-1.5 leading-relaxed text-slate-300 font-sans">
            {mitigationEnabled ? (
              <p>
                Bằng cách thêm Hash Tag <code className="text-emerald-400 font-bold">{'{event_1}'}</code> vào đầu tên key, Redis Cluster chỉ tính hash cho chuỗi trong dấu ngoặc nhọn. Cả 2 key lập tức được định tuyến về <strong className="text-white">chính xác cùng 1 Hash Slot (Slot #4120 trên Node 1)</strong>. Lua script được phép chạy nguyên tử, đảm bảo khách nhận đủ cả 2 món hoặc không mất gì cả!
              </p>
            ) : (
              <p>
                Do 2 key nằm ở 2 server vật lý khác nhau (Node 1 và Node 3), Redis báo lỗi <code className="text-rose-400 font-mono">CROSSSLOT Keys in request don&apos;t hash to the same slot</code>. Nếu tách ra thành 2 lệnh độc lập, hệ thống đối mặt nguy cơ: Đã trừ tiền vé concert nhưng vé gửi xe bị hết hàng, dẫn tới giao dịch nửa vời và dữ liệu phân tán bị lệch!
              </p>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
