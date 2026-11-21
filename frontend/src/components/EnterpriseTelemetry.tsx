import React, { useState } from 'react';
import {
  MultiMetricGrid,
  StatMetricCard,
  ProgressMetricCard,
} from '@etherealui/react';
import {
  ShieldAlert,
  Landmark,
  Scale,
  RefreshCw,
  Building2,
  FileCheck2,
  CheckCircle2,
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
  activeRole = 'TREASURY_OPS',
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

  const cbsMetrics = [
    {
      title: 'Cadangan Kas Likuiditas (GL 1001)',
      value: liquidityFormatted,
      change: '+4.2% MoM',
      trend: 'up' as const,
      subtitle: 'Nostro Settlement Bank Indonesia',
      sparkline: [82, 85, 84, 89, 91, 90, 94, 96, 95, 99],
      accent: 'emerald' as const,
    },
    {
      title: 'Kliring BI-FAST & RTGS Hari Ini',
      value: clearingCount.toLocaleString('id-ID'),
      change: '+18.5% YoY',
      trend: 'up' as const,
      subtitle: 'Throughput: 1.450 tx/sec',
      sparkline: [32, 45, 52, 61, 74, 82, 88, 91, 95, 98],
      accent: 'blue' as const,
    },
    {
      title: 'Invarian Keseimbangan Jurnal',
      value: unbalancedCount === 0 ? '100% SEIMBANG' : `${unbalancedCount} SELISIH!`,
      change: '0.00 Var',
      trend: 'neutral' as const,
      subtitle: 'Σ Debit ≡ Σ Kredit (Zero Variance)',
      sparkline: [100, 100, 100, 100, 100, 100, 100, 100, 100, 100],
      accent: 'emerald' as const,
    },
    {
      title: 'Tingkat Kepatuhan AML / OJK',
      value: 'PASSED (STRICT)',
      change: 'Audit Trail L4',
      trend: 'up' as const,
      subtitle: 'PPATK CTR Threshold > Rp 500 Jt',
      sparkline: [95, 96, 97, 98, 98, 99, 99, 100, 100, 100],
      accent: 'indigo' as const,
    },
  ];

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
              <h2 className="text-sm sm:text-base font-bold text-foreground">
                Control Tower Operasional Inti Perbankan
              </h2>
              <span className="rounded-full bg-emerald-500/10 px-2.5 py-0.5 text-[10px] font-mono font-semibold text-emerald-500 border border-emerald-500/20">
                OJK REGULATED
              </span>
            </div>
            <p className="text-xs text-muted-foreground mt-0.5">
              Konsol telemetri sentral untuk pemantauan cadangan likuiditas, kliring RTGS/BI-FAST, brankas kas fisik cabang, dan buku besar akuntansi.
            </p>
          </div>
        </div>

        <div className="flex items-center gap-2 shrink-0 self-end md:self-center">
          <button
            type="button"
            onClick={handleAuditVerify}
            disabled={isVerifying}
            className="flex items-center gap-1.5 rounded-xl border border-border bg-secondary px-3 py-1.5 text-xs font-semibold text-foreground hover:bg-muted transition cursor-pointer"
          >
            <RefreshCw className={`h-3.5 w-3.5 text-primary ${isVerifying ? 'animate-spin' : ''}`} />
            <span>{isVerifying ? 'Verifikasi...' : 'Verifikasi Ledger Invariant'}</span>
          </button>
        </div>
      </div>

      {/* Official Ethereal UI MultiMetricGrid Block */}
      <MultiMetricGrid metrics={cbsMetrics} />

      {/* Wholesale Banking vs Regulatory SLA Progress Cards */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        <ProgressMetricCard
          title="Kapasitas Likuiditas Bank Indonesia (Giro Wajib Minimum)"
          current="Rp 8.500 M"
          target="Rp 10.000 M (85%)"
          percentage={85}
          statusText="Sesuai ketentuan PBI No. 24/5/PBI/2022 • Rasio Pemenuhan Likuiditas 100%"
          variant="emerald"
        />
        <ProgressMetricCard
          title="Beban Kliring Real-Time (Throughput Harian)"
          current="28.410 Tx"
          target="100.000 Tx Target"
          percentage={28.4}
          statusText="SNAP-BI v2.4 • Rata-rata Latensi 14ms (P99: 38ms)"
          variant="blue"
        />
      </div>
    </div>
  );
};

export default EnterpriseTelemetry;
