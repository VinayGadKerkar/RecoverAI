-- name: CreateRecoveryAttempt :one
INSERT INTO recovery_attempts (payment_id, merchant_id, attempt_number, status)
VALUES ($1, $2, $3, 'pending')
RETURNING *;

-- name: UpdateRecoveryAttempt :one
UPDATE recovery_attempts
SET status = $2, action = $3, ai_command = $4, policy_decision = $5,
    policy_reason = $6, executed_at = $7, result = $8, updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: GetRecoveryAttemptsByPayment :many
SELECT * FROM recovery_attempts WHERE payment_id = $1 ORDER BY attempt_number ASC;

-- name: CountRecoveryAttemptsByPayment :one
SELECT COUNT(*) FROM recovery_attempts WHERE payment_id = $1;

-- name: GetLatestRecoveryAttempt :one
SELECT * FROM recovery_attempts
WHERE payment_id = $1
ORDER BY attempt_number DESC
LIMIT 1;

-- name: ListRecoveryAttemptsByMerchant :many
SELECT ra.*, p.amount, p.currency, p.error_code
FROM recovery_attempts ra
JOIN payments p ON p.id = ra.payment_id
WHERE ra.merchant_id = $1
ORDER BY ra.created_at DESC
LIMIT $2 OFFSET $3;
