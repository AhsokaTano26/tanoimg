export function rotatedSize({width,height}, quarterTurns) {
  return quarterTurns % 2 ? {width:height,height:width} : {width,height};
}

export function dragRectangle(start, end, bounds) {
  const left=Math.max(0,Math.min(bounds.width,Math.floor(Math.min(start.x,end.x))));
  const top=Math.max(0,Math.min(bounds.height,Math.floor(Math.min(start.y,end.y))));
  const right=Math.max(left,Math.min(bounds.width,Math.ceil(Math.max(start.x,end.x))));
  const bottom=Math.max(top,Math.min(bounds.height,Math.ceil(Math.max(start.y,end.y))));
  return {x:left,y:top,width:right-left,height:bottom-top};
}

export function rotateRectangleClockwise(rect, oldSize) {
  return {x:oldSize.height-rect.y-rect.height,y:rect.x,width:rect.height,height:rect.width};
}

export function editorOutputFormat(sourceFormat) {
  switch ((sourceFormat||'').toLowerCase()) {
    case 'jpg': case 'jpeg': return {mime:'image/jpeg',extension:'jpg',replaceable:true};
    case 'png': return {mime:'image/png',extension:'png',replaceable:true};
    case 'webp': return {mime:'image/webp',extension:'webp',replaceable:true};
    default: return {mime:'image/png',extension:'png',replaceable:false};
  }
}
