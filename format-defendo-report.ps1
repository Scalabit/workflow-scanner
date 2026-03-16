# Defendo Security Report Formatter
# Runs the agent, formats the output nicely, and saves with message ID

param(
    [string]$CheckList = "1,2,3,4,5,6,7,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31,32,33,34"
)

# Run the agent and capture output
Write-Host "Running Defendo security checks..." -ForegroundColor Cyan
$rawOutput = & "C:\defendo\guardify-agent.exe" --pubsub-project workflow-scanner --pubsub-topic defendo-windows-alerts --json --only $CheckList

# Extract JSON and message ID
$jsonStart = $rawOutput.IndexOf('{')
$jsonEnd = $rawOutput.LastIndexOf('}') + 1
$jsonData = $rawOutput.Substring($jsonStart, $jsonEnd - $jsonStart)

# Extract message ID from the "Published to Pub/Sub" line
$pubsubLine = $rawOutput | Where-Object { $_ -like "*Published to Pub/Sub*message ID*" }
$messageId = if ($pubsubLine -match "message ID:\s*(\d+)") { $matches[1] } else { "unknown" }

# Parse JSON
try {
    $report = $jsonData | ConvertFrom-Json
} catch {
    Write-Error "Failed to parse JSON output"
    exit 1
}

# Create formatted report
$formattedReport = @"
=== DEFENDO SECURITY REPORT ===
Message ID: $messageId
Timestamp: $($report.timestamp)
Host: $($report.system.hostname)
User: $($report.system.username)
Architecture: $($report.system.architecture)

"@

# Categorize results
$critical = $report.results | Where-Object {$_.status -eq "CRITICAL"}
$warnings = $report.results | Where-Object {$_.status -eq "WARNING"}
$errors = $report.results | Where-Object {$_.status -eq "ERROR"}
$ok = $report.results | Where-Object {$_.status -eq "OK"}

# Add CRITICAL issues
if ($critical.Count -gt 0) {
    $formattedReport += "🔴 CRITICAL ISSUES ($($critical.Count)):`n"
    foreach ($item in $critical) {
        $formattedReport += "  - $($item.name): $($item.message)`n"
    }
    $formattedReport += "`n"
}

# Add WARNINGS
if ($warnings.Count -gt 0) {
    $formattedReport += "🟡 WARNINGS ($($warnings.Count)):`n"
    foreach ($item in $warnings) {
        $formattedReport += "  - $($item.name): $($item.message)`n"
    }
    $formattedReport += "`n"
}

# Add ERRORS
if ($errors.Count -gt 0) {
    $formattedReport += "❌ ERRORS ($($errors.Count)):`n"
    foreach ($item in $errors) {
        $formattedReport += "  - $($item.name): $($item.message)`n"
    }
    $formattedReport += "`n"
}

# Add PASSING checks
if ($ok.Count -gt 0) {
    $formattedReport += "✅ PASSING CHECKS ($($ok.Count)):`n"
    foreach ($item in $ok) {
        $formattedReport += "  - $($item.name)`n"
    }
    $formattedReport += "`n"
}

# Add summary
$total = $report.results.Count
$formattedReport += "SUMMARY:`n"
$formattedReport += "  Total Checks: $total`n"
$formattedReport += "  Critical: $($critical.Count)`n"
$formattedReport += "  Warnings: $($warnings.Count)`n"
$formattedReport += "  Errors: $($errors.Count)`n"
$formattedReport += "  Passing: $($ok.Count)`n"
$formattedReport += "`n"

# Calculate security score
$passingScore = if ($total -gt 0) { [math]::Round(($ok.Count / $total) * 100, 1) } else { 0 }
$formattedReport += "SECURITY SCORE: $passingScore% ($($ok.Count)/$total checks passing)`n"

# Save to file with message ID
$reportsDir = "C:\defendo\reports"
if (!(Test-Path $reportsDir)) {
    New-Item -ItemType Directory -Path $reportsDir -Force | Out-Null
}

$filename = "defendo-report-$messageId.txt"
$filepath = Join-Path $reportsDir $filename

$formattedReport | Out-File -FilePath $filepath -Encoding UTF8

