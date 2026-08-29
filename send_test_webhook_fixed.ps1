# Send a test webhook to verify the fix
# This simulates a failed payment with BAD_REQUEST_ERROR

$webhookSecret = "recoverai_secret"
$webhookUrl = "http://localhost:8080/webhooks/razorpay"

# Generate unique IDs
$timestamp = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds()
$eventId = "evt_test_" + (New-Guid).ToString().Substring(0, 8)
$paymentId = "pay_test_" + (New-Guid).ToString().Substring(0, 8)
$orderId = "order_test_" + (New-Guid).ToString().Substring(0, 8)

# Webhook payload
$payload = @{
    entity = "event"
    account_id = "acc_test123"
    event = "payment.failed"
    contains = @("payment")
    payload = @{
        payment = @{
            entity = @{
                id = $paymentId
                entity = "payment"
                amount = 79900
                currency = "INR"
                status = "failed"
                order_id = $orderId
                invoice_id = $null
                international = $false
                method = "card"
                amount_refunded = 0
                refund_status = $null
                captured = $false
                description = "Test payment after schema fix"
                card_id = $null
                bank = $null
                wallet = $null
                vpa = $null
                email = "test@example.com"
                contact = "+919999999999"
                customer_id = "cust_test123"
                token_id = $null
                notes = @{
                    test_type = "schema_fix_validation"
                }
                fee = 0
                tax = 0
                error_code = "BAD_REQUEST_ERROR"
                error_description = "Payment processing failed"
                error_source = "customer"
                error_step = "payment_authentication"
                error_reason = "payment_failed"
                acquirer_data = @{
                    auth_code = $null
                }
                created_at = $timestamp
            }
        }
    }
    created_at = $timestamp
} | ConvertTo-Json -Depth 10

# Generate HMAC signature
$hmac = New-Object System.Security.Cryptography.HMACSHA256
$hmac.Key = [Text.Encoding]::UTF8.GetBytes($webhookSecret)
$signature = [BitConverter]::ToString($hmac.ComputeHash([Text.Encoding]::UTF8.GetBytes($payload))).Replace("-", "").ToLower()

Write-Host "🧪 Sending test webhook (after schema fix)" -ForegroundColor Cyan
Write-Host "Payment ID: $paymentId" -ForegroundColor Yellow
Write-Host "Error Code: BAD_REQUEST_ERROR (17 chars)" -ForegroundColor Yellow
Write-Host ""

try {
    $response = Invoke-RestMethod -Uri $webhookUrl `
        -Method POST `
        -Body $payload `
        -ContentType "application/json" `
        -Headers @{
            "X-Razorpay-Signature" = $signature
            "X-Razorpay-Event-Id" = $eventId
        }
    
    Write-Host "✅ Webhook accepted!" -ForegroundColor Green
    Write-Host ""
    Write-Host "🔍 Check results:" -ForegroundColor Cyan
    Write-Host "   docker-compose logs -f worker --tail=20" -ForegroundColor Gray
    Write-Host "   http://localhost:3000/dashboard/cases" -ForegroundColor Gray
    
} catch {
    Write-Host "❌ Error: $($_.Exception.Message)" -ForegroundColor Red
}
