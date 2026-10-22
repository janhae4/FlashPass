'use client';

import React, { useState } from 'react';
import {
  FlaskConical,
  Server,
  Clock,
  Ghost,
  Network,
  Sparkles,
} from 'lucide-react';
import FailoverSimulation from './FailoverSimulation';
import TimeoutRaceSimulation from './TimeoutRaceSimulation';
import DualWriteSimulation from './DualWriteSimulation';
import CrossSlotSimulation from './CrossSlotSimulation';

export default function EdgeCasesLab() {
  const [activeScenario, setActiveScenario] = useState<
    'failover' | 'timeout' | 'dualwrite' | 'crossslot'
  >('failover');

  const scenarios = [
    {
      id: 'failover',
      title: '1. Async Failover',
      subtitle: 'Master sập trước khi sync',
      icon: Server,
      color: 'rose',
    },
    {
      id: 'timeout',
      title: '2. Timeout vs Webhook',
      subtitle: '2 khách cùng trả tiền 1 vé',
      icon: Clock,
      color: 'amber',
    },
    {
      id: 'dualwrite',
      title: '3. Dual-Write Crash',
      subtitle: 'Vé ma (Ghost Inventory)',
      icon: Ghost,
      color: 'purple',
    },
    {
      id: 'crossslot',
      title: '4. Cluster Cross-Slot',
      subtitle: 'Lỗi chia shard đa key',
      icon: Network,
      color: 'sky',
    },
  ] as const;

  return (
    <div className="space-y-8 animate-in fade-in duration-300">
      {/* Title & Introduction */}
      <div className="flex flex-col md:flex-row md:items-center justify-between gap-4 pb-4 border-b border-slate-800">
        <div>
          <div className="flex items-center gap-2">
            <span className="px-2.5 py-0.5 rounded-full text-xs font-bold bg-indigo-500/20 text-indigo-400 border border-indigo-500/30 flex items-center gap-1.5">
              <FlaskConical className="w-3.5 h-3.5" />
              Distributed Systems Testing Lab
            </span>
            <span className="text-xs text-slate-400">Kiểm thử tương tác trực quan</span>
          </div>
          <h2 className="text-2xl font-bold text-white flex items-center gap-2 mt-1">
            <Sparkles className="w-6 h-6 text-indigo-400" />
            Phòng Thí Nghiệm Lỗi Phân Tán (Distributed Edge Cases)
          </h2>
          <p className="text-slate-400 text-sm mt-1 max-w-3xl">
            Tại sao chỉ có Lua Script là chưa đủ để chống bán âm 100% trong môi trường Production thực tế? Hãy tương tác với 4 cạm bẫy kinh điển dưới đây để thấy nguyên nhân và giải pháp công nghiệp.
          </p>
        </div>
      </div>

      {/* Scenario Navigation Tabs */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
        {scenarios.map((sc) => {
          const Icon = sc.icon;
          const isActive = activeScenario === sc.id;
          return (
            <button
              key={sc.id}
              onClick={() => setActiveScenario(sc.id)}
              className={`p-4 rounded-2xl border text-left transition-all ${
                isActive
                  ? 'bg-gradient-to-b from-slate-900 to-slate-950 border-indigo-500 shadow-lg shadow-indigo-500/20 ring-1 ring-indigo-500/50'
                  : 'bg-slate-900/50 border-slate-800 hover:border-slate-700 hover:bg-slate-900/80 text-slate-400'
              }`}
            >
              <div className="flex items-center gap-2">
                <div
                  className={`p-2 rounded-xl ${
                    isActive ? 'bg-indigo-600 text-white' : 'bg-slate-800 text-slate-400'
                  }`}
                >
                  <Icon className="w-4 h-4" />
                </div>
                <div className="overflow-hidden">
                  <div
                    className={`font-bold text-xs truncate ${
                      isActive ? 'text-white' : 'text-slate-300'
                    }`}
                  >
                    {sc.title}
                  </div>
                  <div className="text-[11px] text-slate-500 truncate mt-0.5">
                    {sc.subtitle}
                  </div>
                </div>
              </div>
            </button>
          );
        })}
      </div>

      {/* Active Scenario Component */}
      <div className="p-6 rounded-2xl border border-slate-800 bg-slate-900/40 shadow-2xl">
        {activeScenario === 'failover' && <FailoverSimulation />}
        {activeScenario === 'timeout' && <TimeoutRaceSimulation />}
        {activeScenario === 'dualwrite' && <DualWriteSimulation />}
        {activeScenario === 'crossslot' && <CrossSlotSimulation />}
      </div>
    </div>
  );
}
