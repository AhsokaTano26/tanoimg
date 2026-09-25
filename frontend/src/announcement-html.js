import createDOMPurify from 'dompurify';

const purifiers=new WeakMap();
const config={
  ALLOWED_TAGS:['p','br','strong','b','em','i','u','s','a','ul','ol','li','blockquote','code','h2','h3'],
  ALLOWED_ATTR:['href','title'],
  ALLOW_DATA_ATTR:false,
  ALLOW_ARIA_ATTR:false,
  ALLOWED_URI_REGEXP:/^(?:(?:https?:|mailto:)|\/(?!\/)|#)/i,
};

export function sanitizeAnnouncement(content, browserWindow=window){
  let purifier=purifiers.get(browserWindow);
  if(!purifier){purifier=createDOMPurify(browserWindow);purifiers.set(browserWindow,purifier);}
  return purifier.sanitize(String(content??''),config);
}