# Also save raw JSON
$rawFilename = "defendo-report-$messageId.json"
$rawFilepath = Join-Path $reportsDir $rawFilename
$jsonData | Out-File -FilePath $rawFilepath -Encoding UTF8

# Upload formatted report to Cloud Storage
try {
    $bucket = "workflow-scanner-defendo-deployment"
    $gsutilPath = "C:\Program Files (x86)\Google\Cloud SDK\google-cloud-sdk\bin\gsutil.cmd"
    
    if (Test-Path $gsutilPath) {
        & $gsutilPath cp $filepath "gs://$bucket/reports/"
        & $gsutilPath cp $rawFilepath "gs://$bucket/reports/"
        Write-Host "Reports uploaded to Cloud Storage: gs://$bucket/reports/" -ForegroundColor Cyan
    }
} catch {
    Write-Host "Failed to upload to Cloud Storage: $($_.Exception.Message)" -ForegroundColor Yellow
}

# Publish formatted summary to Pub/Sub
try {
    $summaryData = @{
        message_id = $messageId
        timestamp = $report.timestamp
        hostname = $report.system.hostname
        security_score = $passingScore
        summary = @{
            total_checks = $total
            critical_issues = $critical.Count
            warnings = $warnings.Count
            errors = $errors.Count
            passing = $ok.Count
        }
        report_files = @{
            formatted = "gs://$bucket/reports/$filename"
            raw_json = "gs://$bucket/reports/$rawFilename"
        }
        top_critical_issues = $critical | Select-Object -First 3 | ForEach-Object { $_.name }
    } | ConvertTo-Json -Depth 3

    # Publish to separate topic for formatted reports
    & "C:\defendo\guardify-agent.exe" --pubsub-project workflow-scanner --pubsub-topic defendo-formatted-reports --json | Out-Null
    
    Write-Host "Report summary published to Pub/Sub topic: defendo-formatted-reports" -ForegroundColor Cyan
} catch {
    Write-Host "Failed to publish summary to Pub/Sub: $($_.Exception.Message)" -ForegroundColor Yellow
}

# Display summary
Write-Host ""
Write-Host "=== DEFENDO SECURITY REPORT ===" -ForegroundColor Cyan
Write-Host "Message ID: $messageId" -ForegroundColor Gray
Write-Host "Timestamp: $($report.timestamp)" -ForegroundColor Gray
Write-Host "Host: $($report.system.hostname)" -ForegroundColor Gray
Write-Host ""

if ($critical.Count -gt 0) {
    Write-Host "🔴 CRITICAL ISSUES ($($critical.Count)):" -ForegroundColor Red
    $critical | ForEach-Object { Write-Host "  - $($_.name): $($_.message)" -ForegroundColor Red }
    Write-Host ""
}

if ($warnings.Count -gt 0) {
    Write-Host "🟡 WARNINGS ($($warnings.Count)):" -ForegroundColor Yellow  
    $warnings | ForEach-Object { Write-Host "  - $($_.name): $($_.message)" -ForegroundColor Yellow }
    Write-Host ""
}

if ($errors.Count -gt 0) {
    Write-Host "❌ ERRORS ($($errors.Count)):" -ForegroundColor Magenta
    $errors | ForEach-Object { Write-Host "  - $($_.name): $($_.message)" -ForegroundColor Magenta }
    Write-Host ""
}

if ($ok.Count -gt 0) {
    Write-Host "✅ PASSING CHECKS ($($ok.Count)):" -ForegroundColor Green
    $ok | ForEach-Object { Write-Host "  - $($_.name)" -ForegroundColor Green }
    Write-Host ""
}

Write-Host "SECURITY SCORE: " -NoNewline
if ($passingScore -ge 80) { 
    Write-Host "$passingScore%" -ForegroundColor Green
} elseif ($passingScore -ge 60) { 
    Write-Host "$passingScore%" -ForegroundColor Yellow
} else { 
    Write-Host "$passingScore%" -ForegroundColor Red
}

Write-Host ""
Write-Host "Reports saved to:" -ForegroundColor Cyan
Write-Host "  Formatted: $filepath" -ForegroundColor Gray
Write-Host "  Raw JSON: $rawFilepath" -ForegroundColor Gray