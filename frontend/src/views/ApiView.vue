<script setup>
import {computed,ref} from 'vue';
import CodeBlock from '../components/CodeBlock.vue';
import FormField from '../components/FormField.vue';
const base=location.origin;
const endpoint=ref('private');
const examples={
 private:{method:'POST',path:'/api/upload/private?visibility=private',title:'上传本地图片',description:'管理员会话或 API Key 上传。visibility 选择 private 或 public，不受访客上传开关和 IP 频率限制。',code:`curl -X POST "${base}/api/upload/private?visibility=private" \
  -H "X-API-Key: YOUR_API_KEY" \
  -F "file=@photo.png"`,params:[['X-API-Key','请求头','API 密钥；管理员 Cookie 会话可省略'],['visibility','查询参数','private（默认）或 public'],['file / image','表单文件','图片内容，格式按文件内容检测']]},
 public:{method:'POST',path:'/api/upload/public',title:'匿名公共上传',description:'站点需开启公共上传，受格式、大小、每分钟限流、黑名单、自动封禁和内容审核限制。',code:`curl -X POST "${base}/api/upload/public" \
  -F "file=@photo.png"`,params:[['file / image','表单文件','一张图片；批量上传请发送多个请求']]},
 url:{method:'POST',path:'/api/upload/url?visibility=public',title:'通过 URL 导入图片',description:'管理员或 API Key 可提交单个 URL，或最多 1000 个 URL 的数组。公开可见性不会把文件下载到客户端再转传。',code:`curl -X POST "${base}/api/upload/url?visibility=public" \
  -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"url":"https://example.com/photo.jpg"}'`,params:[['url','JSON','单个 http(s) URL 或 URL 数组'],['visibility','查询参数','private（默认）或 public'],['returnBase64','JSON，可选','最多 10 张且单张 4 MiB，不建议大批量使用']]},
 publicURL:{method:'POST',path:'/api/upload/public/url',title:'公共 URL 上传',description:'匿名用户一次提交一个外部图片 URL，与公共文件上传共用频率和封禁计数。禁止本机及内网地址。',code:`curl -X POST "${base}/api/upload/public/url" \
  -H "Content-Type: application/json" \
  -d '{"url":"https://example.com/photo.jpg"}'`,params:[['url','JSON','一个外部 http(s) 图片 URL，最长 2048 字符']]},
 list:{method:'GET',path:'/api/images?page=1&limit=20',title:'分页读取图库',description:'访客只获得允许公开展示的图片；管理员默认获得全部正常图片。scope=public 强制使用公共展示规则。',code:`curl "${base}/api/images?page=1&limit=20&scope=public"`,params:[['page','查询参数','页码，从 1 开始'],['limit','查询参数','每页 1–100 张，默认 20'],['scope','查询参数，可选','public 强制按公开展示配置过滤']]},
 recycle:{method:'DELETE',path:'/api/images/{id}',title:'删除与恢复',description:'软删除只允许管理员会话，API Key 不能管理图片。使用恢复接口可将图片移出回收站。',code:`# 先登录并保存会话 Cookie
curl -c cookies.txt -X POST "${base}/api/auth/login" \
  -H "Content-Type: application/json" \
  -d '{"username":"ADMIN","password":"PASSWORD"}'

# 软删除图片
curl -b cookies.txt -X DELETE "${base}/api/images/IMAGE_ID"

# 从回收站恢复
curl -b cookies.txt -X PUT "${base}/api/images/IMAGE_ID/restore"`,params:[['id','路径参数','图片记录 ID，来自上传或图库响应'],['Cookie','请求头','管理员登录会话']]},
};
const current=computed(()=>examples[endpoint.value]);
const response=JSON.stringify({success:true,data:{id:'image-id',uuid:'image-uuid',filename:'image-uuid.png',format:'png',size:245760,width:1920,height:1080,url:'/i/image-uuid.png',uploadedByType:'private'}},null,2);
const jsExample=`const form = new FormData();
form.append("file", fileInput.files[0]);

const response = await fetch("${base}/api/upload/private", {
  method: "POST",
  headers: { "X-API-Key": "YOUR_API_KEY" },
  body: form
});
const result = await response.json();
if (!response.ok || !result.success) {
  throw new Error(result.message);
}
console.log(new URL(result.data.url, "${base}").href);`;
</script>
<template><section class="page-view api-documentation"><div class="api-hero"><div><span class="eyebrow">DEVELOPER REFERENCE</span><h1>连接你的图片工作流。</h1><p>从一张图片到自动化批量上传，使用简单的 HTTP 接口接入 TanoImg。</p><a class="outline-button" href="/api/openapi.json" target="_blank" rel="noopener noreferrer">查看 OpenAPI JSON</a></div><span class="api-badge">REST / JSON</span></div><div class="api-summary"><div><span class="api-dot green"></span><strong>两种认证方式</strong><p>管理员 Cookie · API Key</p></div><div><span class="api-dot cyan"></span><strong>流式上传</strong><p>4 路并发 · 分页读取</p></div><div><span class="api-dot amber"></span><strong>公共上传保护</strong><p>限流 · 格式检查 · 自动封禁</p></div></div><section class="settings-panel"><div class="panel-heading"><h2>接口参考</h2><FormField v-model="endpoint" label="选择接口" type="select" :options="Object.entries(examples).map(([value,item])=>({label:item.title,value}))" /></div><div class="endpoint-line"><span class="method-badge" :class="current.method.toLowerCase()">{{ current.method }}</span><code>{{ current.path }}</code></div><h3>{{ current.title }}</h3><p>{{ current.description }}</p><div class="api-detail-grid"><div class="api-params"><div class="api-param-head"><span>参数</span><span>类型 / 说明</span></div><div v-for="param in current.params" :key="param[0]" class="api-param"><code>{{ param[0] }}</code><div><span>{{ param[1] }}</span><p>{{ param[2] }}</p></div></div></div><CodeBlock :code="current.code" label="cURL · 请求示例" /></div></section><div class="api-detail-grid"><section class="settings-panel"><h2>成功响应</h2><p>所有接口使用 success / data 包装。图片 url 可能为相对地址，请与站点地址拼接。</p><CodeBlock :code="response" language="json" label="JSON · 单张上传响应" /></section><section class="settings-panel"><h2>JavaScript 上传</h2><p>不要手动设置 multipart 的 Content-Type，让浏览器生成 boundary。</p><CodeBlock :code="jsExample" language="javascript" label="JavaScript · fetch" /></section></div><section class="settings-panel"><h2>错误码与重试</h2><div class="error-table"><div v-for="row in [[400,'请求无效','检查文件、格式、大小或 JSON 参数'],[401,'认证失败','管理员重新登录或检查 API Key'],[403,'拒绝上传','公共上传关闭、IP 被封禁或其他权限限制'],[429,'请求过于频繁','按 Retry-After 等待后重试；不要立即循环请求'],[500,'服务端错误','稍后重试，并检查服务端日志']]" :key="row[0]"><span class="status-code">{{ row[0] }}</span><strong>{{ row[1] }}</strong><span>{{ row[2] }}</span></div></div><CodeBlock :code="JSON.stringify({success:false,message:'公开上传过于频繁，请稍后重试'},null,2)" language="json" label="JSON · 错误响应" /></section><section class="settings-panel api-batch"><h2>批量接入建议</h2><p>管理员或 API Key 使用 4 路并发，每个请求一张图片；保留失败项，指数退避重试 429 / 502 / 503 / 504。公共上传的自动封禁会计算失败和限流请求，请遵循站点规则。</p><p>大量 URL 可使用 <code>POST /api/upload/urls</code>，JSON 字段为 <code>urls</code> 数组，响应为 SSE：<code>start → progress → complete</code>。私人指不在公共图库展示，已知图片直链仍可访问。</p><RouterLink to="/admin/apikeys" class="outline-button">管理 API 密钥</RouterLink></section></section></template>
