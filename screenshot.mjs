import { chromium } from 'playwright';
import { createServer } from 'http';
import { readFile } from 'fs/promises';
import { join, extname } from 'path';

const MIME = {
  '.html': 'text/html',
  '.js': 'application/javascript',
  '.svg': 'image/svg+xml',
  '.png': 'image/png',
  '.css': 'text/css',
};

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
  viewport: { width: 800, height: 900 },
  deviceScaleFactor: 2,
});
await page.goto(`http://localhost:${port}/`);
await page.waitForSelector('.qr-container svg image', { timeout: 10000 });
// Wait for logo background rect to be inserted by the polling script
await page.waitForSelector('.qr-container svg rect + image', { timeout: 10000 });
await page.screenshot({ path: '/tmp/preview.png', fullPage: true });
await browser.close();
server.close();
console.log('Screenshot saved to /tmp/preview.png');
