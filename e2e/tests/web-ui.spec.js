const { test, expect } = require('@playwright/test');

// --- Dashboard ---

test('dashboard loads successfully', async ({ page }) => {
  const response = await page.goto('/');
  expect(response.status()).toBe(200);
  await expect(page).toHaveTitle(/OpenBot/);
});

test('dashboard shows correct title', async ({ page }) => {
  await page.goto('/');
  const heading = page.locator('h1, h2, .title').first();
  await expect(heading).toBeVisible();
});

// --- Chat Page ---

test('chat page loads', async ({ page }) => {
  const response = await page.goto('/chat');
  expect(response.status()).toBe(200);
});

test('chat page has message input', async ({ page }) => {
  await page.goto('/chat');
  const input = page.locator('textarea, input[name="message"], #message-input, [placeholder]').first();
  await expect(input).toBeVisible();
});

test('chat page has send button', async ({ page }) => {
  await page.goto('/chat');
  const sendBtn = page.locator('button[type="submit"], .send-btn, #send-btn').first();
  await expect(sendBtn).toBeVisible();
});

test('chat page has new chat button', async ({ page }) => {
  await page.goto('/chat');
  // Look for clear/new chat button
  const clearBtn = page.locator('button:has-text("New"), button:has-text("Clear"), .clear-btn').first();
  await expect(clearBtn).toBeVisible();
});

// --- Settings Page ---

test('settings page loads', async ({ page }) => {
  const response = await page.goto('/settings');
  expect(response.status()).toBe(200);
});

test('settings page has save button', async ({ page }) => {
  await page.goto('/settings');
  const saveBtn = page.locator('button:has-text("Save"), #save-btn').first();
  await expect(saveBtn).toBeVisible();
});

// --- Status API ---

test('status API returns JSON', async ({ request }) => {
  const response = await request.get('/status');
  expect(response.status()).toBe(200);

  const body = await response.json();
  expect(body.status).toBe('ok');
  expect(body.version).toBeDefined();
  expect(body.time).toBeDefined();
});

test('status API version is not empty', async ({ request }) => {
  const response = await request.get('/status');
  const body = await response.json();
  expect(body.version).not.toBe('');
  expect(body.version).toMatch(/\d+\.\d+/);
});

// --- Config API ---

test('config API returns sanitized config', async ({ request }) => {
  const response = await request.get('/api/config');
  expect(response.status()).toBe(200);

  const body = await response.json();
  expect(body.general).toBeDefined();
  expect(body.providers).toBeDefined();
  expect(body.channels).toBeDefined();
  expect(body.memory).toBeDefined();
  expect(body.security).toBeDefined();
});

test('config API masks sensitive fields', async ({ request }) => {
  const response = await request.get('/api/config');
  const body = await response.json();

  // Telegram token should be masked (contains ****)
  if (body.channels?.telegram?.token) {
    expect(body.channels.telegram.token).toContain('****');
  }
});

// --- Static Assets ---

test('logo asset is accessible', async ({ request }) => {
  const response = await request.get('/assets/logo.png');
  expect(response.status()).toBe(200);
  expect(response.headers()['content-type']).toContain('image/png');
});

test('assets have cache headers', async ({ request }) => {
  const response = await request.get('/assets/logo.png');
  expect(response.headers()['cache-control']).toContain('max-age');
});

// --- Navigation ---

test('navigation between pages works', async ({ page }) => {
  await page.goto('/');

  // Navigate to chat
  const chatLink = page.locator('a[href="/chat"], a:has-text("Chat")').first();
  if (await chatLink.isVisible()) {
    await chatLink.click();
    await expect(page).toHaveURL(/\/chat/);
  }
});

test('navigation to settings works', async ({ page }) => {
  await page.goto('/');

  const settingsLink = page.locator('a[href="/settings"], a:has-text("Settings")').first();
  if (await settingsLink.isVisible()) {
    await settingsLink.click();
    await expect(page).toHaveURL(/\/settings/);
  }
});

// --- Chat Send (integration) ---

test('sending empty message returns error', async ({ request }) => {
  const response = await request.post('/chat/send', {
    multipart: {
      message: '',
    },
  });
  // Empty message should return 400
  expect(response.status()).toBe(400);
  const body = await response.json();
  expect(body.error).toBeDefined();
});

// --- Session Management ---

test('chat clear endpoint works', async ({ request }) => {
  const response = await request.post('/chat/clear');
  expect(response.status()).toBe(200);
  const body = await response.json();
  expect(body.status).toBe('session cleared');
});

test('session cookie is set on chat page', async ({ page }) => {
  await page.goto('/chat');
  const cookies = await page.context().cookies();
  const sessionCookie = cookies.find(c => c.name === 'openbot_session');
  expect(sessionCookie).toBeDefined();
  expect(sessionCookie.value).toMatch(/^web_/);
});
