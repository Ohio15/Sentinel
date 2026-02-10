# Sentinel RMM - Windows Debloat Scripts

Transform any Windows PC (any manufacturer) into a clean, business-ready system by removing bloatware, disabling telemetry, and applying a security baseline.

## Quick Start

### Remote Deployment (via Ethernet direct connection)

1. Connect target PC to management PC via Ethernet
2. On management PC, start HTTP server:
   ```bash
   cd D:/Projects/Sentinel/scripts/debloat
   python -m http.server 8080 --bind 192.168.137.1
   ```
3. On target PC (Admin PowerShell), run:
   ```powershell
   # First enable remote access
   iex (irm http://192.168.137.1:8080/enable-remote.ps1)

   # Then run full debloat
   iex (irm http://192.168.137.1:8080/windows-debloat-full.ps1)
   ```

### Local Execution

Run in Admin PowerShell:
```powershell
.\windows-debloat-full.ps1
```

## Scripts

| Script | Purpose |
|--------|---------|
| `enable-remote.ps1` | Enable WinRM for remote PowerShell access |
| `windows-debloat-full.ps1` | **Master script** - runs all phases |
| `01-remove-oem-bloat.ps1` | Remove OEM/manufacturer bloatware |
| `02-remove-appx-bloat.ps1` | Remove Windows Store bloatware |
| `03-disable-consumer-features.ps1` | Disable auto-install and telemetry |
| `04-disable-annoyances.ps1` | Disable Widgets, Copilot, suggestions |
| `05-security-baseline.ps1` | Enable Defender, firewall, disable risks |

## What Gets Removed

### OEM Bloatware (Phase 1)
| Manufacturer | Apps Removed |
|--------------|--------------|
| **Acer** | Care Center, Quick Access, Planet9, DriverSetupUtility |
| **HP** | Support Assistant, Audio Switch, JumpStart, Sure Click |
| **Dell** | SupportAssist, Digital Delivery, Update, Customer Connect |
| **Lenovo** | Vantage, Now, Settings, ID |
| **ASUS** | MyASUS, Giftbox, Smart Gesture, ROG Gaming Center |
| **All** | Norton, McAfee, ExpressVPN, Amazon, Booking.com |

### Windows Store Apps (Phase 2)
- Xbox apps (unless gaming PC)
- Cortana
- Mixed Reality
- Maps, People, Solitaire
- Skype, Your Phone
- Clipchamp, Power Automate
- Third-party: Netflix, Spotify, Disney+, TikTok, etc.

### What's KEPT
- Microsoft Store
- Calculator, Camera, Notepad, Photos
- Paint, Snipping Tool
- Terminal, PowerShell
- All framework packages (VCLibs, .NET, XAML)

## Settings Changed

### Consumer Features Disabled (Phase 3)
- Auto-install of suggested apps
- Content Delivery Manager
- Telemetry (reduced to minimum)
- Diagnostic services

### Annoyances Disabled (Phase 4)
- Start Menu suggestions/recommendations
- Widgets button
- Chat/Teams button
- Windows Copilot
- Search highlights / Bing suggestions
- Windows Tips

### Security Baseline Applied (Phase 5)
- Windows Defender: Enabled
- Windows Firewall: Enabled (all profiles)
- Remote Registry: Disabled
- Remote Assistance: Disabled
- AutoPlay: Disabled
- SmartScreen: Enabled
- UAC: Enabled

## Post-Debloat Checklist

After running, verify:

- [ ] **REBOOT** the system
- [ ] Microsoft 365 / Office (if pre-installed) still works
- [ ] Windows Defender is active (Security app)
- [ ] All hardware drivers functional
- [ ] No promotional apps in Start Menu
- [ ] No Widgets on taskbar

## Network Setup for Remote Deployment

When target PC is connected directly via Ethernet (no router):

**Management PC:**
```powershell
# Set static IP (or enable ICS)
netsh interface ip set address "Ethernet" static 192.168.137.1 255.255.255.0
```

**Target PC:**
```powershell
# Set static IP
netsh interface ip set address "Ethernet" static 192.168.137.2 255.255.255.0 192.168.137.1
```

**Test connectivity:**
```powershell
ping 192.168.137.2  # From management PC
Test-WSMan -ComputerName 192.168.137.2  # After enable-remote.ps1
```

## Troubleshooting

| Issue | Solution |
|-------|----------|
| Script won't run | Run PowerShell as Administrator |
| WinRM connection refused | Run `enable-remote.ps1` on target first |
| Apps reinstall after reboot | Run Phase 2 again, then reboot |
| Defender won't enable | Remove third-party antivirus first |
| Some settings reverted | Apply to Default profile (Phase 4 does this) |

## License

Part of Sentinel RMM. For internal use.
