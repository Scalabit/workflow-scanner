# Wazuh Agent Installation Script for Windows
# This script downloads, installs, and configures the Wazuh agent

param(
    [string]$WazuhManagerHost = "${wazuh_manager_ip}",
    [string]$WazuhAgentName = "${wazuh_agent_name}",
    [string]$WazuhVersion = "4.14.2"
)

# Function to write logs
function Write-Log {
    param([string]$Message)
    $timestamp = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
    Write-Host "[$timestamp] $Message"
    Add-Content -Path "C:\wazuh-install.log" -Value "[$timestamp] $Message"
}

try {
    Write-Log "Starting Wazuh agent installation..."

    # Create log directory
    if (!(Test-Path "C:\")) {
        New-Item -ItemType Directory -Path "C:\" -Force
    }

    # Download Wazuh agent MSI
    $wazuhUrl = "https://packages.wazuh.com/4.x/windows/wazuh-agent-$WazuhVersion-1.msi"
    $downloadPath = "C:\wazuh-agent-$WazuhVersion-1.msi"
    
    Write-Log "Downloading Wazuh agent from: $wazuhUrl"
    
    # Use TLS 1.2 for secure download
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
    
    try {
        Invoke-WebRequest -Uri $wazuhUrl -OutFile $downloadPath -UseBasicParsing
        Write-Log "Wazuh agent downloaded successfully to: $downloadPath"
    } catch {
        Write-Log "Failed to download Wazuh agent: $($_.Exception.Message)"
        throw
    }

    # Verify file exists and has content
    if (!(Test-Path $downloadPath)) {
        Write-Log "Downloaded file not found at: $downloadPath"
        throw "Download verification failed"
    }

    $fileSize = (Get-Item $downloadPath).Length
    Write-Log "Downloaded file size: $fileSize bytes"

    if ($fileSize -lt 1MB) {
        Write-Log "Downloaded file appears to be too small, possible download error"
        throw "Download verification failed - file too small"
    }

    # Prepare installation parameters
    $installArgs = @(
        "/i", "`"$downloadPath`"",
        "/q",  # Quiet installation
        "WAZUH_MANAGER=`"$WazuhManagerHost`""
        "WAZUH_AGENT_NAME=`"$WazuhAgentName`""
    )

    # Only add registration server if manager host is provided
    if ($WazuhManagerHost -and $WazuhManagerHost -ne "") {
        $installArgs += "WAZUH_REGISTRATION_SERVER=`"$WazuhManagerHost`""
    }

    Write-Log "Installing Wazuh agent with parameters: $($installArgs -join ' ')"

    # Install Wazuh agent
    $process = Start-Process -FilePath "msiexec.exe" -ArgumentList $installArgs -Wait -PassThru -NoNewWindow
    
    if ($process.ExitCode -eq 0) {
        Write-Log "Wazuh agent installed successfully"
    } else {
        Write-Log "Wazuh agent installation failed with exit code: $($process.ExitCode)"
        throw "Installation failed"
    }

    # Wait a moment for the service to be created
    Start-Sleep -Seconds 10

    # Check if Wazuh service exists and start it
    $wazuhService = Get-Service -Name "WazuhSvc" -ErrorAction SilentlyContinue
    
    if ($wazuhService) {
        Write-Log "Wazuh service found. Current status: $($wazuhService.Status)"
        
        if ($wazuhService.Status -ne 'Running') {
            Write-Log "Starting Wazuh service..."
            try {
                Start-Service -Name "WazuhSvc"
                Write-Log "Wazuh service started successfully"
            } catch {
                Write-Log "Failed to start Wazuh service: $($_.Exception.Message)"
            }
        } else {
            Write-Log "Wazuh service is already running"
        }
    } else {
        Write-Log "Warning: Wazuh service not found after installation"
    }

    # Configure Windows Event Log monitoring
    $ossecConfigPath = "C:\Program Files (x86)\ossec-agent\ossec.conf"
    if (Test-Path $ossecConfigPath) {
        Write-Log "Configuring Wazuh for Windows Event Log monitoring..."
        
        # Backup original configuration
        Copy-Item $ossecConfigPath "$ossecConfigPath.backup" -Force
        
        # Read current config
        $configContent = Get-Content $ossecConfigPath -Raw
        
        # Add Windows Event Log monitoring if not already present
        if ($configContent -notmatch "Application.*eventchannel") {
            $eventLogConfig = @"

  <!-- Windows Event Log monitoring -->
  <localfile>
    <location>Application</location>
    <log_format>eventchannel</log_format>
  </localfile>
  
  <localfile>
    <location>System</location>
    <log_format>eventchannel</log_format>
  </localfile>
  
  <localfile>
    <location>Security</location>
    <log_format>eventchannel</log_format>
  </localfile>

"@
            # Insert before closing ossec_config tag
            $configContent = $configContent -replace '</ossec_config>', "$eventLogConfig</ossec_config>"
            Set-Content $ossecConfigPath -Value $configContent
            Write-Log "Windows Event Log monitoring configuration added"
            
            # Restart service to apply configuration
            if ($wazuhService -and $wazuhService.Status -eq 'Running') {
                Write-Log "Restarting Wazuh service to apply configuration..."
                Restart-Service -Name "WazuhSvc" -Force
                Write-Log "Wazuh service restarted"
            }
        }
    } else {
        Write-Log "Warning: Wazuh configuration file not found at expected location"
    }

    # Clean up downloaded installer
    if (Test-Path $downloadPath) {
        Remove-Item $downloadPath -Force
        Write-Log "Cleaned up downloaded installer"
    }

    Write-Log "Wazuh agent installation and configuration completed successfully"
    
    # Create a completion marker file
    "Wazuh agent installed successfully on $(Get-Date)" | Out-File -FilePath "C:\wazuh-install-complete.txt"

} catch {
    $errorMessage = "Wazuh agent installation failed: $($_.Exception.Message)"
    Write-Log $errorMessage
    Write-Error $errorMessage
    
    # Create error marker file
    $errorMessage | Out-File -FilePath "C:\wazuh-install-error.txt"
    exit 1
}

Write-Log "Script execution completed"