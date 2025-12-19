const fs = require('fs');
let code = fs.readFileSync('src/renderer/pages/Devices.tsx', 'utf8');

// Fix download result toast
code = code.replace(
  "? 'bg-green-50 border border-green-200'",
  "? 'bg-green-50 dark:bg-green-900/30 border border-green-200 dark:border-green-800'"
);
code = code.replace(
  ": 'bg-red-50 border border-red-200'",
  ": 'bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800'"
);

// Fix check/error icons
code = code.replace(
  '<CheckIcon className="w-5 h-5 text-green-600" />',
  '<CheckIcon className="w-5 h-5 text-green-600 dark:text-green-400" />'
);
code = code.replace(
  '<ErrorIcon className="w-5 h-5 text-red-600" />',
  '<ErrorIcon className="w-5 h-5 text-red-600 dark:text-red-400" />'
);

// Fix result text colors
code = code.replace(
  "downloadResult.type === 'success' ? 'text-green-700' : 'text-red-700'",
  "downloadResult.type === 'success' ? 'text-green-700 dark:text-green-300' : 'text-red-700 dark:text-red-300'"
);

// Fix close button hover
code = code.replace(
  'className="ml-auto text-gray-400 hover:text-gray-600"',
  'className="ml-auto text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"'
);

// Fix platform download buttons (bg-gray-50 hover:bg-gray-100)
code = code.replace(
  /className="flex items-center gap-3 p-4 bg-gray-50 rounded-lg hover:bg-gray-100 transition-colors border border-border disabled:opacity-50 disabled:cursor-not-allowed text-left"/g,
  'className="flex items-center gap-3 p-4 bg-gray-50 dark:bg-gray-800 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors border border-border disabled:opacity-50 disabled:cursor-not-allowed text-left"'
);

// Fix MSI result toast
code = code.replace(
  "msiResult.type === 'success'\n                  ? 'bg-green-50 border border-green-200'\n                  : 'bg-red-50 border border-red-200'",
  "msiResult.type === 'success'\n                  ? 'bg-green-50 dark:bg-green-900/30 border border-green-200 dark:border-green-800'\n                  : 'bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800'"
);

// Fix MSI result text colors
code = code.replace(
  "msiResult.type === 'success' ? 'text-green-700' : 'text-red-700'",
  "msiResult.type === 'success' ? 'text-green-700 dark:text-green-300' : 'text-red-700 dark:text-red-300'"
);

// Fix silent install command label
code = code.replace(
  '<p className="text-xs text-green-600 mb-1">Silent install command:</p>',
  '<p className="text-xs text-green-600 dark:text-green-400 mb-1">Silent install command:</p>'
);

// Fix info boxes (bg-gray-50 without hover)
code = code.replace(
  '<div className="p-4 bg-gray-50 rounded-lg border border-border">',
  '<div className="p-4 bg-gray-50 dark:bg-gray-800 rounded-lg border border-border">'
);

// Fix terminal icon color
code = code.replace(
  '<TerminalIcon className="w-5 h-5 text-gray-600" />',
  '<TerminalIcon className="w-5 h-5 text-gray-600 dark:text-gray-400" />'
);

// Fix code text color
code = code.replace(
  '<code className="text-xs text-gray-600 block mt-1">',
  '<code className="text-xs text-gray-600 dark:text-gray-400 block mt-1">'
);

// Fix PowerShell button
code = code.replace(
  'className="flex items-center gap-3 p-4 bg-blue-50 rounded-lg hover:bg-blue-100 transition-colors border border-blue-200 disabled:opacity-50 disabled:cursor-not-allowed text-left"',
  'className="flex items-center gap-3 p-4 bg-blue-50 dark:bg-blue-900/30 rounded-lg hover:bg-blue-100 dark:hover:bg-blue-900/50 transition-colors border border-blue-200 dark:border-blue-800 disabled:opacity-50 disabled:cursor-not-allowed text-left"'
);

// Fix PowerShell icon colors
code = code.replace(
  '<PowerShellIcon className="w-5 h-5 text-blue-700" />',
  '<PowerShellIcon className="w-5 h-5 text-blue-700 dark:text-blue-400" />'
);

// Fix PowerShell text colors
code = code.replace(
  '<p className="font-medium text-blue-900">Run PowerShell Install</p>',
  '<p className="font-medium text-blue-900 dark:text-blue-100">Run PowerShell Install</p>'
);
code = code.replace(
  '<p className="text-xs text-blue-700">',
  '<p className="text-xs text-blue-700 dark:text-blue-300">'
);

// Fix PowerShell play icon
code = code.replace(
  '<PlayIcon className="w-5 h-5 text-blue-600" />',
  '<PlayIcon className="w-5 h-5 text-blue-600 dark:text-blue-400" />'
);

// Fix info icon
code = code.replace(
  '<InfoIcon className="w-5 h-5 text-gray-500" />',
  '<InfoIcon className="w-5 h-5 text-gray-500 dark:text-gray-400" />'
);

// Fix installation notes code snippet
code = code.replace(
  '<code className="bg-gray-100 px-1 rounded">chmod +x sentinel-agent</code>',
  '<code className="bg-gray-100 dark:bg-gray-700 px-1 rounded">chmod +x sentinel-agent</code>'
);

// Fix StatusBadge offline state
code = code.replace(
  "offline: 'bg-gray-100 text-gray-600',",
  "offline: 'bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-300',"
);

// Fix Apple icon for dark mode (it's gray-600 which is hard to see)
code = code.replace(
  '<AppleIcon className="w-5 h-5 text-gray-600" />',
  '<AppleIcon className="w-5 h-5 text-gray-600 dark:text-gray-400" />'
);
code = code.replace(
  '<AppleIcon className="w-4 h-4 text-gray-600" />',
  '<AppleIcon className="w-4 h-4 text-gray-600 dark:text-gray-400" />'
);

fs.writeFileSync('src/renderer/pages/Devices.tsx', code);
console.log('Fixed dark mode for Agent Installation cards');
