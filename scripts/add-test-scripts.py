import json

package_file = 'D:/Projects/Sentinel/package.json'

with open(package_file, 'r', encoding='utf-8') as f:
    package = json.load(f)

# Add test scripts
package['scripts']['test'] = 'vitest run'
package['scripts']['test:watch'] = 'vitest'
package['scripts']['test:coverage'] = 'vitest run --coverage'
package['scripts']['test:ui'] = 'vitest --ui'

with open(package_file, 'w', encoding='utf-8') as f:
    json.dump(package, f, indent=2)

print('Added test scripts to package.json')
