import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createRouter, createMemoryHistory } from 'vue-router';
import { accessGuard, routeRecords, loginDestination } from '../src/routes.js';

function setup(authenticated = false) {
  const session = { admin: authenticated };
  const component = { render() {} };
  const views = Object.fromEntries(['gallery', 'upload', 'login', 'layout', 'notFound', 'recycle', 'stats', 'api', 'settings'].map(name => [name, component]));
  const router = createRouter({ history: createMemoryHistory(), routes: routeRecords(views) });
  router.beforeEach(accessGuard(session, async () => session.admin));
  return { router, session };
}
test('guests can browse images and upload; all admin routes require login', async () => {
  const { router } = setup();
  for (const path of ['/', '/upload']) {
    await router.push(path);
    assert.equal(router.currentRoute.value.path, path);
    assert.equal(router.currentRoute.value.meta.requiresAuth, undefined);
  }
  for (const name of ['gallery', 'upload', 'recycle', 'stats', 'api', 'settings']) {
    await router.push('/admin/' + name);
    assert.equal(router.currentRoute.value.name, 'login');
    assert.equal(router.currentRoute.value.query.redirect, '/admin/' + name);
  }
});
test('authenticated pages use router navigation, reloadable URLs and history', async () => {
  const { router, session } = setup(true);
  await router.push('/admin/gallery');
  await router.push('/admin/settings');
  const back = new Promise(resolve => { const remove = router.afterEach(() => { remove(); resolve(); }); });
  router.back();
  await back;
  assert.equal(router.currentRoute.value.path, '/admin/gallery');
  session.admin = false;
  await router.push('/admin/recycle');
  assert.equal(router.currentRoute.value.name, 'login');
});
test('legacy protected links remain protected and login returns to the intended page', async () => {
  const { router, session } = setup();
  await router.push('/settings');
  assert.equal(router.currentRoute.value.query.redirect, '/admin/settings');
  session.admin = true;
  await router.replace('/login?redirect=/admin/recycle');
  assert.equal(router.currentRoute.value.path, '/admin/recycle');
});
test('login redirects cannot leave the local admin area', () => {
  for (const value of ['https://evil.test', '//evil.test', '/admin/../upload', '/admin?x=1', ['/', '/admin'], null]) {
    assert.equal(loginDestination(value), '/admin/gallery');
  }
  assert.equal(loginDestination('/admin/settings'), '/admin/settings');
});
