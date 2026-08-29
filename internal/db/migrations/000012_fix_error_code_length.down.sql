-- Revert error code length change
ALTER TABLE recovery_cases 
ALTER COLUMN upi_error_code TYPE VARCHAR(10);
