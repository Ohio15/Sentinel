const fs = require('fs');

// Update index.html to add theme toggle
let html = fs.readFileSync('src/portal/index.html', 'utf8');

if (html.includes('theme-toggle')) {
  console.log('Theme toggle already in HTML');
} else {
  // Use regex to find and replace
  const pattern = /<span id="userName"><\/span>\s*\n\s*<button class="btn btn-secondary btn-sm" onclick="logout\(\)">Sign Out<\/button>/;

  const replacement = `<span id="userName"></span>
        <button class="theme-toggle" onclick="toggleTheme()" title="Toggle dark mode" id="themeToggle">
          <svg id="sunIcon" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <circle cx="12" cy="12" r="5"></circle>
            <line x1="12" y1="1" x2="12" y2="3"></line>
            <line x1="12" y1="21" x2="12" y2="23"></line>
            <line x1="4.22" y1="4.22" x2="5.64" y2="5.64"></line>
            <line x1="18.36" y1="18.36" x2="19.78" y2="19.78"></line>
            <line x1="1" y1="12" x2="3" y2="12"></line>
            <line x1="21" y1="12" x2="23" y2="12"></line>
            <line x1="4.22" y1="19.78" x2="5.64" y2="18.36"></line>
            <line x1="18.36" y1="5.64" x2="19.78" y2="4.22"></line>
          </svg>
          <svg id="moonIcon" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="display: none;">
            <path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"></path>
          </svg>
        </button>
        <button class="btn btn-secondary btn-sm" onclick="logout()">Sign Out</button>`;

  if (pattern.test(html)) {
    html = html.replace(pattern, replacement);
    fs.writeFileSync('src/portal/index.html', html);
    console.log('Added theme toggle to HTML');
  } else {
    console.log('Could not find user-info section via regex');
    // Debug - show what's there
    const userInfoMatch = html.match(/<div class="user-info"[\s\S]*?<\/div>/);
    if (userInfoMatch) {
      console.log('Found user-info:', userInfoMatch[0].substring(0, 200));
    }
  }
}

console.log('Done!');
