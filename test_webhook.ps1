# RecoverAI Webhook Test Script
# Generates proper HMAC-SHA256 signatures for testing

param(
    [string]$WebhookSecret = "recoverai_secret",
    [string]$ApiUrl = "http://localhost:8080",
    [string]$ErrorCode = "U30",
    [int]$Amount = 499900
)

function Get-HmacSha256 {
    param([string]$Message, [string]$Secret)
    
    $hmac = New-Object System.Security.Cryptography.HMACSHA256
    $hmac.Key = [Text.Encoding]::UTF8.GetBytes($Secret)
    $hashBytes = $hmac.ComputeHash([Text.Encoding]::UTF8.GetBytes($Message))
    return [BitConverter]::ToString($hashBytes).Replace("-", "").ToLower()
}

# Generate test data
$timestamp = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds()
$randomSuffix = Get-Random -Maximum 99999
$eventId = "evt_test_${ErrorCode}_${randomSuffix}"
$paymentId = "pay_test_${ErrorCode}_${randomSuffix}"

# Build webhook payload
$payload = @{
    entity = "event"
    account_id = "acc_demo"
    event = "payment.failed"
    contains = @("payment")
    payload = @{
        payment = @{
            entity = @{
                id = $paymentId
                amount = $Amount
                currency = "INR"
                status = "failed"
                method = "upi"
                error_code = $ErrorCode
                error_description = "Test failure for $ErrorCode"
                bank = "HDFC"
                vpa = "test@upi"
                email = "test@example.com"
                contact = "+919999999999"
                created_at = $timestamp
            }
        }
    }
    created_at = $timestamp
}

# Convert to JSON (this is what gets signed)
$body = $payload | ConvertTo-Json -Depth 10 -Compress

# Generate HMAC signature
$signature = Get-HmacSha256 -Message $body -Secret $WebhookSecret

Write-Host ""
Write-Host "Sending webhook:" -ForegroundColor Cyan
Write-Host "  Event ID: $eventId" -ForegroundColor Gray
Write-Host "  Payment ID: $paymentId" -ForegroundColor Gray
Write-Host "  Error Code: $ErrorCode" -ForegroundColor Gray
$amountRupees = $Amount / 100
Write-Host "  Amount: Rs.$amountRupees" -ForegroundColor Gray

try {
    $response = Invoke-RestMethod -Uri "$ApiUrl/webhooks/razorpay" -Method Post -Body $body -ContentType "application/json" -Headers @{"X-Razorpay-Event-Id" = $eventId; "X-Razorpay-Signature" = $signature}
    
    Write-Host ""
    Write-Host "Success!" -ForegroundColor Green
    $responseJson = $response | ConvertTo-Json
    Write-Host "  Response: $responseJson" -ForegroundColor Gray
    return $true
} catch {
    Write-Host ""
    Write-Host "Failed!" -ForegroundColor Red
    $errorMsg = $_.Exception.Message
    Write-Host "  Error: $errorMsg" -ForegroundColor Red
    return $false
}
