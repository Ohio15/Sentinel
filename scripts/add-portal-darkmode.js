const fs = require('fs');

let css = fs.readFileSync('src/portal/styles.css', 'utf8');

// Check if dark mode already added
if (css.includes('.dark {')) {
  console.log('Dark mode CSS already exists');
  process.exit(0);
}

// Add new variables to :root and add .dark section
const rootEnd = css.indexOf('}', css.indexOf(':root {'));
const beforeRoot = css.substring(0, rootEnd);
const afterRoot = css.substring(rootEnd);

const newVars = `  --bg: #f8f9fa;
  --bg-card: #ffffff;
  --text: #212529;
  --text-muted: #6c757d;
`;

const darkMode = `

/* Dark mode */
.dark {
  --primary: #4da3ff;
  --primary-hover: #2b8aed;
  --primary-light: rgba(77, 163, 255, 0.15);
  --secondary: #9ca3af;
  --success: #34d399;
  --warning: #fbbf24;
  --danger: #f87171;
  --light: #1f2937;
  --dark: #f9fafb;
  --border: #374151;
  --shadow: 0 2px 4px rgba(0, 0, 0, 0.3);
  --shadow-lg: 0 10px 25px rgba(0, 0, 0, 0.4);
  --bg: #111827;
  --bg-card: #1f2937;
  --text: #f9fafb;
  --text-muted: #9ca3af;
}
`;

css = beforeRoot + newVars + afterRoot.replace('}', '}' + darkMode);

// Update body to use new variables
css = css.replace(
  /color: var\(--dark\);/g,
  'color: var(--text);'
);
css = css.replace(
  /background: var\(--light\);/g,
  'background: var(--bg);'
);

// Update backgrounds to use --bg-card
css = css.replace(
  /\.header \{[\s\S]*?background: white;/,
  '.header {\n  background: var(--bg-card);'
);

// Add toggle button styles
if (!css.includes('.theme-toggle')) {
  css += `

/* Dark mode toggle */
.theme-toggle {
  background: transparent;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: 0.5rem;
  cursor: pointer;
  color: var(--text-muted);
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.2s;
}

.theme-toggle:hover {
  background: var(--primary-light);
  color: var(--primary);
  border-color: var(--primary);
}

.theme-toggle svg {
  width: 20px;
  height: 20px;
}
`;
}

fs.writeFileSync('src/portal/styles.css', css);
console.log('Added dark mode CSS');
