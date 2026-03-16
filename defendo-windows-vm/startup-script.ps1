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
# Download defendo agent from Cloud Storage
$defendoPath = "C:\defendo"
$logFile = "$defendoPath\logs\agent-$(Get-Date -Format 'yyyy-MM-dd').log"

function Write-Log {
    param($Message)
    $timestamp = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
    "$timestamp - $Message" | Tee-Object -FilePath $logFile -Append | Write-Output
}

try {
    Write-Log "Starting defendo agent download from Cloud Storage..."
    
    # Download files from Cloud Storage
    $bucket = "workflow-scanner-defendo-deployment"
    $gsutilPath = "C:\Program Files (x86)\Google\Cloud SDK\google-cloud-sdk\bin\gsutil.cmd"
    
    if (Test-Path $gsutilPath) {
        Write-Log "Downloading agent files..."
        & $gsutilPath -m cp "gs://$bucket/*" $defendoPath
        
        # Check if download successful
        $agentPath = "$defendoPath\guardify-agent.exe"
        if (Test-Path $agentPath) {
            Write-Log "Agent downloaded successfully, running deployment script..."
            
            if (Test-Path "$defendoPath\deploy-agent.ps1") {
                & "$defendoPath\deploy-agent.ps1"
            } else {
                Write-Log "Deployment script not found, installing manually..."
                
                # Manual deployment
                $serviceName = "DefendoAgent"
                $project = "workflow-scanner"
                $topic = "defendo-windows-alerts"
                
                # Stop existing service
                Stop-Service -Name $serviceName -Force -ErrorAction SilentlyContinue
                & sc.exe delete $serviceName 2>$null
                
                # Create new service (excluding WiFi check #8 for Windows Server)
                $arguments = '--pubsub-project workflow-scanner --pubsub-topic defendo-windows-alerts --interval 10m --json --only "1,2,3,4,5,6,7,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31,32,33,34"'
                
                & sc.exe create $serviceName binPath= "`"$agentPath`" $arguments" start= auto DisplayName= "Defendo Security Agent"
                & sc.exe description $serviceName "Defendo security monitoring agent"
                
                # Start service
                Start-Service -Name $serviceName
                Write-Log "Defendo agent service started successfully"
            }
        } else {
            Write-Log "Agent download failed"
        }
    } else {
        Write-Log "Google Cloud SDK not found"
    }
    
} catch {
    Write-Log "Error: $($_.Exception.Message)"
}
'@

$downloadScript | Out-File -FilePath "$defendoPath\download-agent.ps1" -Encoding UTF8

Write-Output "Defendo Windows VM setup completed"