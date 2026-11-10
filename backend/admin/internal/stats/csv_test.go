package stats

import (
	"context"
	"crypto/rand"
	"encoding/csv"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPaymentsCSV checks GET /api/payments.csv has the header and amounts in major units. Needs TEST_DATABASE_URL
// migrated by the user service's tests.
func TestPaymentsCSV(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	txn := "txn_csv_" + strings.ToLower(rand.Text()[:8])
	if _, err := pool.Exec(ctx, `insert into paddle_transactions (transaction_id, status, currency_code, subtotal, tax, total,
		earnings, billed_at, paddle_updated_at) values ($1, 'completed', 'usd', 1000, 205, 1205, 1100, now(), now())`, txn); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `delete from paddle_transactions where transaction_id = $1`, txn)
	})

	h := &Handler{model: &model{db: pool}}
	w := httptest.NewRecorder()
	if err := h.paymentsCSV(w, httptest.NewRequest("GET", "/api/payments.csv", nil), ""); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(w.Body).ReadAll()
	if err != nil || len(rows) < 2 || strings.Join(rows[0], ",") != "transaction_id,email,currency,subtotal,tax,total,earnings,status,billed_at" {
		t.Fatalf("csv: %v %q", err, rows)
	}
	for _, row := range rows[1:] {
		if row[0] == txn {
			if strings.Join(row[2:8], ",") != "usd,10.00,2.05,12.05,11.00,completed" {
				t.Fatalf("row: %q", row)
			}
			return
		}
	}
	t.Fatalf("%s missing: %q", txn, rows)
}
