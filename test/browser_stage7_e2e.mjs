import { spawn } from 'node:child_process';
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

const PORT = 9225;
const chromeProfile = `/tmp/chrome-goto-stage7-${Date.now()}`;
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
    await sleep(500);
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
  const targetHost = process.env.TARGET_HOST || 'http://127.0.0.1:8995';
  console.log(`Starting Stage 7 E2E Browser Journey against ${targetHost}...`);

  try {
    const wsUrl = await getCDP();
    const cdp = new CDPClient(wsUrl);
    await cdp.connect();
    await cdp.send('Page.enable');
    await cdp.send('Runtime.enable');

    // 1. Visit management UI while unauthenticated
    console.log('Step 1: Visiting management UI while unauthenticated...');
    await cdp.navigate(`${targetHost}/admin`);
    await sleep(600);

    // 2. Verify OIDC login occurs
    let currentURL = await cdp.eval('window.location.href');
    console.log('Step 2: Current URL after unauthenticated access:', currentURL);
    if (!currentURL.includes('/authorize')) {
      throw new Error(`Expected redirect to OIDC /authorize, got: ${currentURL}`);
    }

    // 3. Complete login through test identity provider
    console.log('Step 3: Completing login through IdP as engineer@example.com...');
    await cdp.eval(`document.getElementById('login-submit').click();`);
    await sleep(800);

    // 4. Verify management UI loads
    currentURL = await cdp.eval('window.location.href');
    console.log('Step 4: Current URL after OIDC callback:', currentURL);
    if (!currentURL.includes('/admin')) {
      throw new Error(`Expected redirect to /admin, got: ${currentURL}`);
    }
    const pageText = await cdp.eval('document.body.innerText');
    if (!pageText.includes('engineer@example.com')) {
      throw new Error('User identity engineer@example.com not displayed on dashboard');
    }
    console.log('Management UI loaded with authenticated user.');

    // 5. Create a short link
    console.log('Step 5: Creating short link "prod-wiki"...');
    await cdp.eval(`
      document.querySelector('input[name="slug"]').value = 'prod-wiki';
      document.querySelector('input[name="target"]').value = 'https://en.wikipedia.org/wiki/Production';
      document.querySelector('form[action="/admin/links"]').submit();
    `);
    await sleep(800);

    // 6. Verify it appears in the list
    console.log('Step 6: Verifying link appears in table...');
    const listText = await cdp.eval('document.body.innerText');
    if (!listText.includes('/prod-wiki') || !listText.includes('https://en.wikipedia.org/wiki/Production')) {
      throw new Error('Created link /prod-wiki not found in link list');
    }
    console.log('Verified: /prod-wiki is present in list.');

    // 7 & 8. Navigate directly to short URL and verify redirect
    console.log('Step 7 & 8: Verifying short link resolution redirect...');
    const res1 = await fetch(`${targetHost}/prod-wiki`, { redirect: 'manual' });
    console.log('Resolution status:', res1.status, 'Location:', res1.headers.get('location'));
    if (res1.status !== 302 || res1.headers.get('location') !== 'https://en.wikipedia.org/wiki/Production') {
      throw new Error(`Expected 302 redirect to Wikipedia production, got status ${res1.status} loc ${res1.headers.get('location')}`);
    }

    // 9. Edit the destination
    console.log('Step 9: Editing destination to https://en.wikipedia.org/wiki/Reliability_engineering...');
    await cdp.eval(`
      const row = Array.from(document.querySelectorAll('tr')).find(r => r.innerText.includes('/prod-wiki'));
      const editForm = row.querySelector('form[action="/admin/links/edit"]');
      editForm.querySelector('input[name="target"]').value = 'https://en.wikipedia.org/wiki/Reliability_engineering';
      editForm.submit();
    `);
    await sleep(800);

    // 10. Verify short URL now reaches new destination
    console.log('Step 10: Verifying changed destination redirect...');
    const res2 = await fetch(`${targetHost}/prod-wiki`, { redirect: 'manual' });
    console.log('Updated resolution status:', res2.status, 'Location:', res2.headers.get('location'));
    if (res2.status !== 302 || res2.headers.get('location') !== 'https://en.wikipedia.org/wiki/Reliability_engineering') {
      throw new Error(`Expected updated redirect, got status ${res2.status} loc ${res2.headers.get('location')}`);
    }

    // 11. Attempt duplicate creation and verify useful error
    console.log('Step 11: Attempting duplicate creation of "prod-wiki"...');
    await cdp.eval(`
      document.querySelector('input[name="slug"]').value = 'prod-wiki';
      document.querySelector('input[name="target"]').value = 'https://another.org';
      document.querySelector('form[action="/admin/links"]').submit();
    `);
    await sleep(800);

    const dupError = await cdp.eval(`document.querySelector('.alert-error')?.innerText || ''`);
    console.log('Duplicate error message displayed:', dupError);
    if (!dupError.includes('already exists')) {
      throw new Error(`Expected duplicate error message, got: ${dupError}`);
    }

    // 12. Delete the link
    console.log('Step 12: Deleting short link "prod-wiki"...');
    await cdp.eval(`
      const row = Array.from(document.querySelectorAll('tr')).find(r => r.innerText.includes('/prod-wiki'));
      const deleteForm = row.querySelector('form[action="/admin/links/delete"]');
      deleteForm.submit();
    `);
    await sleep(800);

    // 13. Verify short URL now returns 404
    console.log('Step 13: Verifying /prod-wiki returns 404...');
    const res3 = await fetch(`${targetHost}/prod-wiki`, { redirect: 'manual' });
    console.log('Deleted resolution status:', res3.status);
    if (res3.status !== 404) {
      throw new Error(`Expected 404 for deleted link, got ${res3.status}`);
    }

    // Capture screenshot before logout
    let artifactDir = '/Users/sgzmd/.gemini/antigravity/brain/b0d34f3b-e7f1-460c-8aa2-fa1c04f8edf7';
    if (!fs.existsSync(artifactDir)) {
      artifactDir = process.env.ARTIFACT_DIR || '/tmp';
    }
    await cdp.screenshot(`${artifactDir}/e2e_acceptance_screenshot.png`);
    console.log(`Saved screenshot artifact to ${artifactDir}/e2e_acceptance_screenshot.png`);

    // 14. Log out
    console.log('Step 14: Logging out...');
    await cdp.eval(`document.querySelector('form[action="/auth/logout"] button').click();`);
    await sleep(800);

    // 15. Verify management UI again requires authentication
    console.log('Step 15: Verifying /admin requires authentication after logout...');
    await cdp.navigate(`${targetHost}/admin`);
    await sleep(600);
    currentURL = await cdp.eval('window.location.href');
    console.log('Current URL when visiting /admin after logout:', currentURL);
    if (!currentURL.includes('/authorize')) {
      throw new Error(`Expected redirect to /authorize after logout, got: ${currentURL}`);
    }

    cdp.close();
    console.log('ALL 15 STEPS OF STAGE 7 E2E BROWSER ACCEPTANCE PASSED SUCCESSFULLY!');
  } finally {
    try { chrome.kill('SIGKILL'); } catch (_) {}
    try { fs.rmSync(chromeProfile, { recursive: true, force: true }); } catch (_) {}
  }
}

run().catch(err => {
  console.error('E2E Browser Test Failed:', err);
  try { chrome.kill('SIGKILL'); } catch (_) {}
  try { fs.rmSync(chromeProfile, { recursive: true, force: true }); } catch (_) {}
  process.exit(1);
});
