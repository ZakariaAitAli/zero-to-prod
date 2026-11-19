import assert from 'node:assert/strict'
import { afterEach, mock, test } from 'node:test'

import {
  ApiError,
  createWorkItem,
  getSession,
  processWorkItem,
} from '../src/api/work-items.ts'

afterEach(() => mock.restoreAll())

for (const status of [403, 429, 500, 503]) {
  test(`session HTTP ${status} reports an unavailable gateway`, async () => {
    mock.method(globalThis, 'fetch', async () => new Response('', { status }))
    assert.deepEqual(await getSession(), { kind: 'unavailable' })
  })
}

test('session network failure does not masquerade as development mode', async () => {
  mock.method(globalThis, 'fetch', async () => { throw new TypeError('offline') })
  assert.deepEqual(await getSession(), { kind: 'unavailable' })
})

test('successful HTML fallback identifies development without a gateway', async () => {
  mock.method(globalThis, 'fetch', async () => new Response('<html>Vite</html>', {
    headers: { 'Content-Type': 'text/html; charset=utf-8' },
  }))
  assert.deepEqual(await getSession(), { kind: 'no-gateway' })
})

test('unauthenticated session remains distinct from an outage', async () => {
  mock.method(globalThis, 'fetch', async () => new Response('', { status: 401 }))
  assert.deepEqual(await getSession(), { kind: 'signed-out' })
})

for (const body of [{ email: { unexpected: 'object' } }, {}, null, ['user']]) {
  test(`invalid session shape is unavailable: ${JSON.stringify(body)}`, async () => {
    mock.method(globalThis, 'fetch', async () => Response.json(body))
    assert.deepEqual(await getSession(), { kind: 'unavailable' })
  })
}

test('malformed session JSON is unavailable', async () => {
  mock.method(globalThis, 'fetch', async () => new Response('{', {
    headers: { 'Content-Type': 'application/json' },
  }))
  assert.deepEqual(await getSession(), { kind: 'unavailable' })
})

test('valid gateway identity is returned as text', async () => {
  mock.method(globalThis, 'fetch', async () => Response.json({ email: 'person@example.test' }))
  assert.deepEqual(await getSession(), { kind: 'signed-in', email: 'person@example.test' })
})

test('create preserves same-origin session and the CSRF header', async () => {
  let observed: RequestInit | undefined
  mock.method(globalThis, 'fetch', async (url: string, init: RequestInit) => {
    assert.equal(url, '/api/items')
    observed = init
    return Response.json({ id: 1, title: 'Test', status: 'pending' })
  })
  await createWorkItem({ title: 'Test' })
  assert.equal(observed?.credentials, 'same-origin')
  assert.equal(new Headers(observed?.headers).get('X-Requested-With'), 'work-items-web')
  assert.equal(observed?.method, 'POST')
  assert.equal(observed?.body, JSON.stringify({ title: 'Test' }))
})

test('non-JSON upstream error keeps the HTTP status', async () => {
  mock.method(globalThis, 'fetch', async () => new Response('upstream failed', { status: 503 }))
  await assert.rejects(createWorkItem({ title: 'Test' }), (error: unknown) =>
    error instanceof ApiError && error.message === 'http_503')
})

test('already-active processing follows the existing job instead of resubmitting', async () => {
  const paths: string[] = []
  mock.method(globalThis, 'fetch', async (url: string) => {
    paths.push(url)
    if (url === '/api/items/1/process') {
      return Response.json({ error: 'processing_already_active', processing_job_id: 17 }, { status: 409 })
    }
    assert.equal(url, '/api/processing-jobs/17')
    return Response.json({ id: 17, state: 'accepted' })
  })
  assert.equal((await processWorkItem(1)).id, 17)
  assert.deepEqual(paths, ['/api/items/1/process', '/api/processing-jobs/17'])
})

for (const body of [{ user: 'operator' }, { email: '', user: 'operator' }]) {
  test(`gateway user fallback remains supported: ${JSON.stringify(body)}`, async () => {
    mock.method(globalThis, 'fetch', async () => Response.json(body))
    assert.deepEqual(await getSession(), { kind: 'signed-in', email: 'operator' })
  })
}

test('a successful unexpected content type is unavailable, not development mode', async () => {
  mock.method(globalThis, 'fetch', async () => new Response('unexpected', {
    headers: { 'Content-Type': 'text/plain' },
  }))
  assert.deepEqual(await getSession(), { kind: 'unavailable' })
})
