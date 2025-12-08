const sharp = require('sharp');
const path = require('path');
const fs = require('fs');

const assetsDir = path.join(__dirname, '..', 'assets');
const svgPath = path.join(assetsDir, 'icon.svg');

async function generateIcons() {
  const svgBuffer = fs.readFileSync(svgPath);

  // Generate PNG at 256x256
  await sharp(svgBuffer)
    .resize(256, 256)
    .png()
    .toFile(path.join(assetsDir, 'icon.png'));

  console.log('Created icon.png (256x256)');

  // Generate multiple sizes for ICO
  const sizes = [16, 32, 48, 64, 128, 256];
  const pngBuffers = [];

  for (const size of sizes) {
    const buffer = await sharp(svgBuffer)
      .resize(size, size)
      .png()
      .toBuffer();
    pngBuffers.push({ size, buffer });
    console.log(`Generated ${size}x${size} PNG buffer`);
  }

  // Create ICO file manually (simplified ICO format)
  // ICO format: header + directory entries + image data
  const iconDir = Buffer.alloc(6 + sizes.length * 16);

  // ICO header
  iconDir.writeUInt16LE(0, 0); // Reserved, always 0
  iconDir.writeUInt16LE(1, 2); // Type: 1 for ICO
  iconDir.writeUInt16LE(sizes.length, 4); // Number of images

  let dataOffset = 6 + sizes.length * 16;
  const imageDataBuffers = [];

  for (let i = 0; i < pngBuffers.length; i++) {
    const { size, buffer } = pngBuffers[i];
    const entryOffset = 6 + i * 16;

    // Directory entry
    iconDir.writeUInt8(size === 256 ? 0 : size, entryOffset); // Width (0 = 256)
    iconDir.writeUInt8(size === 256 ? 0 : size, entryOffset + 1); // Height
    iconDir.writeUInt8(0, entryOffset + 2); // Color palette
    iconDir.writeUInt8(0, entryOffset + 3); // Reserved
    iconDir.writeUInt16LE(1, entryOffset + 4); // Color planes
    iconDir.writeUInt16LE(32, entryOffset + 6); // Bits per pixel
    iconDir.writeUInt32LE(buffer.length, entryOffset + 8); // Image size
    iconDir.writeUInt32LE(dataOffset, entryOffset + 12); // Image offset

    imageDataBuffers.push(buffer);
    dataOffset += buffer.length;
  }

  // Combine header and image data
  const icoBuffer = Buffer.concat([iconDir, ...imageDataBuffers]);
  fs.writeFileSync(path.join(assetsDir, 'icon.ico'), icoBuffer);

  console.log('Created icon.ico');
  console.log('Icon generation complete!');
}

generateIcons().catch(console.error);
