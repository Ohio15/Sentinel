import re

with open('D:/Projects/Sentinel/src/main/main.ts', 'r', encoding='utf-8') as f:
    content = f.read()

# Find the line with .replace('# Usage: and add another .replace after it
pattern = r"(\.replace\('# Usage: irm https://your-server/install\.ps1 \| iex', `# Pre-configured for: \$\{serverUrl\}\\n# Generated: \$\{new Date\(\)\.toISOString\(\)\}`\);)\n(\})"

replacement = r"\1\n    .replace(/# Or with parameters:.*/, '# Server and token are pre-configured - just run this script!');\n\2"

new_content, count = re.subn(pattern, replacement, content)

if count > 0:
    print(f'Made {count} replacement(s)')
    with open('D:/Projects/Sentinel/src/main/main.ts', 'w', encoding='utf-8') as f:
        f.write(new_content)
else:
    print('Pattern not found, trying simpler approach...')
    # Simpler approach - just find and replace the specific line
    if "    .replace('# Usage: irm https://your-server/install.ps1 | iex'" in content:
        old_line = "    .replace('# Usage: irm https://your-server/install.ps1 | iex', `# Pre-configured for: ${serverUrl}\\n# Generated: ${new Date().toISOString()}`);\n}"
        new_line = "    .replace('# Usage: irm https://your-server/install.ps1 | iex', `# Pre-configured for: ${serverUrl}\\n# Generated: ${new Date().toISOString()}`)\n    .replace(/# Or with parameters:.*/, '# Server and token are pre-configured - just run this script!');\n}"

        if old_line in content:
            content = content.replace(old_line, new_line)
            print('Made replacement with simpler approach')
            with open('D:/Projects/Sentinel/src/main/main.ts', 'w', encoding='utf-8') as f:
                f.write(content)
        else:
            print('Simpler pattern also not found')
            # Show what's actually there
            idx = content.find(".replace('# Usage:")
            if idx != -1:
                print('Content around pattern:')
                print(repr(content[idx:idx+250]))
