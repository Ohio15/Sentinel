const fs = require('fs');

let content = fs.readFileSync('src/renderer/pages/Clients.tsx', 'utf8');

// Check if already added
if (content.includes('Sync state when client prop changes')) {
  console.log('useEffect already exists');
  process.exit(0);
}

// Find the line to insert after
const targetLine = "const [error, setError] = useState<string | null>(null);";
const insertAfter = targetLine + "\n";
const newCode = targetLine + `

  // Sync state when client prop changes (for editing different clients)
  useEffect(() => {
    setName(client?.name || '');
    setDescription(client?.description || '');
    setColor(client?.color || '#6366f1');
    setLogoUrl(client?.logoUrl || '');
    setLogoWidth(client?.logoWidth || 32);
    setLogoHeight(client?.logoHeight || 32);
    setLogoError(false);
    setError(null);
  }, [client]);
`;

if (content.includes(targetLine)) {
  content = content.replace(targetLine, newCode);
  fs.writeFileSync('src/renderer/pages/Clients.tsx', content);
  console.log('Updated successfully');
} else {
  console.log('Target line not found');
}
