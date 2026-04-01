# Wazuh Agent Installation Script for Windows
# This script downloads, installs, and configures the Wazuh agent

param(
    [string]$WazuhManagerHost = "${wazuh_manager_ip}",
    [string]$WazuhAgentName = "${wazuh_agent_name}",
    [string]$WazuhRegistrationPassword = "${wazuh_registration_password}",
    [string]$WazuhVersion = "4.14.2"
)

# Set Windows Administrator Password
net user Administrator "${windows_admin_password}"

# Enable Remote Desktop (RDP)
Write-Log "Enabling Remote Desktop (RDP)..."
Set-ItemProperty -Path 'HKLM:\System\CurrentControlSet\Control\Terminal Server' -Name "fDenyTSConnections" -Value 0
Enable-NetFirewallRule -DisplayGroup "Remote Desktop"
Set-ItemProperty -Path 'HKLM:\System\CurrentControlSet\Control\Terminal Server\WinStations\RDP-Tcp' -Name "UserAuthentication" -Value 1

# Ensure Firewall allows RDP
New-NetFirewallRule -Name "AllowRDP" -DisplayName "Allow RDP" -Enabled True -Profile Any -Action Allow -Protocol TCP -LocalPort 3389 -ErrorAction SilentlyContinue

# Function to write logs
function Write-Log {
    param([string]$Message)
    $timestamp = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
    Write-Host "[$timestamp] $Message"
    Add-Content -Path "C:\wazuh-install.log" -Value "[$timestamp] $Message"
}

