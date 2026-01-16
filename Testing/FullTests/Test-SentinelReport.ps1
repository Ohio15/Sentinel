#Requires -Version 5.1
<#
.SYNOPSIS
    Test Report Generator for Sentinel Agent Testing
.DESCRIPTION
    Generates comprehensive test reports in multiple formats including
    HTML, JSON, and console output with detailed analysis.
.NOTES
    Author: Sentinel QA Team
    Version: 1.0.0
#>

[CmdletBinding()]
param(
    [Parameter()]
    [hashtable]$TestResults,

    [Parameter()]
    [string]$OutputPath = ".\TestResults",

    [Parameter()]
    [ValidateSet("All", "HTML", "JSON", "Console")]
    [string]$Format = "All",

    [Parameter()]
    [string]$Title = "Sentinel Agent Test Report"
)

$ErrorActionPreference = "Stop"

function Write-TestLog {
    param([string]$Message, [string]$Level = "Info")
    $color = switch ($Level) {
        "Info" { "White" }
        "Warning" { "Yellow" }
        "Error" { "Red" }
        "Success" { "Green" }
    }
    Write-Host "[REPORT] $Message" -ForegroundColor $color
}

function Get-TestResultsFromFiles {
    param([string]$Path)

    $allResults = @{
        StartTime = Get-Date
        EndTime = Get-Date
        TotalTests = 0
        Passed = 0
        Failed = 0
        Skipped = 0
        Results = @()
        Components = @{}
    }

    $jsonFiles = Get-ChildItem -Path $Path -Filter "*-results-*.json" -ErrorAction SilentlyContinue

    foreach ($file in $jsonFiles) {
        try {
            $fileResults = Get-Content $file.FullName | ConvertFrom-Json

            if ($fileResults.Component) {
                $allResults.Components[$fileResults.Component] = $fileResults.Tests

                foreach ($test in $fileResults.Tests) {
                    $allResults.TotalTests++
                    switch ($test.Status) {
                        "Passed" { $allResults.Passed++ }
                        "Failed" { $allResults.Failed++ }
                        default { $allResults.Skipped++ }
                    }
                    $allResults.Results += $test
                }
            } elseif ($fileResults.Results) {
                foreach ($test in $fileResults.Results) {
                    $allResults.TotalTests++
                    switch ($test.Status) {
                        "Passed" { $allResults.Passed++ }
                        "Failed" { $allResults.Failed++ }
                        default { $allResults.Skipped++ }
                    }
                    $allResults.Results += $test
                }
            }
        }
        catch {
            Write-TestLog "Failed to parse $($file.Name): $($_.Exception.Message)" -Level Warning
        }
    }

    return $allResults
}

