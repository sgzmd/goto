import { spawn } from 'node:child_process';
import fs from 'node:fs';

const chromeProfile = `/tmp/chrome-goto-stage4-${Date.now()}`;
const chrome = spawn('/Applications/Google Chrome.app/Contents/MacOS/Google Chrome', [
  '--headless',
  '--disable-gpu',
  '--remote-debugging-port=9224',
  `--user-data-dir=${chromeProfile}`,
  'about:blank'
], { stdio: 'ignore' });

async function sleep(ms) {
  return new Promise(r => setTimeout(r, ms));
}

async function getCDP() {
  for (let i = 0; i < 20; i++) {
    try {
      const res = await fetch('http://127.0.0.1:9224/json/list');
      if (res.ok) {
        const data = await res.json();
        const page = data.find(t => t.type === 'page');
        if (page && page.webSocketDebuggerUrl) {
          return page.webSocketDebuggerUrl;
        }
      }
    } catch (e) {}
    await sleep(200);
  }
  throw new Error('Chrome CDP page target did not become ready');
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
  try {
    const wsUrl = await getCDP();
    const cdp = new CDPClient(wsUrl);
    await cdp.connect();
    await cdp.send('Page.enable');
    await cdp.send('Runtime.enable');

    console.log('1. Navigating to protected /admin...');
    await cdp.navigate('http://127.0.0.1:8990/admin');
    await sleep(600);

    let currentURL = await cdp.eval('window.location.href');
    console.log('Current URL after navigating to /admin:', currentURL);
    if (!currentURL.includes('/authorize')) {
      throw new Error(`Expected redirect to OIDC authorize endpoint, got ${currentURL}`);
    }

    console.log('2. Submitting mock OIDC login form as engineer@example.com...');
    await cdp.eval(`document.getElementById('login-submit').click();`);
    await sleep(800);

    currentURL = await cdp.eval('window.location.href');
    console.log('Current URL after OIDC callback:', currentURL);
    if (!currentURL.includes('/admin')) {
      throw new Error(`Expected redirect back to /admin, got ${currentURL}`);
    }

    const bodyText = await cdp.eval('document.body.innerText');
    if (!bodyText.includes('engineer@example.com')) {
      throw new Error('Authenticated user email not found on admin page');
    }
    console.log('User successfully authenticated as engineer@example.com.');

    console.log('3. Creating link "team" -> "https://example.com/team" via authenticated UI...');
    await cdp.eval(`
      document.querySelector('input[name="slug"]').value = 'team';
      document.querySelector('input[name="target"]').value = 'https://example.com/team';
      document.querySelector('form[action="/admin/links"]').submit();
    `);
    await sleep(800);

    const updatedText = await cdp.eval('document.body.innerText');
    if (!updatedText.includes('/team')) {
      throw new Error('Created link /team not found in admin table');
    }
    console.log('Link /team created and listed.');

    console.log('4. Logging out...');
    await cdp.eval(`document.querySelector('form[action="/auth/logout"] button').click();`);
    await sleep(800);

    currentURL = await cdp.eval('window.location.href');
    console.log('Current URL after logout:', currentURL);

    console.log('5. Verifying /admin now redirects to OIDC login again...');
    await cdp.navigate('http://127.0.0.1:8990/admin');
    await sleep(600);
    currentURL = await cdp.eval('window.location.href');
    console.log('Current URL when accessing /admin logged out:', currentURL);
    if (!currentURL.includes('/authorize')) {
      throw new Error(`Expected redirect to /authorize after logout, got ${currentURL}`);
    }

    await cdp.screenshot('/tmp/goto-stage4-auth.png');
    console.log('Saved screenshot to /tmp/goto-stage4-auth.png');

    cdp.close();
    console.log('STAGE 4 BROWSER OIDC VERIFICATION SUCCESSFUL!');
  } finally {
    chrome.kill('SIGKILL');
  }
}

run().catch(err => {
  console.error('Test failed:', err);
  chrome.kill('SIGKILL');
  process.exit(1);
});
