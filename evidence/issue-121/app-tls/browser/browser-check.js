// Browser sign-in check for the security lab (ADR 0008), run in headless
// Chromium (Playwright 1.49.1) inside a container on the lab's edge network.
// The lab CA is trusted through Chromium's NSS database ($HOME/.pki/nssdb);
// no certificate error is ignored. Values of passwords, cookies, codes, and
// tokens are never recorded.
'use strict';
const fs = require('fs');
const { chromium } = require('playwright');

const APP = 'https://work-items.localhost:9443';
const OUT = process.env.OUT_DIR;
const results = [];
const requests = [];
const consoleErrors = [];

function record(area, check, expected, observed) {
  const result = expected === 'observe' ? 'info' : (String(observed) === String(expected) ? 'pass' : 'fail');
  results.push({ vantage: area, check, expected: String(expected), observed: String(observed), result });
  console.log(`${result.toUpperCase().padEnd(4)} ${area.padEnd(28)} ${check} expected=${expected} observed=${observed}`);
}

// Path without the query string (which can carry codes and state).
function pathOf(url) {
  const u = new URL(url);
  return `${u.host}${u.pathname}`;
}

function watch(page, label) {
  page.on('response', (response) => {
    const url = response.url();
    if (!/work-items\.localhost/.test(url)) return;
    requests.push({ session: label, method: response.request().method(), path: pathOf(url), status: response.status() });
  });
  page.on('console', (msg) => { if (msg.type() === 'error') consoleErrors.push(`${label}: ${msg.text().slice(0, 200)}`); });
}

async function signIn(page, user, password) {
  await page.goto(`${APP}/`);
  await page.getByRole('link', { name: 'Sign in' }).first().click();
  await page.waitForSelector('#kc-form-login', { timeout: 20000 });
  const loginHost = new URL(page.url()).host;
  await page.fill('#username', user);
  await page.fill('#password', password);
  await Promise.all([
    page.waitForURL(`${APP}/`, { timeout: 30000 }),
    page.click('#kc-login'),
  ]);
  return loginHost;
}

