# Defendo Agent Startup Script
# This script runs when the Windows VM boots up

# Set execution policy
Set-ExecutionPolicy -ExecutionPolicy RemoteSigned -Force

# Create defendo directory
$defendoPath = "C:\defendo"
New-Item -ItemType Directory -Path $defendoPath -Force

# Create logs directory
$logsPath = "$defendoPath\logs"
New-Item -ItemType Directory -Path $logsPath -Force

# Create scheduled task to download and run defendo agent
$taskAction = New-ScheduledTaskAction -Execute "PowerShell.exe" -Argument "-ExecutionPolicy RemoteSigned -File C:\defendo\download-agent.ps1"
$taskTrigger = New-ScheduledTaskTrigger -AtStartup
$taskSettings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -StartWhenAvailable
$taskPrincipal = New-ScheduledTaskPrincipal -UserId "SYSTEM" -LogonType ServiceAccount -RunLevel Highest

Register-ScheduledTask -TaskName "DefendoAgentDownload" -Action $taskAction -Trigger $taskTrigger -Settings $taskSettings -Principal $taskPrincipal -Force

# Create the download script
$downloadScript = @'
# Download defendo agent from GitHub Actions artifacts
$defendoPath = "C:\defendo"
$logFile = "$defendoPath\logs\agent-$(Get-Date -Format 'yyyy-MM-dd').log"

function Write-Log {
    param($Message)
    $timestamp = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
    "$timestamp - $Message" | Tee-Object -FilePath $logFile -Append | Write-Output
}

try {
    Write-Log "Starting defendo agent download..."
    
    # Note: In production, this would download from GitHub releases or artifacts
    # For now, create placeholder
    Write-Log "Waiting for defendo binary deployment..."
    
    # Check if guardify-agent.exe exists
    $agentPath = "$defendoPath\guardify-agent.exe"
    if (Test-Path $agentPath) {
        Write-Log "Defendo agent found, starting security checks..."
        
        # Run the agent with Pub/Sub integration
        $project = "workflow-scanner"
        $topic = "defendo-security-alerts"
        
        & $agentPath --pubsub-project $project --pubsub-topic $topic --interval 1h --json | Tee-Object -FilePath $logFile -Append
    } else {
        Write-Log "Defendo agent not found at $agentPath"
    }
    
} catch {
    Write-Log "Error: $($_.Exception.Message)"
}
'@

$downloadScript | Out-File -FilePath "$defendoPath\download-agent.ps1" -Encoding UTF8

Write-Output "Defendo Windows VM setup completed"