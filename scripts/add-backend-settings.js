const fs = require('fs');
const path = 'D:/Projects/Sentinel/src/renderer/pages/Settings.tsx';
let content = fs.readFileSync(path, 'utf8');

// Add handleConnectBackend function before handleSave
const handleSaveLoc = '  const handleSave = async () => {';
const handleConnectBackend = `  const handleConnectBackend = async () => {
    if (!backendUrl) {
      setBackendError('Please enter a backend URL');
      return;
    }
    if (!backendEmail || !backendPassword) {
      setBackendError('Please enter credentials');
      return;
    }

    setBackendConnecting(true);
    setBackendError('');

    try {
      // Set the URL first
      await window.api.backend.setUrl(backendUrl);

      // Then authenticate
      const result = await window.api.backend.authenticate(backendEmail, backendPassword);

      if (result.success) {
        setBackendConnected(true);
        setBackendPassword(''); // Clear password after successful connection
        alert('Successfully connected to external backend');
      } else {
        setBackendError(result.error || 'Authentication failed');
        setBackendConnected(false);
      }
    } catch (error) {
      setBackendError(error.message || 'Connection failed');
      setBackendConnected(false);
    } finally {
      setBackendConnecting(false);
    }
  };

`;

if (!content.includes('handleConnectBackend')) {
  content = content.replace(handleSaveLoc, handleConnectBackend + handleSaveLoc);
  console.log('Added handleConnectBackend function');
}

fs.writeFileSync(path, content, 'utf8');
console.log('Done');
