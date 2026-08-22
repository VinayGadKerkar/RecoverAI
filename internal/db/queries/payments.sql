-- name: GetPaymentByID :one
SELECT * FROM payments WHERE id = $1 LIMIT 1;

-- name: CreatePayment :one
INSERT INTO payments (
    id, merchant_id, order_id, amount, currency, status, method,
    error_code, error_description, error_source, error_step, error_reason,
    bank, vpa, card_id, email, contact, description, razorpay_created_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19
)
ON CONFLICT (id) DO UPDATE SET
    status        = EXCLUDED.status,
    error_code    = EXCLUDED.error_code,
    updated_at    = NOW()
RETURNING *;

-- name: UpdatePaymentStatus :one
UPDATE payments SET status = $2, updated_at = NOW() WHERE id = $1 RETURNING *;

-- name: ListFailedPaymentsByMerchant :many
SELECT * FROM payments
WHERE merchant_id = $1 AND status = 'failed'
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: CountFailedPaymentsByMerchant :one
SELECT COUNT(*) FROM payments WHERE merchant_id = $1 AND status = 'failed';

-- name: GetRecentPaymentsByMerchant :many
SELECT * FROM payments
WHERE merchant_id = $1
ORDER BY created_at DESC
LIMIT $2;
