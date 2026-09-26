// Fetch IDs only. Keep each response and each mutation bounded for large libraries.
export async function fetchSelectionIDs(filters, request, signal) {
  const ids = new Set();
  let cursor = '';
  do {
    signal?.throwIfAborted();
    const query = new URLSearchParams({...filters, selection:'all', after:cursor});
    const data = await request('/api/images?'+query, {signal});
    signal?.throwIfAborted();
    for (const id of data.ids) ids.add(id);
    cursor = data.nextCursor;
  } while (cursor);
  return [...ids];
}

export async function sendSelectionBatches(ids, send, completed) {
  for (let offset=0; offset<ids.length; offset+=1000) {
    const batch = ids.slice(offset, offset+1000);
    const result = await send(batch);
    completed(batch, result);
  }
}
