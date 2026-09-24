import { spawn } from 'node:child_process';
import http from 'node:http';
import fs from 'node:fs';

function findChrome() {
  if (process.env.CHROME_BIN) return process.env.CHROME_BIN;
  const candidates = [
    '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
    '/usr/bin/google-chrome',
    '/usr/bin/google-chrome-stable',
    '/usr/bin/chromium',
    '/usr/bin/chromium-browser'
  ];
  for (const c of candidates) {
    if (fs.existsSync(c)) return c;
  }
  return 'google-chrome';
}

// Launch chrome
const PORT = 9223;
const chromeProfile = `/tmp/chrome-goto-stage3-${Date.now()}`;
let chromeStderr = '';
const chrome = spawn(findChrome(), [
  '--headless',
  '--disable-gpu',
  '--no-sandbox',
  '--disable-setuid-sandbox',
  '--disable-dev-shm-usage',
  '--no-first-run',
  '--no-default-browser-check',
  '--remote-debugging-address=127.0.0.1',
  `--remote-debugging-port=${PORT}`,
  `--user-data-dir=${chromeProfile}`,
  'about:blank'
], { stdio: ['ignore', 'ignore', 'pipe'] });

if (chrome.stderr) {
  chrome.stderr.on('data', chunk => {
    chromeStderr += chunk.toString();
  });
}

async function sleep(ms) {
  return new Promise(r => setTimeout(r, ms));
}

async function getCDP() {
  let lastErr = null;
  for (let i = 0; i < 50; i++) {
    try {
      const res = await fetch(`http://127.0.0.1:${PORT}/json/list`);
      if (res.ok) {
        const data = await res.json();
        const page = data.find(t => t.type === 'page');
        if (page && page.webSocketDebuggerUrl) {
          return page.webSocketDebuggerUrl;
        }
        try {
          const newRes = await fetch(`http://127.0.0.1:${PORT}/json/new`, { method: 'PUT' });
          if (newRes.ok) {
            const newTarget = await newRes.json();
            if (newTarget && newTarget.webSocketDebuggerUrl) {
              return newTarget.webSocketDebuggerUrl;
            }
          }
        } catch (_) {}
      }
    } catch (e) {
      lastErr = e;
    }
    await sleep(200);
  }
  throw new Error(`Chrome CDP page target did not become ready (last error: ${lastErr ? lastErr.message : 'none'}, stderr: ${chromeStderr})`);
}

class CDPClient {
  constructor(wsUrl) {
    this.ws = new WebSocket(wsUrl);
    this.id = 1;
    this.callbacks = new Map();
  }

  async connect() {
    return new Promise((resolve, reject) => {
      this.ws.onopen = resolve;
      this.ws.onerror = reject;
      this.ws.onmessage = (event) => {
        const msg = JSON.parse(event.data);
        if (msg.id && this.callbacks.has(msg.id)) {
          const { resolve, reject } = this.callbacks.get(msg.id);
          this.callbacks.delete(msg.id);
          if (msg.error) reject(new Error(msg.error.message));
          else resolve(msg.result);
        }
      };
    });
  }

  async send(method, params = {}) {
    const id = this.id++;
    return new Promise((resolve, reject) => {
      this.callbacks.set(id, { resolve, reject });
      this.ws.send(JSON.stringify({ id, method, params }));
    });
  }

  async eval(expression) {
    const res = await this.send('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true });
    if (res.exceptionDetails) {
      throw new Error(`Eval failed: ${JSON.stringify(res.exceptionDetails)}`);
    }
    return res.result?.value;
  }

  async navigate(url) {
    await this.send('Page.navigate', { url });
    await sleep(400);
  }

  async screenshot(path) {
    const res = await this.send('Page.captureScreenshot', { format: 'png' });
    fs.writeFileSync(path, Buffer.from(res.data, 'base64'));
  }

  close() {
    this.ws.close();
  }
}

async function run() {
  try {
    const wsUrl = await getCDP();
    const cdp = new CDPClient(wsUrl);
    await cdp.connect();
    await cdp.send('Page.enable');
    await cdp.send('Runtime.enable');

    console.log('1. Loading management page http://127.0.0.1:8989/admin...');
    await cdp.navigate('http://127.0.0.1:8989/admin');
    let title = await cdp.eval('document.title');
    console.log('Page title:', title);
    if (title !== 'Goto Links') throw new Error(`Unexpected title: ${title}`);

    console.log('2. Creating short link "docs" -> "https://example.com/docs"...');
    await cdp.eval(`
      document.querySelector('input[name="slug"]').value = 'docs';
      document.querySelector('input[name="target"]').value = 'https://example.com/docs';
      document.querySelector('form[action="/admin/links"]').submit();
    `);
    await sleep(600);

    const linksList = await cdp.eval(`document.body.innerText`);
    if (!linksList.includes('/docs')) throw new Error('Expected /docs in link table');
    console.log('Link created and displayed in table.');

    console.log('3. Following /docs to verify redirect...');
    // We can fetch /docs with redirect: manual to verify 302 location
    const redirRes = await fetch('http://127.0.0.1:8989/docs', { redirect: 'manual' });
    console.log('Redirect status:', redirRes.status, 'Location:', redirRes.headers.get('location'));
    if (redirRes.status !== 302 || redirRes.headers.get('location') !== 'https://example.com/docs') {
      throw new Error('Redirect did not match expected target');
    }

    console.log('4. Editing target of "docs" to "https://example.com/v2"...');
    await cdp.eval(`
      const row = Array.from(document.querySelectorAll('tr')).find(r => r.innerText.includes('/docs'));
      const editForm = row.querySelector('form[action="/admin/links/edit"]');
      editForm.querySelector('input[name="target"]').value = 'https://example.com/v2';
      editForm.submit();
    `);
    await sleep(600);

    console.log('5. Verifying changed redirect...');
    const redirRes2 = await fetch('http://127.0.0.1:8989/docs', { redirect: 'manual' });
    console.log('Redirect status:', redirRes2.status, 'Location:', redirRes2.headers.get('location'));
    if (redirRes2.status !== 302 || redirRes2.headers.get('location') !== 'https://example.com/v2') {
      throw new Error('Updated redirect did not match expected target');
    }

    console.log('6. Deleting link "docs"...');
    await cdp.eval(`
      const row = Array.from(document.querySelectorAll('tr')).find(r => r.innerText.includes('/docs'));
      const deleteForm = row.querySelector('form[action="/admin/links/delete"]');
      deleteForm.submit();
    `);
    await sleep(600);

    console.log('7. Verifying /docs now returns 404...');
    const notFoundRes = await fetch('http://127.0.0.1:8989/docs', { redirect: 'manual' });
    console.log('Status code for deleted link:', notFoundRes.status);
    if (notFoundRes.status !== 404) {
      throw new Error(`Expected 404 for deleted link, got ${notFoundRes.status}`);
    }

    await cdp.screenshot('/tmp/goto-stage3-admin.png');
    console.log('Captured screenshot to /tmp/goto-stage3-admin.png');

    cdp.close();
    console.log('STAGE 3 BROWSER VERIFICATION SUCCESSFUL!');
  } finally {
    try { chrome.kill('SIGKILL'); } catch (_) {}
    try { fs.rmSync(chromeProfile, { recursive: true, force: true }); } catch (_) {}
  }
}

run().catch(err => {
  console.error('Test failed:', err);
  try { chrome.kill('SIGKILL'); } catch (_) {}
  try { fs.rmSync(chromeProfile, { recursive: true, force: true }); } catch (_) {}
  process.exit(1);
});
