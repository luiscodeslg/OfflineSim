// Renders the app icon to PNG and wraps it in an .ico (PNG-in-ICO, Vista+).
// Usage: node testdata/make_icon.js
const { chromium } = require('playwright');
const fs = require('fs');
const path = require('path');

const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32" width="256" height="256"><rect x="1" y="1" width="30" height="30" rx="7" fill="#15181e"/><path d="M16 4 6 9v6.5c0 6 4.2 10.3 10 12 5.8-1.7 10-6 10-12V9z" fill="#f5a623"/><path d="M11 16.5l3.5 3.5 7-8" stroke="#15181e" stroke-width="2.6" fill="none" stroke-linecap="round" stroke-linejoin="round"/></svg>`;

(async () => {
  const root = path.join(__dirname, '..');
  const b = await chromium.launch();
  const p = await b.newPage({ viewport: { width: 256, height: 256 } });
  await p.setContent(`<html><body style="margin:0;background:transparent">${svg}</body></html>`);
  const png = await p.screenshot({ omitBackground: true });
  await b.close();
  fs.writeFileSync(path.join(root, 'web', 'icon.png'), png);
  const hdr = Buffer.alloc(22);
  hdr.writeUInt16LE(0, 0); hdr.writeUInt16LE(1, 2); hdr.writeUInt16LE(1, 4);
  hdr.writeUInt8(0, 6); hdr.writeUInt8(0, 7); hdr.writeUInt8(0, 8); hdr.writeUInt8(0, 9);
  hdr.writeUInt16LE(1, 10); hdr.writeUInt16LE(32, 12); hdr.writeUInt32LE(png.length, 14); hdr.writeUInt32LE(22, 18);
  fs.writeFileSync(path.join(root, 'icon.ico'), Buffer.concat([hdr, png]));
})();
