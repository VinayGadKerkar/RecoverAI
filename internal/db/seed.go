package db

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Seed inserts demo merchants and sample failed payments for local development.
func Seed(ctx context.Context, pool *pgxpool.Pool) error {
	slog.Info("seeding merchants...")

	merchants := []struct {
		id         string
		name       string
		email      string
		razorpayID string
	}{
		{"merch_01", "Acme Electronics", "ops@acme.example.com", "acc_acme01"},
		{"merch_02", "QuickMart", "finance@quickmart.example.com", "acc_qm02"},
		{"merch_03", "TravelEasy", "payments@traveleasy.example.com", "acc_te03"},
	}

	for _, m := range merchants {
		_, err := pool.Exec(ctx, `
			INSERT INTO merchants (id, name, email, razorpay_id, is_active)
			VALUES ($1, $2, $3, $4, true)
			ON CONFLICT (id) DO NOTHING
		`, m.id, m.name, m.email, m.razorpayID)
		if err != nil {
			return fmt.Errorf("insert merchant %s: %w", m.id, err)
		}
	}

	slog.Info("seeding sample failed payments...")

	// Sample failed payments covering all UPI error codes
	payments := []struct {
		id         string
		merchantID string
		amount     int64
		currency   string
		status     string
		method     string
		errorCode  string
		bank       string
	}{
		{"pay_u16_01", "merch_01", 49900, "INR", "failed", "upi", "U16", "SBI"},
		{"pay_u30_01", "merch_01", 149900, "INR", "failed", "upi", "U30", "HDFC"},
		{"pay_z9_01", "merch_02", 299900, "INR", "failed", "upi", "Z9", "ICICI"},
		{"pay_u68_01", "merch_02", 99900, "INR", "failed", "upi", "U68", "AXIS"},
		{"pay_rb_01", "merch_03", 4999900, "INR", "failed", "upi", "RB", "SBI"},
		{"pay_yg_01", "merch_03", 9999900, "INR", "failed", "upi", "YG", "HDFC"},
	}

	for _, p := range payments {
		_, err := pool.Exec(ctx, `
			INSERT INTO payments (id, merchant_id, amount, currency, status, method, error_code, bank)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (id) DO NOTHING
		`, p.id, p.merchantID, p.amount, p.currency, p.status, p.method, p.errorCode, p.bank)
		if err != nil {
			return fmt.Errorf("insert payment %s: %w", p.id, err)
		}
	}

	slog.Info("seed complete")
	return nil
}
