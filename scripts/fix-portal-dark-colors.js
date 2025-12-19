const fs = require('fs');
let css = fs.readFileSync('src/portal/styles.css', 'utf8');

// Fix ticket-detail-header gradient
css = css.replace(
  'background: linear-gradient(to bottom, #fafbfc, white);',
  'background: linear-gradient(to bottom, var(--bg), var(--bg-card));'
);

// Fix comment-form-container and similar #fafbfc backgrounds
css = css.replace(/background: #fafbfc;/g, 'background: var(--bg);');

// Fix the older-comments-toggle background
css = css.replace(
  'background: #e5e7eb;',
  'background: var(--border);'
);

// Add dark mode for select dropdowns (they often don't inherit)
if (!css.includes('.dark select')) {
  css += `

/* Dark mode select styling */
.dark select {
  background-color: var(--bg-card);
  color: var(--text);
}

.dark select option {
  background-color: var(--bg-card);
  color: var(--text);
}

/* Dark mode input placeholder */
.dark input::placeholder,
.dark textarea::placeholder {
  color: var(--text-muted);
}

/* Dark mode readonly fields */
.dark .readonly-field {
  background-color: var(--bg) !important;
  color: var(--text-muted) !important;
}
`;
}

fs.writeFileSync('src/portal/styles.css', css);
console.log('Fixed dark mode colors');