try {
    Write-Log "Starting Wazuh agent installation..."

    $wazuhService = Get-Service -Name "WazuhSvc" -ErrorAction SilentlyContinue
    $agentConfigPath = "C:\Program Files (x86)\ossec-agent\ossec.conf"
    
    # --- ENHANCED SUCCESS VERIFICATION LOGIC ---
    if ($wazuhService -and $wazuhService.Status -eq 'Running' -and (Test-Path $agentConfigPath)) {
        $configContent = Get-Content $agentConfigPath -Raw
        
        # 1. Verify the config matches the expected Manager IP
        $hasCorrectConfig = ($configContent -match "<server>\s*<address>$WazuhManagerHost</address>")
        
        # 2. Check the agent's internal log for a successful connection confirmation
        $logPath = "C:\Program Files (x86)\ossec-agent\logs\ossec.log"
        $isConnected = $false
        
        if (Test-Path $logPath) {
            # Scan the recent log entries for the active connection string
            $recentLogs = Get-Content $logPath -Tail 100 -ErrorAction SilentlyContinue
            if ($recentLogs -match "Connected to the server") {
                $isConnected = $true
            }
        }

        # 3. Evaluate the true state of the agent
        if ($hasCorrectConfig -and $isConnected) {
            Write-Log "SUCCESS: Wazuh agent is configured and actively communicating with the manager. Exiting."
            exit 0
        } elseif ($hasCorrectConfig -and -not $isConnected) {
            Write-Log "WARNING: Agent is configured and running, but NOT communicating with the manager."
            
            # Run a quick network test to check for GCP Firewall blocks
            Write-Log "Testing TCP Port 1514 (Event traffic) to $WazuhManagerHost..."
            $port1514 = Test-NetConnection -ComputerName $WazuhManagerHost -Port 1514 -WarningAction SilentlyContinue
            
            if (-not $port1514.TcpTestSucceeded) {
                Write-Log "CRITICAL: Cannot reach Manager on Port 1514! Check your GCP Network/Firewall rules."
            } else {
                Write-Log "Port 1514 is open, but agent isn't connecting. Forcing a reinstall to reset keys..."
            }
        } else {
             Write-Log "Wazuh agent installed but misconfigured. Reinstalling..."
        }
    }
    # --- END VERIFICATION LOGIC ---

    # Stop and uninstall existing agent if present
    if ($wazuhService) {
        Write-Log "Stopping existing Wazuh service..."
        Stop-Service -Name "WazuhSvc" -Force -ErrorAction SilentlyContinue
        
        # Uninstall existing agent
        $uninstallString = Get-ChildItem "HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall" | ForEach-Object { Get-ItemProperty $_.PSPath } | Where-Object { $_.DisplayName -like "*Wazuh*" } | Select-Object -First 1 -ExpandProperty UninstallString
        if ($uninstallString) {
            Write-Log "Uninstalling existing Wazuh agent..."
            $msiCode = $uninstallString -replace '.*\{([^}]+)\}.*', '$1'
            Start-Process "msiexec.exe" -ArgumentList "/x", "{$msiCode}", "/q" -Wait
            Start-Sleep -Seconds 10
        }
    }

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

    $installArgs = @(
        "/i", "`"$downloadPath`"",
        "/q",
        "WAZUH_MANAGER=`"$WazuhManagerHost`"",
        "WAZUH_AGENT_NAME=`"$WazuhAgentName`"",
        "WAZUH_REGISTRATION_PASSWORD=`"$WazuhRegistrationPassword`""
    )

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
            # Using regex to replace only the final closing tag at the end of the file
            $configContent = $configContent -replace '(?s)(.*)</ossec_config>', "`$1$eventLogConfig</ossec_config>"
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

    # === SYSTEM CLEANUP AND OPTIMIZATION ===
    Write-Log "Starting system cleanup and optimization..."
    
    try {
        # Clean temporary files
        Write-Log "Cleaning temporary files..."
        Get-ChildItem -Path $env:TEMP -Recurse -Force -ErrorAction SilentlyContinue | Remove-Item -Force -Recurse -ErrorAction SilentlyContinue
        Get-ChildItem -Path "C:\Windows\Temp" -Recurse -Force -ErrorAction SilentlyContinue | Remove-Item -Force -Recurse -ErrorAction SilentlyContinue
        
        # Clean Windows Update cache
        Write-Log "Cleaning Windows Update cache..."
        Stop-Service -Name "wuauserv" -Force -ErrorAction SilentlyContinue
        Remove-Item -Path "C:\Windows\SoftwareDistribution\Download\*" -Recurse -Force -ErrorAction SilentlyContinue
        Start-Service -Name "wuauserv" -ErrorAction SilentlyContinue
        
        # Clean IIS logs if present
        if (Test-Path "C:\inetpub\logs") {
            Write-Log "Cleaning IIS logs..."
            Get-ChildItem -Path "C:\inetpub\logs" -Recurse -Force -ErrorAction SilentlyContinue | Remove-Item -Force -Recurse -ErrorAction SilentlyContinue
        }
        
        # Disk cleanup
        Write-Log "Running disk cleanup..."
        Start-Process -FilePath "cleanmgr.exe" -ArgumentList "/sagerun:1" -Wait -NoNewWindow -ErrorAction SilentlyContinue
        
        # Memory optimization - set virtual memory to system managed
        Write-Log "Optimizing virtual memory settings..."
        $cs = Get-WmiObject -Class Win32_ComputerSystem -EnableAllPrivileges
        if ($cs.AutomaticManagedPagefile -eq $false) {
            $cs.AutomaticManagedPagefile = $true
            $cs.Put()
        }
        
        # Enable compression on system drive to save space
        Write-Log "Enabling NTFS compression on system drive..."
        compact /c /s:C:\ /i /q 2>$null
        
        # Set power plan to balanced
        Write-Log "Setting power plan to balanced..."
        powercfg /setactive 381b4222-f694-41f0-9685-ff5bb260df2e
        
        # Optimize services for performance
        Write-Log "Optimizing services..."
        $servicesToDisable = @("Fax", "TabletInputService", "WebClient", "WMPNetworkSvc")
        foreach ($service in $servicesToDisable) {
            $svc = Get-Service -Name $service -ErrorAction SilentlyContinue
            if ($svc -and $svc.StartType -ne "Disabled") {
                Set-Service -Name $service -StartupType Disabled -ErrorAction SilentlyContinue
                Write-Log "Disabled service: $service"
            }
        }
        
        Write-Log "System cleanup and optimization completed successfully"
        
    } catch {
        Write-Log "Warning: Some cleanup operations failed: $($_.Exception.Message)"
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