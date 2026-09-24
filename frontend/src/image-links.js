const escapeHTML = value => String(value).replace(/[&<>"']/g, char => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[char]));
export function imageLinks(image, origin) {
  const url = new URL(image.url, origin).href;
  const name = image.originalName || image.filename || 'image';
  const markdownName = name.replace(/[\\\[\]]/g, '\\$&').replace(/[\r\n]/g, ' ');
  return {
    '直链': url,
    HTML: `<img src="${escapeHTML(url)}" alt="${escapeHTML(name)}" />`,
    Markdown: `![${markdownName}](${url.replace(/\(/g,'%28').replace(/\)/g,'%29')})`,
    BBCode: `[img]${url}[/img]`,
  };
}
