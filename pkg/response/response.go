package response

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// Envelope represents the universal JSON response structure for the Core Banking API.
type Envelope struct {
	Status    string      `json:"status"`
	Message   string      `json:"message,omitempty"`
	Data      interface{} `json:"data,omitempty"`
	Meta      interface{} `json:"meta,omitempty"`
	TraceID   string      `json:"trace_id,omitempty"`
	Timestamp string      `json:"timestamp"`
}

// JSON sends a standard JSON response envelope.
func JSON(w http.ResponseWriter, r *http.Request, status int, message string, data interface{}, meta interface{}) {
	w.Header().Set("Content-Type", "application/json")
	traceID := middleware.GetReqID(r.Context())
	if traceID != "" {
		w.Header().Set("X-Request-ID", traceID)
	}
	w.WriteHeader(status)

	env := Envelope{
		Status:    "SUCCESS",
		Message:   message,
		Data:      data,
		Meta:      meta,
		TraceID:   traceID,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	_ = json.NewEncoder(w).Encode(env)
}

// MaskEmail masks PII email addresses according to banking standards (e.g., "budi.santoso@cbs-bank.co.id" -> "b***o@cbs-bank.co.id").
func MaskEmail(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return "***"
	}
	local, domain := parts[0], parts[1]
	if len(local) <= 1 {
		return "*@" + domain
	}
	if len(local) == 2 {
		return string(local[0]) + "*@" + domain
	}
	return string(local[0]) + "***" + string(local[len(local)-1]) + "@" + domain
}

// MaskAccountNumber masks bank account numbers for audit logs (e.g., "5240301001" -> "524***1001").
func MaskAccountNumber(accNo string) string {
	if len(accNo) <= 6 {
		return "***"
	}
	return accNo[:3] + "***" + accNo[len(accNo)-4:]
}

// MaskNIK masks a 16-digit Indonesian National ID (NIK) (e.g., "3171012345670001" -> "3171********0001").
func MaskNIK(nik string) string {
	if len(nik) < 8 {
		return "****************"
	}
	return nik[:4] + "********" + nik[len(nik)-4:]
}

// FormatIDR formats an integer IDR amount into human-readable Rupiah string (e.g., 2500000 -> "Rp 2.500.000").
func FormatIDR(amount int64) string {
	negative := amount < 0
	if negative {
		amount = -amount
	}
	str := fmt.Sprintf("%d", amount)
	n := len(str)
	if n <= 3 {
		if negative {
			return "-Rp " + str
		}
		return "Rp " + str
	}
	var b strings.Builder
	if negative {
		b.WriteString("-Rp ")
	} else {
		b.WriteString("Rp ")
	}
	rem := n % 3
	if rem > 0 {
		b.WriteString(str[:rem])
		if n > rem {
			b.WriteByte('.')
		}
	}
	for i := rem; i < n; i += 3 {
		b.WriteString(str[i : i+3])
		if i+3 < n {
			b.WriteByte('.')
		}
	}
	return b.String()
}
