const escapeHTML = value => String(value).replace(/[&<>"']/g, character => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[character]));
const escapeMarkdown = value => String(value).replace(/[\\\[\]]/g,'\\$&').replace(/[\r\n]/g,' ');

export function renderEmbedTemplate(template,image,origin) {
  const html = /<\/?[a-z][^>]*>/i.test(template.body);
  const markdown = !html && /!\[[^\]]*\]\(/.test(template.body);
  const values = {
    url:new URL(image.url,origin).href,
    alt:image.alt || image.originalName || image.filename || 'image',
    width:String(image.width || ''),
    height:String(image.height || ''),
    filename:image.filename || image.originalName || '',
  };
  return template.body.replace(/\{(url|alt|width|height|filename)\}/g,(_,key) => {
    const value = values[key];
    if (html) return escapeHTML(value);
    if (markdown && key === 'alt') return escapeMarkdown(value);
    if (markdown && key === 'url') return value.replace(/\(/g,'%28').replace(/\)/g,'%29');
    return value;
  });
}