function Write-ConsoleReport {
    param([hashtable]$Results)

    $width = 70

    Write-Host ""
    Write-Host ("=" * $width) -ForegroundColor Cyan
    Write-Host $Title.PadLeft(($width + $Title.Length) / 2) -ForegroundColor Cyan
    Write-Host ("=" * $width) -ForegroundColor Cyan
    Write-Host ""

    # Summary section
    Write-Host "SUMMARY" -ForegroundColor Yellow
    Write-Host ("-" * 20)

    $passRate = if ($Results.TotalTests -gt 0) {
        [math]::Round(($Results.Passed / $Results.TotalTests) * 100, 1)
    } else { 0 }

    Write-Host "Total Tests:  $($Results.TotalTests)"
    Write-Host "Passed:       " -NoNewline
    Write-Host "$($Results.Passed)" -ForegroundColor Green -NoNewline
    Write-Host " ($passRate%)"
    Write-Host "Failed:       " -NoNewline
    Write-Host "$($Results.Failed)" -ForegroundColor $(if ($Results.Failed -gt 0) { "Red" } else { "Green" })
    Write-Host "Skipped:      " -NoNewline
    Write-Host "$($Results.Skipped)" -ForegroundColor Yellow

    if ($Results.StartTime -and $Results.EndTime) {
        $duration = ($Results.EndTime - $Results.StartTime).TotalSeconds
        Write-Host "Duration:     ${duration}s"
    }

    Write-Host ""

    # Failed tests detail
    $failedTests = $Results.Results | Where-Object { $_.Status -eq "Failed" }
    if ($failedTests.Count -gt 0) {
        Write-Host "FAILED TESTS" -ForegroundColor Red
        Write-Host ("-" * 20)

        foreach ($test in $failedTests) {
            Write-Host "  X $($test.Name)" -ForegroundColor Red
            if ($test.Error) {
                Write-Host "    Error: $($test.Error)" -ForegroundColor DarkRed
            }
        }
        Write-Host ""
    }

    # Component breakdown
    if ($Results.Components.Count -gt 0) {
        Write-Host "COMPONENT BREAKDOWN" -ForegroundColor Yellow
        Write-Host ("-" * 20)

        foreach ($component in $Results.Components.Keys) {
            $componentTests = $Results.Components[$component]
            $componentPassed = ($componentTests | Where-Object { $_.Status -eq "Passed" }).Count
            $componentTotal = $componentTests.Count

            $status = if ($componentPassed -eq $componentTotal) { "Green" }
                      elseif ($componentPassed -gt 0) { "Yellow" }
                      else { "Red" }

            Write-Host "  $component`: " -NoNewline
            Write-Host "$componentPassed/$componentTotal passed" -ForegroundColor $status
        }
        Write-Host ""
    }

    # All tests detail
    Write-Host "ALL TESTS" -ForegroundColor Yellow
    Write-Host ("-" * 20)

    foreach ($test in $Results.Results) {
        $icon = switch ($test.Status) {
            "Passed" { "[PASS]" }
            "Failed" { "[FAIL]" }
            default { "[SKIP]" }
        }
        $color = switch ($test.Status) {
            "Passed" { "Green" }
            "Failed" { "Red" }
            default { "Yellow" }
        }

        Write-Host "  $icon $($test.Name)" -ForegroundColor $color
    }

    Write-Host ""
    Write-Host ("=" * $width) -ForegroundColor Cyan

    # Final verdict
    if ($Results.Failed -eq 0) {
        Write-Host "ALL TESTS PASSED" -ForegroundColor Green
    } else {
        Write-Host "SOME TESTS FAILED" -ForegroundColor Red
    }
    Write-Host ("=" * $width) -ForegroundColor Cyan
    Write-Host ""
}

