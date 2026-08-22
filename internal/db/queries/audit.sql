-- name: CreateAuditLog :one
INSERT INTO audit_log (payment_id, merchant_id, stage, action, actor, decision, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetAuditLogByPayment :many
SELECT * FROM audit_log WHERE payment_id = $1 ORDER BY created_at ASC;

-- name: GetAuditLogByMerchant :many
SELECT * FROM audit_log
WHERE merchant_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;
