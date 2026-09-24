import{u as i,o as n,m as c,a as u,b as r,c as l,d,h as m,i as h}from"./index-BVkSTVPD.js";const f={__name:"ApiView",props:{admin:Boolean},setup(p){const s=p,o=h(null),a=i();let e;return n(()=>{e=c(o.value,"api",s.admin,a)}),u(()=>e?.canLeave()),r(()=>e?.dispose()),(q,t)=>(l(),d("section",{ref_key:"root",ref:o,id:"api-view",class:"page-view"},[...t[0]||(t[0]=[m(`<div class="section-title"><div><h1>API 使用说明</h1><p>使用管理员会话或 API Key 批量上传与管理图片。</p></div></div><div class="api-grid"><section class="settings-panel"><h2>私有上传</h2><p>请求头传入 API Key，表单字段为 <code>file</code> 或 <code>image</code>。适合脚本和自动化客户端。</p><pre><code>curl -H &#39;X-API-Key: sk-...&#39; \\
  -F &#39;file=@photo.jpg&#39; \\
  https://img.example.com/api/upload/private</code></pre></section><section class="settings-panel"><h2>URL 上传</h2><p>发送一个 URL 或 URL 数组，服务端下载后保存。大量 URL 可使用流式进度接口。</p><pre><code>POST /api/upload/url
{&quot;url&quot;:&quot;https://example.com/photo.jpg&quot;}

POST /api/upload/urls
{&quot;urls&quot;:[&quot;https://example.com/a.jpg&quot;]}</code></pre></section><section class="settings-panel"><h2>公开上传</h2><p>管理员开启公开上传后，无须密钥。格式、大小、频率和内容审核由站点配置控制。</p><pre><code>POST /api/upload/public
Content-Type: multipart/form-data
file=@photo.jpg</code></pre></section><section class="settings-panel"><h2>响应与批量建议</h2><p>响应为 JSON；成功时图片信息位于 <code>data</code>。API Key 批量上传建议使用 4 路并发；收到 429 时按 Retry-After 重试。</p><pre><code>{&quot;success&quot;:true,&quot;data&quot;:{&quot;url&quot;:&quot;/i/uuid.jpg&quot;}}</code></pre></section></div>`,2)])],512))}};export{f as default};