function Write-HTMLReport {
    param(
        [hashtable]$Results,
        [string]$OutputFile
    )

    $passRate = if ($Results.TotalTests -gt 0) {
        [math]::Round(($Results.Passed / $Results.TotalTests) * 100, 1)
    } else { 0 }

    $statusClass = if ($Results.Failed -eq 0) { "success" }
                   elseif ($Results.Failed -lt $Results.Passed) { "warning" }
                   else { "danger" }

    $html = @"
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>$Title</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background: #1a1a2e; color: #eee; padding: 20px; }
        .container { max-width: 1200px; margin: 0 auto; }
        h1 { text-align: center; margin-bottom: 30px; color: #00d9ff; }
        .summary { display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: 20px; margin-bottom: 30px; }
        .card { background: #16213e; border-radius: 10px; padding: 20px; text-align: center; }
        .card h2 { font-size: 2.5em; margin-bottom: 5px; }
        .card p { color: #888; }
        .success h2 { color: #00ff88; }
        .danger h2 { color: #ff4444; }
        .warning h2 { color: #ffaa00; }
        .info h2 { color: #00d9ff; }
        .progress-bar { height: 10px; background: #333; border-radius: 5px; overflow: hidden; margin: 20px 0; }
        .progress-fill { height: 100%; transition: width 0.5s; }
        .progress-fill.success { background: linear-gradient(90deg, #00ff88, #00d9ff); }
        .progress-fill.warning { background: linear-gradient(90deg, #ffaa00, #ff6600); }
        .progress-fill.danger { background: linear-gradient(90deg, #ff4444, #ff0000); }
        table { width: 100%; border-collapse: collapse; margin-top: 20px; }
        th, td { padding: 12px; text-align: left; border-bottom: 1px solid #333; }
        th { background: #16213e; color: #00d9ff; }
        tr:hover { background: rgba(0, 217, 255, 0.1); }
        .status { padding: 4px 12px; border-radius: 20px; font-size: 0.85em; font-weight: bold; }
        .status.passed { background: rgba(0, 255, 136, 0.2); color: #00ff88; }
        .status.failed { background: rgba(255, 68, 68, 0.2); color: #ff4444; }
        .status.skipped { background: rgba(255, 170, 0, 0.2); color: #ffaa00; }
        .error-detail { color: #ff6666; font-size: 0.9em; margin-top: 5px; }
        .timestamp { text-align: center; color: #666; margin-top: 30px; }
    </style>
</head>
<body>
    <div class="container">
        <h1>$Title</h1>

        <div class="summary">
            <div class="card info">
                <h2>$($Results.TotalTests)</h2>
                <p>Total Tests</p>
            </div>
            <div class="card success">
                <h2>$($Results.Passed)</h2>
                <p>Passed</p>
            </div>
            <div class="card danger">
                <h2>$($Results.Failed)</h2>
                <p>Failed</p>
            </div>
            <div class="card warning">
                <h2>$($Results.Skipped)</h2>
                <p>Skipped</p>
            </div>
        </div>

        <div class="progress-bar">
            <div class="progress-fill $statusClass" style="width: ${passRate}%"></div>
        </div>
        <p style="text-align: center; color: #888;">Pass Rate: ${passRate}%</p>

        <table>
            <thead>
                <tr>
                    <th>Test Name</th>
                    <th>Status</th>
                    <th>Details</th>
                </tr>
            </thead>
            <tbody>
"@

    foreach ($test in $Results.Results) {
        $statusClass = $test.Status.ToLower()
        $details = if ($test.Error) {
            "<div class='error-detail'>$($test.Error)</div>"
        } elseif ($test.Duration) {
            "$($test.Duration)s"
        } else {
            "-"
        }

        $html += @"
                <tr>
                    <td>$($test.Name)</td>
                    <td><span class="status $statusClass">$($test.Status.ToUpper())</span></td>
                    <td>$details</td>
                </tr>
"@
    }

    $html += @"
            </tbody>
        </table>

        <p class="timestamp">Generated: $(Get-Date -Format "yyyy-MM-dd HH:mm:ss")</p>
    </div>
</body>
</html>
"@

    $html | Out-File $OutputFile -Encoding UTF8
    Write-TestLog "HTML report generated: $OutputFile" -Level Success
}

function Write-JSONReport {
    param(
        [hashtable]$Results,
        [string]$OutputFile
    )

    $report = @{
        title = $Title
        generatedAt = (Get-Date).ToString("o")
        summary = @{
            totalTests = $Results.TotalTests
            passed = $Results.Passed
            failed = $Results.Failed
            skipped = $Results.Skipped
            passRate = if ($Results.TotalTests -gt 0) {
                [math]::Round(($Results.Passed / $Results.TotalTests) * 100, 2)
            } else { 0 }
        }
        tests = $Results.Results
        components = $Results.Components
    }

    $report | ConvertTo-Json -Depth 10 | Out-File $OutputFile -Encoding UTF8
    Write-TestLog "JSON report generated: $OutputFile" -Level Success
}

# Main execution
function Start-ReportGeneration {
    Write-TestLog "Generating test reports..." -Level Info

    # Create output directory
    if (-not (Test-Path $OutputPath)) {
        New-Item -ItemType Directory -Path $OutputPath -Force | Out-Null
    }

    # Get results
    $results = if ($TestResults) {
        $TestResults
    } else {
        Get-TestResultsFromFiles -Path $OutputPath
    }

    if ($results.TotalTests -eq 0) {
        Write-TestLog "No test results found to report" -Level Warning
        return
    }

    $timestamp = Get-Date -Format "yyyyMMdd-HHmmss"

    switch ($Format) {
        "Console" {
            Write-ConsoleReport -Results $results
        }
        "HTML" {
            $htmlFile = Join-Path $OutputPath "test-report-$timestamp.html"
            Write-HTMLReport -Results $results -OutputFile $htmlFile
        }
        "JSON" {
            $jsonFile = Join-Path $OutputPath "test-report-$timestamp.json"
            Write-JSONReport -Results $results -OutputFile $jsonFile
        }
        "All" {
            Write-ConsoleReport -Results $results

            $htmlFile = Join-Path $OutputPath "test-report-$timestamp.html"
            Write-HTMLReport -Results $results -OutputFile $htmlFile

            $jsonFile = Join-Path $OutputPath "test-report-$timestamp.json"
            Write-JSONReport -Results $results -OutputFile $jsonFile
        }
    }

    Write-TestLog "Report generation complete" -Level Success
}

Start-ReportGeneration
