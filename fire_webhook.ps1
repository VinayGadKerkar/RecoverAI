# fire_webhook.ps1
# Fires a signed Razorpay payment.failed webhook to the local API.
# Usage:  .\fire_webhook.ps1
# Optional: pass -Count N to fire N events (default 5)

param([int]$Count = 5)

$API_URL = "http://localhost:8080"
$WEBHOOK_SECRET = "recoverai_secret"   # must match RAZORPAY_WEBHOOK_SECRET in .env

$UPI_ERRORS = @("U30","Z9","Z8","XC","XH","XJ","XK","XL","XM","XN","XP")
$AMOUNTS    = @(50000, 149900, 299900, 75000, 999900, 500000, 25000, 120000)

function Get-HmacSha256([string]$key, [string]$message) {
    $hmac = New-Object System.Security.Cryptography.HMACSHA256
    $hmac.Key = [Text.Encoding]::UTF8.GetBytes($key)
    $bytes = $hmac.ComputeHash([Text.Encoding]::UTF8.GetBytes($message))
    return ($bytes | ForEach-Object { $_.ToString("x2") }) -join ""
}

Write-Host "Firing $Count payment.failed webhooks..." -ForegroundColor Cyan

for ($i = 1; $i -le $Count; $i++) {
    $payId    = "pay_demo_" + [System.Guid]::NewGuid().ToString("N").Substring(0,14)
    $orderId  = "order_demo_" + [System.Guid]::NewGuid().ToString("N").Substring(0,12)
    $evtId    = "evt_demo_" + [System.Guid]::NewGuid().ToString("N").Substring(0,14)
    $upiCode  = $UPI_ERRORS[(Get-Random -Maximum $UPI_ERRORS.Length)]
    $amount   = $AMOUNTS[(Get-Random -Maximum $AMOUNTS.Length)]

    $body = @"
{"entity":"event","account_id":"acc_demo_merchant","event":"payment.failed","contains":["payment"],"payload":{"payment":{"entity":{"id":"$payId","entity":"payment","amount":$amount,"currency":"INR","status":"failed","order_id":"$orderId","method":"upi","error_code":"$upiCode","error_description":"Payment failed","error_reason":"payment_failed","error_source":"customer","error_step":"payment_authentication","vpa":"customer@upi","notes":{"upi_error_code":"$upiCode","merchant_id":"demo"},"created_at":$(([DateTimeOffset]::UtcNow.ToUnixTimeSeconds()))}}}}
"@

    $sig = Get-HmacSha256 -key $WEBHOOK_SECRET -message $body

    try {
        $resp = Invoke-RestMethod -Method Post `
            -Uri "$API_URL/webhooks/razorpay" `
            -ContentType "application/json" `
            -Body $body `
            -Headers @{
                "X-Razorpay-Signature" = $sig
                "X-Razorpay-Event-Id"  = $evtId
            }
        Write-Host "  [$i/$Count] $payId  UPI=$upiCode  ₹$([math]::Round($amount/100,0))  -> OK" -ForegroundColor Green
    } catch {
        $code = $_.Exception.Response.StatusCode.value__
        Write-Host "  [$i/$Count] $payId  -> FAILED ($code): $($_.ErrorDetails.Message)" -ForegroundColor Red
    }

    Start-Sleep -Milliseconds 200
}

Write-Host "`nDone. Open http://localhost:3000/dashboard to see the cases." -ForegroundColor Cyan
