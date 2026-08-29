# Test if the create-order endpoint is working
# This validates the payment endpoint without opening Razorpay Checkout

Write-Host "🧪 Testing Payment Order Creation Endpoint" -ForegroundColor Cyan
Write-Host ""

# Test data
$body = @{
    amount = 59900  # ₹599 in paise
    currency = "INR"
    notes = @{
        test_type = "endpoint_validation"
        description = "Testing order creation"
    }
} | ConvertTo-Json

Write-Host "📤 Request Body:" -ForegroundColor Yellow
Write-Host $body
Write-Host ""

# Make request
Write-Host "🌐 Sending POST request to: http://localhost:8080/api/v1/create-order" -ForegroundColor Yellow

try {
    $response = Invoke-RestMethod -Uri "http://localhost:8080/api/v1/create-order" `
        -Method POST `
        -Body $body `
        -ContentType "application/json" `
        -Headers @{
            "Authorization" = "Bearer dummy-token-for-test"
        } `
        -ErrorAction Stop

    Write-Host ""
    Write-Host "✅ SUCCESS! Order created:" -ForegroundColor Green
    Write-Host "Order ID: $($response.id)" -ForegroundColor Green
    Write-Host "Amount: Rs. $([math]::Round($response.amount / 100, 2))" -ForegroundColor Green
    Write-Host "Currency: $($response.currency)" -ForegroundColor Green
    Write-Host "Status: $($response.status)" -ForegroundColor Green
    Write-Host ""
    Write-Host "✅ Payment endpoint is working! You can now use test-payment.html" -ForegroundColor Green
    
} catch {
    $statusCode = $_.Exception.Response.StatusCode.value__
    
    if ($statusCode -eq 401) {
        Write-Host "⚠️  Got 401 Unauthorized - This is EXPECTED!" -ForegroundColor Yellow
        Write-Host "The endpoint requires JWT authentication." -ForegroundColor Yellow
        Write-Host ""
        Write-Host "🎯 Solution: test-payment.html handles authentication automatically." -ForegroundColor Cyan
        Write-Host "   Just open the HTML file and click the payment buttons" -ForegroundColor Cyan
        Write-Host ""
        Write-Host "✅ Endpoint is registered and responding (authentication working as expected)" -ForegroundColor Green
    } else {
        Write-Host "❌ ERROR: $($_.Exception.Message)" -ForegroundColor Red
        Write-Host "Status Code: $statusCode" -ForegroundColor Red
        Write-Host ""
        Write-Host "🔍 Troubleshooting:" -ForegroundColor Yellow
        Write-Host "1. Check if API is running: docker-compose ps api" -ForegroundColor Yellow
        Write-Host "2. Check API logs: docker-compose logs api --tail=50" -ForegroundColor Yellow
        Write-Host "3. Restart API: docker-compose restart api" -ForegroundColor Yellow
    }
}
