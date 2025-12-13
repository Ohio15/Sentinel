filepath = 'D:/Projects/Sentinel/agent/internal/updater/updater.go'

with open(filepath, 'r', encoding='utf-8') as f:
    content = f.read()

# Fix missing tab on batchContent line
content = content.replace('\nbatchContent := fmt.Sprintf', '\n\tbatchContent := fmt.Sprintf')

with open(filepath, 'w', encoding='utf-8') as f:
    f.write(content)

print('Fixed indentation')
