Get-Service | Where-Object { $_.Name -like '*sentinel*' -or $_.DisplayName -like '*sentinel*' } | Format-List Name, Status, DisplayName
