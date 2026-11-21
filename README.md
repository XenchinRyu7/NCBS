# Nusantara Core Banking System (NCBS) — Enterprise Monorepo

Sistem Perbankan Inti (**Core Banking System / CBS**) level Enterprise mengadopsi standar **Double-Entry General Ledger** & **SNAP-BI Open Banking**, diarsiteksikan ulang ke **Rust (Axum + Tokio)** untuk performa tinggi & keselamatan memori, dan dilengkapi frontend portal perbankan **Astro SSR (Server-Side Rendering)** dengan panduan desain **Ethereal UI** (Dark-First Telemetry Canvas).

---

## 🏛️ Struktur Monorepo

```
NCBS/
├── backend/                       # Rust Axum Core Banking Engine
│   ├── Cargo.toml
│   ├── src/
│   │   ├── main.rs                # Entrypoint server (Port 8080)
│   │   ├── lib.rs                 # Core banking library & router
│   │   ├── models.rs              # Domain models (Double-entry, CIF, Rekening, VA)
│   │   ├── repository.rs          # Thread-safe in-memory state & General Ledger
│   │   ├── service.rs             # RBAC, autentikasi JWT, PIN lockout logic
│   │   ├── middleware.rs          # Zero-trust auth & RFC 7807 problem details
│   │   ├── handlers.rs            # SNAP-BI RESTful HTTP API handlers
│   │   └── config.rs              # Konfigurasi environment
│   └── tests/
│       └── integration_tests.rs   # 6 Automated Enterprise Banking Test Suites
├── frontend/                      # Astro SSR Web Application (Port 3000)
│   ├── astro.config.mjs           # Mode: output 'server' (@astrojs/node)
│   ├── tailwind.config.mjs        # Ethereal UI semantic tokens
│   ├── src/
│   │   ├── layouts/Layout.astro   # App shell dengan live GL indicator
│   │   ├── pages/
│   │   │   ├── index.astro        # Telemetry overview & directory 6 role
│   │   │   ├── login.astro        # Multi-role login console 1-click
│   │   │   └── portal.astro       # Interactive banking portal (Customer, Merchant, Teller, CS, Auditor, Admin)
│   │   ├── lib/api.ts             # Typed API client ke Rust backend
│   │   └── styles/globals.css     # Ethereal UI dark telemetry canvas tokens
├── package.json                   # Monorepo orchestration scripts
└── README.md
```

---

## 🚀 Cara Menjalankan & Menguji

### 1. Menjalankan Backend Rust (Port 8080)
```bash
# Menjalankan unit & integration test suite (6/6 passing)
pnpm test:backend
# atau: cargo test --manifest-path backend/Cargo.toml

# Menjalankan server backend
pnpm dev:backend
# atau: cargo run --manifest-path backend/Cargo.toml
```

API Documentation Playground: **`http://localhost:8080/docs`** (Interactive Scalar OpenAPI)

---

### 2. Menjalankan Frontend Astro SSR (Port 3000)
```bash
pnpm dev:frontend
# atau: cd frontend && pnpm dev
```

Buka **`http://localhost:3000`** di browser untuk mengakses antarmuka perbankan Ethereal UI.

---

## 🛡️ Arsitektur Hak Akses & Matriks RBAC (Role-Based Access Control)

Sistem mengimplementasikan prinsip *Least Privilege* dan *Zero-Trust Isolation* di tingkat kernel engine:

| Role | Lingkup Operasional | Domain Kewenangan & Hak Akses | Kebijakan Keamanan (Zero-Trust) |
| :--- | :--- | :--- | :--- |
| **`CUSTOMER`** | Nasabah Retail & Priority | Cek saldo & mutasi rekening pribadi, transfer dana (`INTRABANK`, `BI_FAST`, `RTGS`, `LLG`), eksekusi pembayaran Virtual Account. | Isolasi kepemilikan rekening mutlak. Akses lintas CIF menghasilkan HTTP 403 Forbidden. Dibatasi *card tier velocity limit*. |
| **`MERCHANT`** | Korporasi & Biller VA | Pengelolaan rekening giro korporasi, pembuatan tagihan SNAP-BI Virtual Account untuk pelanggan, monitoring status settlement real-time. | Dibatasi hanya pada portofolio tagihan biller terdaftar. Tidak memiliki akses teller kas. |
| **`TELLER`** | Operasional Kantor Cabang | Eksekusi transaksi fisik kas cabang: Setor Tunai (`DEPOSIT`) dan Tarik Tunai (`WITHDRAWAL`) nasabah terhadap General Ledger Kas Utama & Cadangan Likuiditas (`GL 1001000001`). | Wajib menyertakan otorisasi CIF nasabah. Seluruh jurnal terhubung langsung ke cadangan brankas kas fisik cabang. |
| **`CUSTOMER_SERVICE`** | Layanan Administrasi Cabang | Pembukaan rekening tabungan/giro baru, aktivasi status nasabah, dan pembukaan blokir PIN (`PIN_BLOCKED`) akibat 3x kegagalan input PIN. | Otorisasi berbasis verifikasi identitas (KTP/Biometrik). Tidak memiliki wewenang transfer atau manipulasi kas. |
| **`AUDITOR`** | Pengawas Kepatuhan & OJK | Inspeksi Trial Balance General Ledger (`Total Debit == Total Credit`), pengujian invariansi neraca, dan pemantauan menyeluruh *Regulatory Audit Trail*. | Read-Only mutlak (Zero-Write Policy). Menghasilkan alert ketidaksesuaian neraca jika delta selisih non-nol. |
| **`ADMIN`** | Administrator Sistem & IT Core | Pengawasan menyeluruh telemetri sistem, manajemen konektor SNAP-BI Open Banking, dan monitoring status gateway. | Wajib autentikasi bertingkat. Seluruh aktivitas audit tercatat dalam immutable log pengawasan. |

---

## ⚙️ Fitur Keamanan & Kepatuhan Perbankan
1. **Double-Entry General Ledger**: Setiap mutasi dana wajib memiliki counterparty posting debit dan credit yang seimbang (`Δ = Rp 0`).
2. **Three-Strike PIN Lockout**: Memasukkan PIN yang salah 3 kali berturut-turut otomatis mengunci rekening ke status `PIN_BLOCKED`.
3. **Card Tier Velocity Limit**: Limit harian transfer otomatis dibatasi sesuai kartu tier nasabah (Silver: Rp 50 Juta, Gold: Rp 100 Juta, Platinum: Rp 250 Juta, Priority: Rp 1 Miliar).
4. **Idempotency Protection**: Header `X-Idempotency-Key` menjamin tidak ada duplikasi pendebetan (double-debit) saat retry jaringan.
