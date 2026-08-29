param(
    [Parameter(Mandatory=$true, HelpMessage="Payment ID from the failed payment")]
    [string]$PaymentID,
    
    [Parameter(Mandatory=$false)]
    [int]$Amount = 50000
)

$webhookSecret = "recoverai_secret"
$webhookUrl = "http://localhost:8080/webhooks/razorpay"

$timestamp = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds()
$eventId = "evt_success_" + (New-Guid).ToString().Substring(0, 8)

Write-Host "=====================================" -ForegroundColor Cyan
Write-Host "  Sending SUCCESS Webhook" -ForegroundColor Cyan
Write-Host "=====================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "Payment ID: $PaymentID" -ForegroundColor Yellow
Write-Host "Amount: Rs.$($Amount/100)" -ForegroundColor Yellow
Write-Host "Event: payment.captured" -ForegroundColor Green
Write-Host ""

$payload = @{
    entity = "event"
    account_id = "acc_test123"
    event = "payment.captured"
    contains = @("payment")
    payload = @{
        payment = @{
            entity = @{
                id = $PaymentID
                entity = "payment"
                amount = $Amount
                currency = "INR"
                status = "captured"
                order_id = "order_" + (New-Guid).ToString().Substring(0, 8)
                invoice_id = $null
                international = $false
                method = "card"
                amount_refunded = 0
                refund_status = $null
                captured = $true
                description = "Customer self-recovery test"
                card_id = "card_test123"
                bank = $null
                wallet = $null
                vpa = $null
                email = "test@example.com"
                contact = "+919999999999"
                customer_id = "cust_test123"
                token_id = $null
                notes = @{
                    recovery_test = "true"
                }
                fee = 0
                tax = 0
                error_code = $null
                error_description = $null
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

try {
    $response = Invoke-RestMethod -Uri $webhookUrl `
        -Method POST `
        -Body $payload `
        -ContentType "application/json" `
        -Headers @{
            "X-Razorpay-Signature" = $signature
            "X-Razorpay-Event-Id" = $eventId
        }
    
    Write-Host "Success!" -ForegroundColor Green
    Write-Host "Response: $($response | ConvertTo-Json)" -ForegroundColor Gray
    Write-Host ""
    Write-Host "Check results:" -ForegroundColor Cyan
    Write-Host "1. Dashboard: http://localhost:3000/dashboard/cases" -ForegroundColor Gray
    Write-Host "2. Or run: docker exec recoverai-postgres-1 psql -U recoverai -d recoverai -c ""SELECT status, amount_recovered_paise FROM recovery_cases WHERE razorpay_payment_id = '$PaymentID';""" -ForegroundColor Gray
    
} catch {
    Write-Host "Error sending webhook!" -ForegroundColor Red
    Write-Host $_.Exception.Message -ForegroundColor Red
}
