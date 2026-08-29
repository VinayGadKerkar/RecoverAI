Write-Host "==============================================`n" -ForegroundColor Cyan
Write-Host "     Creating Cases with Detailed AI Review" -ForegroundColor Cyan
Write-Host "`n==============================================" -ForegroundColor Cyan

# Test 1: High recovery probability
Write-Host "`n1 Creating U30 case (High Recovery Probability)" -ForegroundColor Yellow
Write-Host "   Amount: Rs.250 | Error: U30 (Transient Failure)" -ForegroundColor Gray
.\test_webhook.ps1 -ErrorCode "U30" -Amount 25000
Write-Host "   Waiting for AI processing..." -ForegroundColor Gray
Start-Sleep -Seconds 6

# Test 2: Moderate recovery
Write-Host "`n2 Creating U16 case (Moderate Recovery)" -ForegroundColor Yellow  
Write-Host "   Amount: Rs.150 | Error: U16 (Insufficient Balance)" -ForegroundColor Gray
.\test_webhook.ps1 -ErrorCode "U16" -Amount 15000
Write-Host "   Waiting for AI processing..." -ForegroundColor Gray
Start-Sleep -Seconds 6

# Test 3: Different error type
Write-Host "`n3 Creating Z9 case (Low Recovery)" -ForegroundColor Yellow
Write-Host "   Amount: Rs.200 | Error: Z9 (Insufficient Funds)" -ForegroundColor Gray
.\test_webhook.ps1 -ErrorCode "Z9" -Amount 20000
Write-Host "   Waiting for AI processing..." -ForegroundColor Gray
Start-Sleep -Seconds 6

Write-Host "`n============================================" -ForegroundColor Green
Write-Host "   Done! Check dashboard now:" -ForegroundColor Green
Write-Host "============================================" -ForegroundColor Green
Write-Host "`n   http://localhost:3000/dashboard/cases`n" -ForegroundColor Cyan

# Show latest AI strategies
Write-Host "Latest AI Strategies:" -ForegroundColor Yellow
docker exec recoverai-postgres-1 psql -U recoverai -d recoverai -c "SELECT LEFT(id::text, 8) as case_id, upi_error_code as error, ai_strategy->'strategy' as strategy, ROUND((ai_strategy->'confidence')::text::numeric, 2) as conf FROM recovery_cases WHERE ai_strategy IS NOT NULL ORDER BY created_at DESC LIMIT 5;"

Write-Host "`nClick 'View Details' on any case to see full AI review!" -ForegroundColor Cyan
