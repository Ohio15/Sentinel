const sharp = require('sharp');
const path = require('path');
const fs = require('fs');

const inputSvg = path.join(__dirname, '../assets/icon.svg');
const outputDir = path.join(__dirname, '../assets');
const pngDir = path.join(outputDir, 'icons/png');

// Ensure directories exist
fs.mkdirSync(pngDir, { recursive: true });

// PNG sizes needed for electron-icon-builder
const sizes = [16, 24, 32, 48, 64, 128, 256, 512, 1024];

async function generateIcons() {
    console.log('Generating icons from SVG...');
    
    const svgBuffer = fs.readFileSync(inputSvg);
    
    // Generate main PNG (256x256)
    await sharp(svgBuffer)
        .resize(256, 256)
        .png()
        .toFile(path.join(outputDir, 'icon.png'));
    console.log('Generated icon.png (256x256)');
    
    // Generate all PNG sizes
    for (const size of sizes) {
        await sharp(svgBuffer)
            .resize(size, size)
            .png()
            .toFile(path.join(pngDir, `${size}x${size}.png`));
        console.log(`Generated ${size}x${size}.png`);
    }
    
    // Generate ICO (requires png2icons or we use electron-icon-builder)
    console.log('\nPNG files generated. Now generating ICO...');
}

generateIcons().then(() => {
    console.log('\nAll PNG icons generated!');
    console.log('Running electron-icon-builder to generate ICO and ICNS...');
}).catch(err => {
    console.error('Error:', err);
    process.exit(1);
});
