# RecoverAI System Verification Script
# Checks if AI is responding and pipeline is working

Write-Host ""
Write-Host "=== RECOVERAI SYSTEM VERIFICATION ===" -ForegroundColor Green
Write-Host ""

# 1. Check AI Mode
Write-Host "1. AI Service Configuration" -ForegroundColor Cyan
Write-Host "   Checking USE_MOCK_AI setting..." -ForegroundColor Gray
$useMockAI = docker-compose exec worker sh -c 'echo $USE_MOCK_AI' 2>$null
if ($useMockAI -match "true") {
    Write-Host "   Mode: MOCK AI (zero tokens)" -ForegroundColor Yellow
} else {
    Write-Host "   Mode: REAL AI (using Groq)" -ForegroundColor Green
}

# 2. Check AI Service Health
Write-Host ""
Write-Host "2. AI Service Health" -ForegroundColor Cyan
try {
    $aiHealth = Invoke-RestMethod -Uri "http://localhost:8000/health" -ErrorAction Stop
    Write-Host "   Status: HEALTHY" -ForegroundColor Green
    Write-Host "   Response: $($aiHealth | ConvertTo-Json -Compress)" -ForegroundColor Gray
} catch {
    Write-Host "   Status: UNHEALTHY" -ForegroundColor Red
    Write-Host "   Error: $($_.Exception.Message)" -ForegroundColor Red
}

# 3. Check Recent Recovery Cases
Write-Host ""
Write-Host "3. Recent Recovery Cases (Last 5)" -ForegroundColor Cyan
$casesQuery = @"
SELECT 
    LEFT(id, 8) as case_id,
    status,
    amount_paise/100 as amount_rupees,
    upi_error_code,
    CASE WHEN ai_strategy IS NOT NULL THEN 'YES' ELSE 'NO' END as has_ai,
    TO_CHAR(created_at, 'HH24:MI:SS') as time
FROM recovery_cases 
ORDER BY created_at DESC 
LIMIT 5;
"@

docker-compose exec -T postgres psql -U recoverai -d recoverai -c $casesQuery

# 4. Check for AI Strategy Data
Write-Host ""
Write-Host "4. Cases with AI Strategy" -ForegroundColor Cyan
$aiStrategyQuery = @"
SELECT 
    COUNT(*) as total_cases,
    COUNT(ai_strategy) as cases_with_ai_strategy,
    ROUND(100.0 * COUNT(ai_strategy) / NULLIF(COUNT(*), 0), 2) as percentage
FROM recovery_cases;
"@

docker-compose exec -T postgres psql -U recoverai -d recoverai -c $aiStrategyQuery

# 5. Check Sample AI Strategy Content
Write-Host ""
Write-Host "5. Sample AI Strategy (Most Recent)" -ForegroundColor Cyan
$sampleQuery = @"
SELECT 
    LEFT(id, 8) as case_id,
    upi_error_code,
    ai_strategy::text
FROM recovery_cases 
WHERE ai_strategy IS NOT NULL 
ORDER BY created_at DESC 
LIMIT 1;
"@

docker-compose exec -T postgres psql -U recoverai -d recoverai -c $sampleQuery

# 6. Check Worker Logs for AI Calls
Write-Host ""
Write-Host "6. Recent Worker Activity (AI Calls)" -ForegroundColor Cyan
Write-Host "   Checking worker logs for AI service calls..." -ForegroundColor Gray
$workerLogs = docker-compose logs worker --tail=100 2>$null
$aiCalls = $workerLogs | Select-String -Pattern "ai_service|calling AI|AI response" -CaseSensitive:$false
if ($aiCalls) {
    Write-Host "   Found $($aiCalls.Count) AI-related log entries" -ForegroundColor Green
    Write-Host "   Last 3 entries:" -ForegroundColor Gray
    $aiCalls | Select-Object -Last 3 | ForEach-Object {
        Write-Host "   $_" -ForegroundColor DarkGray
    }
} else {
    Write-Host "   No AI calls found in recent logs" -ForegroundColor Yellow
}

# 7. Check Bank Outage Flags
Write-Host ""
Write-Host "7. Active Bank Outage Flags" -ForegroundColor Cyan
$outageKeys = docker-compose exec redis redis-cli KEYS "bank_outage:*" 2>$null
if ($outageKeys -and $outageKeys -ne "(empty array)") {
    Write-Host "   Active outages:" -ForegroundColor Yellow
    $outageKeys -split "`n" | Where-Object { $_ -and $_ -ne "(empty array)" } | ForEach-Object {
        $ttl = docker-compose exec redis redis-cli TTL $_ 2>$null
        Write-Host "   - $_ (TTL: $ttl seconds)" -ForegroundColor Gray
    }
} else {
    Write-Host "   No active bank outages" -ForegroundColor Green
}

# 8. Check Kafka Topics
Write-Host ""
Write-Host "8. Kafka Topic Message Counts" -ForegroundColor Cyan
Write-Host "   Checking if messages are flowing through pipeline..." -ForegroundColor Gray
$topics = @("payment.events", "revenue.risk", "recovery.commands", "recovery.results")
foreach ($topic in $topics) {
    try {
        $output = docker-compose exec kafka /opt/kafka/bin/kafka-run-class.sh kafka.tools.GetOffsetShell --broker-list localhost:9092 --topic $topic --time -1 2>$null | Select-String -Pattern ":\d+$"
        if ($output) {
            $offset = ($output -split ":")[-1]
            Write-Host "   - ${topic}: $offset messages" -ForegroundColor Gray
        }
    } catch {
        Write-Host "   - ${topic}: Unable to check" -ForegroundColor Red
    }
}

# 9. Test API Endpoint for Recovery Cases
Write-Host ""
Write-Host "9. API Recovery Cases Endpoint" -ForegroundColor Cyan
try {
    $apiCases = Invoke-RestMethod -Uri "http://localhost:8080/api/v1/recovery-cases?limit=5" -ErrorAction Stop
    $caseCount = if ($apiCases.cases) { $apiCases.cases.Count } else { $apiCases.Count }
    Write-Host "   Status: WORKING" -ForegroundColor Green
    Write-Host "   Cases returned: $caseCount" -ForegroundColor Gray
} catch {
    Write-Host "   Status: ERROR" -ForegroundColor Red
    Write-Host "   Error: $($_.Exception.Message)" -ForegroundColor Red
}

# Summary
Write-Host ""
Write-Host "=== VERIFICATION COMPLETE ===" -ForegroundColor Green
Write-Host ""
Write-Host "To see live logs:" -ForegroundColor Cyan
Write-Host "  docker-compose logs -f worker ai-service" -ForegroundColor Gray
Write-Host ""
Write-Host "To test a webhook:" -ForegroundColor Cyan
Write-Host "  .\test_webhook.ps1 -ErrorCode U30 -Amount 499900" -ForegroundColor Gray
Write-Host ""
Write-Host "Dashboard: http://localhost:3000" -ForegroundColor Yellow
Write-Host ""
