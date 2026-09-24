export function routeRecords(views) {
  return [
    { path: '/', name: 'home', component: views.gallery },
    { path: '/upload', name: 'public-upload', component: views.upload },
    { path: '/login', name: 'login', component: views.login },
    {
      path: '/admin', component: views.layout, meta: { requiresAuth: true },
      children: [
        { path: '', redirect: '/admin/gallery' },
        ...['gallery', 'upload', 'recycle', 'stats', 'api', 'settings'].map(name => ({
          path: name, name: 'admin-' + name, component: views[name], props: { admin: true },
        })),
      ],
    },
    { path: '/gallery', redirect: '/' },
    ...['recycle', 'stats', 'api', 'settings'].map(name => ({ path: '/' + name, redirect: '/admin/' + name })),
    { path: '/:pathMatch(.*)*', name: 'not-found', component: views.notFound },
  ];
}
export function loginDestination(value) {
  return typeof value === 'string' && /^\/admin(?:\/(?:gallery|upload|recycle|stats|api|settings))?$/.test(value)
    ? value : '/admin/gallery';
}
export function accessGuard(session, verify) {
  return async to => {
    if (to.meta.requiresAuth) {
      try { await verify(); } catch { session.admin = false; }
      if (!session.admin) return { name: 'login', query: { redirect: to.path }, replace: true };
    }
    if (to.name === 'login' && session.admin) return loginDestination(to.query.redirect);
  };
}
