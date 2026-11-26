// Takes the UI screenshots for the README and the docs, in light and dark.
//
//   make screenshots
//
// Needs Google Chrome and two running installations:
//   - the development stack with demo data (make dev-up dev-seed run) for most pages,
//   - the end-to-end cluster after a test run (make e2e-up e2e) for a real
//     deployment log and audit log.
// Users and the password "dev" come from dev/keycloak-realm.json.
import { chromium } from 'playwright-core';

const out = process.argv[2] ?? '../docs/assets/screenshots';
const dev = 'http://127.0.0.1:8080';
const e2e = 'http://127.0.0.1:18080';

async function login(browser, base, user, colorScheme) {
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 800 }, deviceScaleFactor: 2, colorScheme });
  const page = await ctx.newPage();
  await page.goto(base + '/');
  await page.fill('#username', user);
  await page.fill('#password', 'dev');
  await page.click('#kc-login');
  await page.waitForURL(base + '/**');
  await page.waitForLoadState('networkidle');
  return { page, base };
}

async function shot({ page, base }, path, name, prepare) {
  await page.goto(base + path);
  await page.waitForLoadState('networkidle');
  if (prepare) await prepare(page);
  await page.waitForTimeout(400); // let transitions settle
  await page.screenshot({ path: `${out}/${name}.png` });
  console.log(name);
}

const openDeployDialog = async (page) => {
  // The newest main build changes the Helm values, so the dialog shows a diff.
  await page.getByRole('tab', { name: 'Branches' }).click();
  await page.waitForLoadState('networkidle');
  // The first artifact that is ready for demo-test; ready targets are plain chips, not details.
  const row = page.locator('li.artifact').filter({ has: page.locator('span.chip', { hasText: 'demo-test' }) }).first();
  await row.locator('.deploy-menu > button').click();
  await row.locator('button[aria-label="Deploy to demo-test"]').click();
  await page.waitForSelector('dialog[open]');
};
const openUserMenu = (page) => page.locator('header button').last().click();

const chrome = await chromium.launch({ channel: 'chrome' });
// The e2e issuer is http://keycloak:18081, reachable from the host on 127.0.0.1:18081.
const chromeE2E = await chromium.launch({
  channel: 'chrome',
  args: ['--host-resolver-rules=MAP keycloak:18081 127.0.0.1:18081'],
});
try {
  for (const scheme of ['light', 'dark']) {
    const suffix = scheme === 'dark' ? '-dark' : '';
    const mia = await login(chrome, dev, 'mia', scheme);
    await shot(mia, '/', 'projects' + suffix);
    await shot(mia, '/projects/example/demo', 'project' + suffix);
    await shot(mia, '/targets/demo-test', 'target' + suffix);
    await shot(mia, '/projects/example/demo', 'deploy-dialog' + suffix, openDeployDialog);
    await shot(mia, '/', 'user-menu' + suffix, openUserMenu);

    const root = await login(chromeE2E, e2e, 'root', scheme);
    await shot(root, '/deployments/1', 'deployment' + suffix);
    await shot(root, '/admin', 'admin' + suffix);
  }
} finally {
  await chrome.close();
  await chromeE2E.close();
}
