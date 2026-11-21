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

## 🛡️ 6 Role Perbankan & Kredensial Demo (Password: `Password#2026`)

| Role | Email Login | Rekening / Identitas | Kapabilitas & Kewenangan |
| :--- | :--- | :--- | :--- |
| **`CUSTOMER`** | `budi.santoso@cbs-bank.co.id`<br>`siti.rahma@cbs-bank.co.id` | `5240301001` (PIN `123456`)<br>`5240302002` (PIN `654321`) | Cek saldo & mutasi rekening sendiri, transfer dana (`INTRABANK`, `BI_FAST`, `RTGS`, `LLG`), bayar Virtual Account. Dilarang melihat rekening nasabah lain (403 Forbidden). |
| **`MERCHANT`** | `merchant@tokonusantara.co.id` | Giro `5240308888`<br>(PIN `888888`) | Kelola giro korporasi, generate tagihan Virtual Account untuk pelanggan, pantau status pembayaran VA. |
| **`TELLER`** | `teller@cbs-bank.co.id` | Staf Cabang Sudirman | Eksekusi Setor Tunai (`DEPOSIT`) & Tarik Tunai (`WITHDRAWAL`) nasabah terhadap GL Kas Utama & Cadangan Likuiditas Bank (`1001000001`). |
| **`CUSTOMER_SERVICE`** | `cs@cbs-bank.co.id` | Staf Cabang Sudirman | Buka blokir PIN rekening yang terkunci akibat 3x salah PIN (`PIN_BLOCKED`), aktivasi/freeze status rekening nasabah. |
| **`AUDITOR`** | `auditor@cbs-bank.co.id` | Pengawas OJK | Akses 100% Read-Only: verifikasi integritas Trial Balance General Ledger (`Total Debit == Total Credit`) dan Regulatory Audit Trail OJK. |
| **`ADMIN`** | `admin@cbs-bank.co.id` | Master Controller | Akses penuh operasional, Sandbox Reset Controller, integrasi SNAP-BI Open Banking partner key. |

---

## ⚙️ Fitur Keamanan & Kepatuhan Perbankan
1. **Double-Entry General Ledger**: Setiap mutasi dana wajib memiliki counterparty posting debit dan credit yang seimbang (`Δ = Rp 0`).
2. **Three-Strike PIN Lockout**: Memasukkan PIN yang salah 3 kali berturut-turut otomatis mengunci rekening ke status `PIN_BLOCKED`.
3. **Card Tier Velocity Limit**: Limit harian transfer otomatis dibatasi sesuai kartu tier nasabah (Silver: Rp 50 Juta, Gold: Rp 100 Juta, Platinum: Rp 250 Juta, Priority: Rp 1 Miliar).
4. **Idempotency Protection**: Header `X-Idempotency-Key` menjamin tidak ada duplikasi pendebetan (double-debit) saat retry jaringan.
