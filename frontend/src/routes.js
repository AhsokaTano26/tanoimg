export const settingsPaths=['appearance','site','public-upload','private-upload','apikeys','moderation','moderation-images','notification','account','blacklist','storage','about'];
const adminPaths=['gallery','upload','recycle','stats','api',...settingsPaths];
export function routeRecords(views) {
 return [
  {path:'/',name:'home',component:views.gallery},
  {path:'/upload',redirect:()=>({path:'/',hash:'#public-upload'})},
  {path:'/login',name:'login',component:views.login},
  {path:'/admin',component:views.layout,meta:{requiresAuth:true},children:[
    {path:'',redirect:'/admin/gallery'},
    ...adminPaths.map(name=>({path:name,name:'admin-'+name,component:views[name]||views.settings,props:{admin:true,publicUpload:name==='public-upload'}})),
    {path:'settings',redirect:'/admin/appearance'},
  ]},
  {path:'/gallery',redirect:'/'},
  ...['recycle','stats','api'].map(name=>({path:'/'+name,redirect:'/admin/'+name})),
  {path:'/settings',redirect:'/admin/appearance'},
  {path:'/:pathMatch(.*)*',name:'not-found',component:views.notFound},
 ];
}
export function loginDestination(value) {
 if(value==='/admin/settings')return '/admin/appearance';
 return typeof value==='string' && (value==='/admin'||adminPaths.some(path=>value==='/admin/'+path)) ? value:'/admin/gallery';
}
export function accessGuard(session,verify) {
 return async to=>{
  if(to.meta.requiresAuth){try{await verify();}catch{session.admin=false;}if(!session.admin)return {name:'login',query:{redirect:to.path},replace:true};}
  if(to.name==='login'&&session.admin)return loginDestination(to.query.redirect);
 };
}
