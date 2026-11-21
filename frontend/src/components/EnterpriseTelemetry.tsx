import React, { useState } from 'react';
import {
  Landmark,
  RefreshCw,
  ArrowUpRight,
  TrendingUp,
  ShieldCheck,
} from 'lucide-react';

export interface EnterpriseTelemetryProps {
  liquidityFormatted?: string;
  clearingCount?: number;
  unbalancedCount?: number;
  activeRole?: string;
}

export const EnterpriseTelemetry: React.FC<EnterpriseTelemetryProps> = ({
  liquidityFormatted = 'Rp 99.175.000.000.000',
  clearingCount = 28410,
  unbalancedCount = 0,
}) => {
  const [isVerifying, setIsVerifying] = useState(false);
  const [lastCheck, setLastCheck] = useState('Real-Time Active');

  const handleAuditVerify = () => {
    setIsVerifying(true);
    setTimeout(() => {
      setIsVerifying(false);
      setLastCheck(new Date().toLocaleTimeString('id-ID'));
    }, 600);
  };

  const metrics = [
    {
      title: 'Cadangan Kas Likuiditas (GL 1001)',
      value: liquidityFormatted,
      change: '+4.2% MoM',
      subtitle: 'Nostro Settlement Bank Indonesia',
      sparkline: [82, 85, 84, 89, 91, 90, 94, 96, 95, 99],
    },
    {
      title: 'Kliring BI-FAST & RTGS Hari Ini',
      value: clearingCount.toLocaleString('id-ID'),
      change: '+18.5% YoY',
      subtitle: 'Throughput: 1.450 tx/sec',
      sparkline: [32, 45, 52, 61, 74, 82, 88, 91, 95, 98],
    },
    {
      title: 'Invarian Keseimbangan Jurnal',
      value: unbalancedCount === 0 ? '100% SEIMBANG' : `${unbalancedCount} SELISIH!`,
      change: 'Zero Var',
      subtitle: 'Σ Debit ≡ Σ Kredit (Zero Invariant)',
      sparkline: [100, 100, 100, 100, 100, 100, 100, 100, 100, 100],
    },
    {
      title: 'Tingkat Kepatuhan AML / OJK',
      value: 'PASSED (STRICT)',
      change: 'Audit L4',
      subtitle: 'PPATK CTR Threshold > Rp 500 Jt',
      sparkline: [95, 96, 97, 98, 98, 99, 99, 100, 100, 100],
    },
  ];

  const renderSparkline = (points: number[]) => {
    const min = Math.min(...points);
    const max = Math.max(...points);
    const range = Math.max(1, max - min);
    const width = 100;
    const height = 32;

    const coords = points
      .map((val, i) => {
        const x = (i / Math.max(1, points.length - 1)) * width;
        const y = height - ((val - min) / range) * (height - 8) - 4;
        return `${x.toFixed(1)},${y.toFixed(1)}`;
      })
      .join(' ');

    return (
      <svg
        width={width}
        height={height}
        viewBox={`0 0 ${width} ${height}`}
        className="overflow-visible shrink-0"
      >
        <polyline
          fill="none"
          stroke="var(--primary)"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          points={coords}
        />
      </svg>
    );
  };

  return (
    <div className="space-y-6">
      {/* Top Banner Control Status */}
      <div className="rounded-2xl border border-border bg-card p-5 shadow-sm flex flex-col md:flex-row items-start md:items-center justify-between gap-4">
        <div className="flex items-center gap-3">
          <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-primary text-primary-foreground font-black text-base shadow-sm">
            <Landmark className="h-5 w-5" />
          </div>
          <div>
            <div className="flex items-center gap-2">
              <h2 className="text-sm sm:text-base font-bold text-card-foreground">
                Control Tower Operasional Inti Perbankan
              </h2>
              <span className="rounded-full bg-primary/10 px-2.5 py-0.5 text-[10px] font-mono font-semibold text-primary border border-primary/20">
                OJK REGULATED
              </span>
            </div>
            <p className="text-xs text-muted-foreground mt-0.5">
              Konsol telemetri sentral untuk cadangan likuiditas, kliring RTGS/BI-FAST, brankas kas cabang, dan buku besar akuntansi.
            </p>
          </div>
        </div>

        <div className="flex items-center gap-2 shrink-0 self-end md:self-center">
          <button
            type="button"
            onClick={handleAuditVerify}
            disabled={isVerifying}
            className="flex items-center gap-1.5 rounded-xl border border-border bg-card px-3 py-1.5 text-xs font-semibold text-card-foreground hover:bg-muted transition cursor-pointer shadow-sm"
          >
            <RefreshCw className={`h-3.5 w-3.5 text-primary ${isVerifying ? 'animate-spin' : ''}`} />
            <span>{isVerifying ? 'Verifikasi...' : 'Verifikasi Ledger Invariant'}</span>
          </button>
        </div>
      </div>

      {/* Strict Semantic Ethereal Metric Grid (Kebal Ghost Field & Contrast Inversion) */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
        {metrics.map((m) => (
          <div
            key={m.title}
            className="rounded-xl border border-border bg-card p-5 shadow-sm transition-colors space-y-3"
          >
            <div className="flex items-center justify-between">
              <span className="text-xs font-medium text-muted-foreground">{m.title}</span>
              <span className="inline-flex items-center gap-1 rounded-md bg-primary/10 px-2 py-0.5 text-[10px] font-mono font-bold text-primary border border-primary/20">
                <ArrowUpRight className="h-3 w-3" />
                {m.change}
              </span>
            </div>

            <div className="flex items-end justify-between gap-4">
              <div>
                <div className="text-xl sm:text-2xl font-bold tracking-tight text-card-foreground font-mono">
                  {m.value}
                </div>
                <p className="text-[11px] text-muted-foreground mt-0.5">{m.subtitle}</p>
              </div>
              {renderSparkline(m.sparkline)}
            </div>
          </div>
        ))}
      </div>

      {/* Wholesale Banking vs Regulatory SLA Progress Cards */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        <div className="rounded-xl border border-border bg-card p-5 space-y-3 shadow-sm">
          <div className="flex items-center justify-between">
            <span className="text-xs font-semibold text-card-foreground">
              Kapasitas Likuiditas Bank Indonesia (Giro Wajib Minimum)
            </span>
            <span className="text-xs font-mono font-bold text-primary">85%</span>
          </div>
          <div className="h-2 w-full rounded-full bg-muted overflow-hidden">
            <div
              className="h-full rounded-full bg-primary transition-all duration-500"
              style={{ width: '85%' }}
            />
          </div>
          <div className="flex items-center justify-between text-[11px] text-muted-foreground font-mono">
            <span>Rp 8.500 M</span>
            <span>Target: Rp 10.000 M</span>
          </div>
          <p className="text-[10px] text-muted-foreground">
            Sesuai ketentuan PBI No. 24/5/PBI/2022 • Rasio Pemenuhan Likuiditas 100%
          </p>
        </div>

        <div className="rounded-xl border border-border bg-card p-5 space-y-3 shadow-sm">
          <div className="flex items-center justify-between">
            <span className="text-xs font-semibold text-card-foreground">
              Beban Kliring Real-Time (Throughput Harian)
            </span>
            <span className="text-xs font-mono font-bold text-primary">28.4%</span>
          </div>
          <div className="h-2 w-full rounded-full bg-muted overflow-hidden">
            <div
              className="h-full rounded-full bg-primary transition-all duration-500"
              style={{ width: '28.4%' }}
            />
          </div>
          <div className="flex items-center justify-between text-[11px] text-muted-foreground font-mono">
            <span>28.410 Tx</span>
            <span>Target: 100.000 Tx</span>
          </div>
          <p className="text-[10px] text-muted-foreground">
            SNAP-BI v2.4 • Rata-rata Latensi 14ms (P99: 38ms)
          </p>
        </div>
      </div>
    </div>
  );
};

export default EnterpriseTelemetry;
