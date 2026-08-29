-- Fix upi_error_code field to support longer error codes from Razorpay
-- Razorpay sends codes like 'BAD_REQUEST_ERROR' (17 chars), not just UPI codes

ALTER TABLE recovery_cases 
ALTER COLUMN upi_error_code TYPE VARCHAR(50);