(async () => {
  const userPassword = fs.readFileSync('/secrets/lab-user-password', 'utf8').trim();
  const viewerPassword = fs.readFileSync('/secrets/lab-viewer-password', 'utf8').trim();
  const browser = await chromium.launch();
  record('environment', 'browser', 'observe', `Chromium ${browser.version()} (headless, Playwright 1.49.1)`);

  // --- lab-user: sign-in, list, create, process, sign-out -------------------
  const context = await browser.newContext();
  const page = await context.newPage();
  watch(page, 'lab-user');

  const first = await page.goto(`${APP}/`);
  const tls = await first.securityDetails();
  record('public TLS', 'UI document status', '200', first.status());
  record('public TLS', 'certificate issuer (verified by Chromium against the lab CA)', 'observe', tls ? tls.issuer : 'none');
  record('public TLS', 'certificate subject', 'observe', tls ? tls.subjectName : 'none');
  record('public TLS', 'protocol', 'observe', tls ? tls.protocol : 'none');
  await page.getByText('Sign in required').first().waitFor({ timeout: 15000 });
  record('lab-user', 'before sign-in: UI shows "Sign in required"', 'yes', 'yes');

  const loginHost = await signIn(page, 'lab-user', userPassword);
  record('lab-user', 'login form served from', 'auth.work-items.localhost:9443', loginHost);
  await page.getByRole('button', { name: 'Sign out' }).waitFor({ timeout: 15000 });
  record('lab-user', 'after sign-in: back on the UI with a session (Sign out shown)', 'yes', 'yes');
  record('lab-user', 'signed-in identity shown', 'observe', (await page.locator('span.text-sm.text-muted-foreground').first().innerText()).trim());
  await page.screenshot({ path: `${OUT}/1-signed-in.png` });

  const cookies = await context.cookies();
  const session = cookies.find((c) => c.name === '__Host-wi-session');
  record('lab-user', 'session cookie attributes (no value recorded)', 'secure=true httpOnly=true sameSite=Lax path=/',
    session ? `secure=${session.secure} httpOnly=${session.httpOnly} sameSite=${session.sameSite} path=${session.path}` : 'missing');
  record('lab-user', 'session cookie host-only for work-items.localhost', 'work-items.localhost', session ? session.domain : 'missing');

  const title = `browser check ${new Date().toISOString()}`;
  await page.getByRole('button', { name: 'New Work Item' }).click();
  await page.getByLabel('Work Item title').fill(title);
  const created = page.waitForResponse((r) => r.url().endsWith('/api/items') && r.request().method() === 'POST');
  await page.getByRole('button', { name: 'Create', exact: true }).click();
  record('lab-user', 'create through the UI: POST /api/items', '201', (await created).status());
  const row = page.getByRole('row').filter({ hasText: title });
  await row.waitFor({ timeout: 15000 });
  record('lab-user', 'new Work Item listed', 'yes', 'yes');

  const processed = page.waitForResponse((r) => /\/api\/items\/\d+\/process$/.test(r.url()));
  await row.getByRole('button', { name: 'Process' }).click();
  record('lab-user', 'process through the UI: POST /api/items/{id}/process', '202', (await processed).status());
  await row.getByText('done', { exact: true }).waitFor({ timeout: 30000 });
  record('lab-user', 'Work Item status after processing', 'done', 'done');
  await page.screenshot({ path: `${OUT}/2-created-and-processed.png` });

  const signedOut = page.waitForResponse((r) => r.url().endsWith('/oauth2/sign_out'));
  await page.getByRole('button', { name: 'Sign out' }).click();
  const signOutResponse = await signedOut;
  record('lab-user', 'sign-out through the UI: POST /oauth2/sign_out', 'observe', signOutResponse.status());
  await page.getByText('Sign in required').first().waitFor({ timeout: 15000 });
  record('lab-user', 'after sign-out: UI shows "Sign in required"', 'yes', 'yes');
  record('lab-user', 'after sign-out: session cookie removed', 'yes',
    (await context.cookies()).some((c) => c.name === '__Host-wi-session') ? 'no' : 'yes');
  await page.getByRole('link', { name: 'Sign in' }).first().click();
  await page.waitForSelector('#kc-form-login', { timeout: 20000 });
  record('lab-user', 'after sign-out: Keycloak asks for the password again', 'yes', 'yes');
  await page.screenshot({ path: `${OUT}/3-signed-out.png` });
  await context.close();

  // --- lab-viewer: authorization through the UI -------------------------------
  const viewerContext = await browser.newContext();
  const viewer = await viewerContext.newPage();
  watch(viewer, 'lab-viewer');
  await signIn(viewer, 'lab-viewer', viewerPassword);
  await viewer.getByRole('button', { name: 'Sign out' }).waitFor({ timeout: 15000 });
  record('lab-viewer', 'signed in; Work Items listed (GET /api/items)', 'yes',
    requests.some((r) => r.session === 'lab-viewer' && r.path === 'work-items.localhost:9443/api/items' && r.status === 200) ? 'yes' : 'no');
  await viewer.getByRole('button', { name: 'New Work Item' }).click();
  await viewer.getByLabel('Work Item title').fill('viewer attempt');
  const denied = viewer.waitForResponse((r) => r.url().endsWith('/api/items') && r.request().method() === 'POST');
  await viewer.getByRole('button', { name: 'Create', exact: true }).click();
  record('lab-viewer', 'create through the UI: POST /api/items', '403', (await denied).status());
  await viewer.getByText('Not allowed').first().waitFor({ timeout: 15000 });
  record('lab-viewer', 'UI explains the refusal ("Not allowed")', 'yes', 'yes');
  await viewer.screenshot({ path: `${OUT}/4-viewer-not-allowed.png` });
  await viewerContext.close();
  await browser.close();

  // --- Negative control: the same browser without the lab CA ------------------
  const untrusting = await chromium.launch({ env: { ...process.env, HOME: '/tmp/no-lab-ca' } });
  const untrustingPage = await (await untrusting.newContext()).newPage();
  let negative = 'loaded';
  try { await untrustingPage.goto(`${APP}/`, { timeout: 15000 }); } catch (err) {
    negative = (String(err.message).match(/net::ERR_[A-Z_]+/) || ['other error'])[0];
  }
  record('negative control', 'same Chromium without the lab CA: navigation', 'net::ERR_CERT_AUTHORITY_INVALID', negative);
  await untrusting.close();

  record('browser', 'console errors on the work-items pages', 'observe', consoleErrors.length ? consoleErrors.join(' | ') : 'none');
  fs.writeFileSync(`${OUT}/browser-requests.json`, JSON.stringify(requests, null, 1) + '\n');
  fs.writeFileSync(`${OUT}/browser-results.json`, JSON.stringify(results, null, 1) + '\n');
  const failed = results.filter((r) => r.result === 'fail').length;
  console.log(`\nBrowser check ${failed ? 'failed' : 'passed'}: ${results.filter((r) => r.result === 'pass').length} passed, ${failed} failed, ${results.filter((r) => r.result === 'info').length} observations.`);
  process.exit(failed ? 1 : 0);
})().catch((err) => {
  console.error(`browser check aborted: ${String(err.message).split('\n')[0]}`);
  fs.writeFileSync(`${OUT}/browser-results.json`, JSON.stringify(results, null, 1) + '\n');
  process.exit(2);
});
