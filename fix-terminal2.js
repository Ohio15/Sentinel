const fs = require('fs');
const path = 'D:/Projects/Sentinel/src/renderer/components/Terminal.tsx';
let content = fs.readFileSync(path, 'utf-8');

// Add error logging if not present
if (!content.includes("console.error('[Terminal] terminal.start error:")) {
  const oldCatch = "} catch (error: any) {\n      setOutput([`Failed to connect:";
  const newCatch = "} catch (error: any) {\n      console.error('[Terminal] terminal.start error:', error);\n      setOutput([`Failed to connect:";

  if (content.includes(oldCatch)) {
    content = content.replace(oldCatch, newCatch);
    fs.writeFileSync(path, content);
    console.log('Added error logging to catch block');
  } else {
    console.log('Catch pattern not found');
    // Try a more flexible pattern
    const idx = content.indexOf('} catch (error: any) {');
    if (idx !== -1) {
      console.log('Found catch at index:', idx);
      console.log('Context:', content.substring(idx, idx + 100));
    }
  }
} else {
  console.log('Error logging already present');
}
