# RecoverAI Demo Scenarios Script
# Runs all 3 demo scenarios with proper webhook signatures

$WebhookSecret = "recoverai_secret"
$ApiUrl = "http://localhost:8080"

function Get-HmacSha256 {
    param([string]$Message, [string]$Secret)
    $hmac = New-Object System.Security.Cryptography.HMACSHA256
    $hmac.Key = [Text.Encoding]::UTF8.GetBytes($Secret)
    $hashBytes = $hmac.ComputeHash([Text.Encoding]::UTF8.GetBytes($Message))
    return [BitConverter]::ToString($hashBytes).Replace("-", "").ToLower()
}

function Send-Webhook {
    param([string]$EventId, [string]$PaymentId, [int]$Amount, [string]$ErrorCode, [string]$Bank, [string]$VPA)
    
    $timestamp = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds()
    $payload = @{
        entity = "event"
        account_id = "acc_demo"
        event = "payment.failed"
        contains = @("payment")
        payload = @{
            payment = @{
                entity = @{
                    id = $PaymentId
                    amount = $Amount
                    currency = "INR"
                    status = "failed"
                    method = "upi"
                    error_code = $ErrorCode
                    error_description = "Test failure"
                    bank = $Bank
                    vpa = $VPA
                    email = "test@example.com"
                    contact = "+919999999999"
                    created_at = $timestamp
                }
            }
        }
        created_at = $timestamp
    }
    
    $body = $payload | ConvertTo-Json -Depth 10 -Compress
    $signature = Get-HmacSha256 -Message $body -Secret $WebhookSecret
    
    try {
        $null = Invoke-RestMethod -Uri "$ApiUrl/webhooks/razorpay" -Method Post -Body $body -ContentType "application/json" -Headers @{"X-Razorpay-Event-Id" = $EventId; "X-Razorpay-Signature" = $signature} -ErrorAction Stop
        return $true
    } catch {
        return $false
    }
}

Write-Host ""
Write-Host "=== DEMO SCENARIOS STARTING ===" -ForegroundColor Green
Write-Host ""

# Scenario 1: Bank Outage Detection
Write-Host "Step 1/3: Firing 15 U28 failures to trigger bank outage detection" -ForegroundColor Cyan
$successCount = 0
for ($i = 1; $i -le 15; $i++) {
    $randomSuffix = Get-Random -Maximum 99999
    $eventId = "evt_demo_outage_${i}_${randomSuffix}"
    $paymentId = "pay_demo_outage_${i}_${randomSuffix}"
    
    if (Send-Webhook -EventId $eventId -PaymentId $paymentId -Amount 899900 -ErrorCode "U28" -Bank "SBI" -VPA "demo${i}@upi") {
        Write-Host "." -NoNewline -ForegroundColor Green
        $successCount++
    } else {
        Write-Host "x" -NoNewline -ForegroundColor Red
    }
    Start-Sleep -Milliseconds 200
}
Write-Host ""
Write-Host "Sent $successCount/15 U28 failures successfully" -ForegroundColor Green
Write-Host "Waiting 5s for outage detection to propagate..." -ForegroundColor Yellow
Start-Sleep -Seconds 5

# Scenario 2: High-LTV Customer Recovery
Write-Host ""
Write-Host "Step 2/3: Firing U30 failure (high-LTV customer) - recovery pipeline" -ForegroundColor Cyan
$randomSuffix = Get-Random -Maximum 99999
$eventId = "evt_demo_u30_${randomSuffix}"
$paymentId = "pay_demo_u30_${randomSuffix}"

if (Send-Webhook -EventId $eventId -PaymentId $paymentId -Amount 499900 -ErrorCode "U30" -Bank "HDFC" -VPA "highltv@upi") {
    Write-Host "Sent successfully" -ForegroundColor Green
} else {
    Write-Host "Failed to send" -ForegroundColor Red
}

# Scenario 3: Negative ROI Block
Write-Host ""
Write-Host "Step 3/3: Firing Z9 failure (Rs.99 new customer) - validator blocks (negative ROI)" -ForegroundColor Cyan
$randomSuffix = Get-Random -Maximum 99999
$eventId = "evt_demo_z9_${randomSuffix}"
$paymentId = "pay_demo_z9_${randomSuffix}"

if (Send-Webhook -EventId $eventId -PaymentId $paymentId -Amount 9900 -ErrorCode "Z9" -Bank "KOTAK" -VPA "newcustomer@upi") {
    Write-Host "Sent successfully" -ForegroundColor Green
} else {
    Write-Host "Failed to send" -ForegroundColor Red
}

Write-Host ""
Write-Host "Waiting 30s for pipeline to process all events..." -ForegroundColor Yellow
Start-Sleep -Seconds 30

Write-Host ""
Write-Host "=== DEMO SCENARIOS COMPLETE ===" -ForegroundColor Green
Write-Host ""
Write-Host "Dashboard should now show:" -ForegroundColor Cyan
Write-Host "  - Bank outage:U28 active in Redis" -ForegroundColor Gray
Write-Host "  - U30 case in in_progress or recovered" -ForegroundColor Gray
Write-Host "  - Z9 Rs.99 case as not_worth_recovering" -ForegroundColor Gray
Write-Host ""
Write-Host "Open dashboard: http://localhost:3000" -ForegroundColor Yellow
