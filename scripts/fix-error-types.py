import os
import re

os.chdir('D:/Projects/Sentinel/src/renderer')

files_to_fix = [
    'components/FileExplorer.tsx',
    'components/RemoteDesktop.tsx',
    'components/Terminal.tsx',
    'components/UpdateNotification.tsx',
    'pages/Certificates.tsx',
    'pages/Clients.tsx',
    'pages/DeviceDetail.tsx',
    'pages/Scripts.tsx',
    'pages/Settings.tsx',
]

for filename in files_to_fix:
    if not os.path.exists(filename):
        print(f'File not found: {filename}')
        continue

    with open(filename, 'r', encoding='utf-8') as f:
        content = f.read()

    original = content

    # Simple string replacements for common patterns
    replacements = [
        # setError patterns
        ('setError(err.message)', "setError(err instanceof Error ? err.message : 'Unknown error')"),
        ('setError(error.message)', "setError(error instanceof Error ? error.message : 'Unknown error')"),

        # || fallback patterns
        ("(err.message || 'Failed to save client')", "(err instanceof Error ? err.message : 'Unknown error')"),
        ("(error.message || 'Connection failed')", "(error instanceof Error ? error.message : 'Unknown error')"),
        ("(error.message || 'Unknown error')", "(error instanceof Error ? error.message : 'Unknown error')"),

        # alert with template literal - err
        ('`Download failed: ${err.message}`', "`Download failed: ${err instanceof Error ? err.message : 'Unknown error'}`"),
        ('`Failed to renew certificates: ${err.message}`', "`Failed to renew certificates: ${err instanceof Error ? err.message : 'Unknown error'}`"),
        ('`Failed to distribute certificate: ${err.message}`', "`Failed to distribute certificate: ${err instanceof Error ? err.message : 'Unknown error'}`"),

        # alert with template literal - error
        ('`Error: ${error.message}`', "`Error: ${error instanceof Error ? error.message : 'Unknown error'}`"),
        ('`Error saving portal settings: ${error.message}`', "`Error saving portal settings: ${error instanceof Error ? error.message : 'Unknown error'}`"),
        ('`Error adding tenant mapping: ${error.message}`', "`Error adding tenant mapping: ${error instanceof Error ? error.message : 'Unknown error'}`"),
        ('`Error deleting tenant mapping: ${error.message}`', "`Error deleting tenant mapping: ${error instanceof Error ? error.message : 'Unknown error'}`"),
        ('`Error saving settings: ${error.message}`', "`Error saving settings: ${error instanceof Error ? error.message : 'Unknown error'}`"),

        # setOutput with template
        ('`Failed to connect: ${error.message}\\n`', "`Failed to connect: ${error instanceof Error ? error.message : 'Unknown error'}\\n`"),

        # setBackendError
        ("setBackendError(error.message || 'Connection failed')", "setBackendError(error instanceof Error ? error.message : 'Connection failed')"),

        # RemoteDesktop
        ("'Failed to connect: ' + (error.message || 'Unknown error')", "'Failed to connect: ' + (error instanceof Error ? error.message : 'Unknown error')"),

        # setCommandOutput
        ('`Error: ${error.message}`', "`Error: ${error instanceof Error ? error.message : 'Unknown error'}`"),
    ]

    for old, new in replacements:
        content = content.replace(old, new)

    if content != original:
        with open(filename, 'w', encoding='utf-8') as f:
            f.write(content)
        print(f'Updated {filename}')
    else:
        print(f'No changes in {filename}')
