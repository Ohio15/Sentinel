const fs = require('fs');
let css = fs.readFileSync('src/portal/styles.css', 'utf8');

// Add dark mode styles for textareas if not already present
const fixes = [
  ['.comment-input-row textarea {', '.comment-input-row textarea {\n  background: var(--bg-card);\n  color: var(--text);'],
  ['.comment-form textarea {', '.comment-form textarea {\n  background: var(--bg-card);\n  color: var(--text);'],
  ['.edit-comment-textarea {', '.edit-comment-textarea {\n  background: var(--bg-card);\n  color: var(--text);'],
];

for (const [search, replace] of fixes) {
  if (css.includes(search) && !css.includes(replace)) {
    css = css.replace(search, replace);
  }
}

fs.writeFileSync('src/portal/styles.css', css);
console.log('Updated textarea styles for dark mode');
