import { chromium } from 'playwright';
import { createServer } from 'http';
import { readFile, writeFile } from 'fs/promises';
import { join, extname } from 'path';
import pixelmatch from 'pixelmatch';
import { PNG } from 'pngjs';

const MIME = {
  '.html': 'text/html',
  '.js': 'application/javascript',
  '.svg': 'image/svg+xml',
  '.png': 'image/png',
  '.css': 'text/css',
  '.ttf': 'font/ttf',
};

const PIXEL_THRESHOLD = 0.3;
const FAIL_PERCENT = 5.0;

const root = process.cwd();
const server = createServer(async (req, res) => {
  const filePath = join(root, req.url === '/' ? 'index.html' : req.url);
  try {
    const data = await readFile(filePath);
    res.writeHead(200, { 'Content-Type': MIME[extname(filePath)] || 'application/octet-stream' });
    res.end(data);
  } catch {
    res.writeHead(404);
    res.end('Not found');
  }
});

await new Promise(resolve => server.listen(0, resolve));
const port = server.address().port;

const browser = await chromium.launch();
const page = await browser.newPage({
  viewport: { width: 800, height: 1800 },
  deviceScaleFactor: 2,
});
await page.goto(`http://localhost:${port}/`);
await page.waitForSelector('.qr-container svg image', { timeout: 10000 });
await page.waitForSelector('.qr-container svg rect + image', { timeout: 10000 });

// Wait for download buttons to be enabled (signals all QR codes + logo inlining ready)
await page.waitForFunction(() =>
  [...document.querySelectorAll('.download-btn')].every(b => !b.disabled),
  { timeout: 10000 }
);

const cardNames = ['chessreel', 'chessparty', 'chessreel-dark', 'chessparty-dark'];
let failures = 0;

for (const name of cardNames) {
  // 1. Capture HTML card screenshot
  const card = page.locator('.card').filter({
    has: page.locator(`#qr-${name}`)
  });
  const htmlPngBuffer = await card.screenshot();

  // 2. Generate PDF via download button click
  const [download] = await Promise.all([
    page.waitForEvent('download'),
    page.click(`#btn-${name}`),
  ]);
  const pdfPath = await download.path();
  const pdfBytes = await readFile(pdfPath);

  // 3. Render PDF to image in-browser using pdf.js
  const htmlPng = PNG.sync.read(htmlPngBuffer);
  const targetWidth = htmlPng.width;
  const targetHeight = htmlPng.height;

  const isDark = name.endsWith('-dark');
  const pdfDataUrl = await page.evaluate(async ({ pdfBase64, targetWidth, targetHeight, isDark }) => {
    // Load pdf.js from CDN
    if (!window.pdfjsLib) {
      await new Promise((resolve, reject) => {
        const script = document.createElement('script');
        script.src = 'https://cdn.jsdelivr.net/npm/pdfjs-dist@3.11.174/build/pdf.min.js';
        script.onload = resolve;
        script.onerror = reject;
        document.head.appendChild(script);
      });
      window.pdfjsLib.GlobalWorkerOptions.workerSrc =
        'https://cdn.jsdelivr.net/npm/pdfjs-dist@3.11.174/build/pdf.worker.min.js';
    }

    const pdfData = Uint8Array.from(atob(pdfBase64), c => c.charCodeAt(0));
    const pdf = await window.pdfjsLib.getDocument({ data: pdfData }).promise;
    const pdfPage = await pdf.getPage(1);

    // Render at high fixed scale for precision, then crop and resize
    const RENDER_SCALE = 8;
    const viewport = pdfPage.getViewport({ scale: RENDER_SCALE });

    const canvas = document.createElement('canvas');
    canvas.width = viewport.width;
    canvas.height = viewport.height;
    const ctx = canvas.getContext('2d');
    ctx.fillStyle = '#ffffff';
    ctx.fillRect(0, 0, canvas.width, canvas.height);
    await pdfPage.render({ canvasContext: ctx, viewport }).promise;

    // Bleed is 3mm on each side. Page is 96×61mm, trim is 90×55mm.
    // Crop the bleed at render resolution, then scale to target dimensions.
    const bleedFracX = 3 / 96;
    const bleedFracY = 3 / 61;
    const cropX = Math.round(canvas.width * bleedFracX);
    const cropY = Math.round(canvas.height * bleedFracY);
    const cropW = Math.round(canvas.width * (1 - 2 * bleedFracX));
    const cropH = Math.round(canvas.height * (1 - 2 * bleedFracY));

    // Draw cropped trim area scaled to match HTML screenshot dimensions
    const outCanvas = document.createElement('canvas');
    outCanvas.width = targetWidth;
    outCanvas.height = targetHeight;
    const outCtx = outCanvas.getContext('2d');
    outCtx.fillStyle = '#ffffff';
    outCtx.fillRect(0, 0, targetWidth, targetHeight);
    outCtx.drawImage(canvas, cropX, cropY, cropW, cropH,
                     0, 0, targetWidth, targetHeight);

    return outCanvas.toDataURL('image/png');
  }, {
    pdfBase64: pdfBytes.toString('base64'),
    targetWidth,
    targetHeight,
    isDark,
  });

  // 4. Decode PDF PNG
  const pdfPngBase64 = pdfDataUrl.replace(/^data:image\/png;base64,/, '');
  const pdfPngBuffer = Buffer.from(pdfPngBase64, 'base64');
  const pdfPng = PNG.sync.read(pdfPngBuffer);

  // Ensure dimensions match
  const width = Math.min(htmlPng.width, pdfPng.width);
  const height = Math.min(htmlPng.height, pdfPng.height);

  const htmlImg = new PNG({ width, height });
  const pdfImg = new PNG({ width, height });
  PNG.bitblt(htmlPng, htmlImg, 0, 0, width, height, 0, 0);
  PNG.bitblt(pdfPng, pdfImg, 0, 0, width, height, 0, 0);

  // 5. Compare with pixelmatch — direct 1:1 comparison, no alignment needed
  const diff = new PNG({ width, height });
  const numDiffPixels = pixelmatch(
    htmlImg.data, pdfImg.data, diff.data,
    width, height,
    { threshold: PIXEL_THRESHOLD }
  );

  const totalPixels = width * height;
  const diffPercent = (numDiffPixels / totalPixels) * 100;

  if (diffPercent > FAIL_PERCENT) {
    console.log(`FAIL: ${name} (diff: ${diffPercent.toFixed(1)}% > ${FAIL_PERCENT.toFixed(1)}%)`);
    await writeFile(`/tmp/vrt-${name}-html.png`, PNG.sync.write(htmlImg));
    await writeFile(`/tmp/vrt-${name}-pdf.png`, PNG.sync.write(pdfImg));
    await writeFile(`/tmp/vrt-${name}-diff.png`, PNG.sync.write(diff));
    console.log(`  Debug images: /tmp/vrt-${name}-{html,pdf,diff}.png`);
    failures++;
  } else {
    console.log(`PASS: ${name} (diff: ${diffPercent.toFixed(1)}%)`);
  }
}

await browser.close();
server.close();
process.exit(failures > 0 ? 1 : 0);
