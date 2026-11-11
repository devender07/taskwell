package stats

import (
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/admin/internal/audit"
	"github.com/devender07/taskwell/backend/admin/internal/web"
)

type Handler struct {
	model *model
	auth  web.Authenticator
}

func New(db *pgxpool.Pool, auth web.Authenticator) *Handler {
	return &Handler{model: &model{db: db}, auth: auth}
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request, _ string) error {
	s, err := h.model.stats(r.Context())
	if err != nil {
		return err
	}
	web.WriteJSON(w, http.StatusOK, s)
	return nil
}

// major formats a minor-unit amount with 2 decimals (1200 is 12.00).
func major(n int64) string { return strconv.FormatFloat(float64(n)/100, 'f', 2, 64) }

// paymentsCSV downloads every Paddle transaction, recorded in admin_audit.
func (h *Handler) paymentsCSV(w http.ResponseWriter, r *http.Request, adminID string) error {
	ps, err := h.model.payments(r.Context())
	if err != nil {
		return err
	}
	if err := audit.Record(r.Context(), h.model.db, adminID, audit.PaymentsExported, "", web.ClientIP(r)); err != nil {
		return err
	}
	rows := make([][]string, len(ps))
	for i, p := range ps {
		earnings := ""
		if p.Earnings != nil {
			earnings = major(*p.Earnings)
		}
		rows[i] = []string{p.TransactionID, p.Email, p.Currency, major(p.Subtotal), major(p.Tax), major(p.Total), earnings,
			p.Status, p.BilledAt.UTC().Format(time.RFC3339)}
	}
	web.WriteCSV(w, "payments.csv", []string{"transaction_id", "email", "currency", "subtotal", "tax", "total", "earnings",
		"status", "billed_at"}, rows)
	return nil
}
