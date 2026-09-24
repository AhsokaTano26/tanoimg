# EasyImg remote migration

Reuse the existing NeDB → SQLite converter. The client compacts NeDB journals to their current live records, audits image references, and builds a deterministic uncompressed TAR using bounded streaming buffers. A cached archive and source fingerprint support subsequent resumes. Credentials are read interactively or from environment variables, never saved in the checkpoint.

Administrator-only API:

- `GET /api/admin/migrations`: authenticated protocol discovery.
- `DELETE /api/admin/migrations/{id}`: reset incomplete/failed staging only; preparing and ready datasets are protected.
- `POST /api/admin/migrations`: begin/resume using archive SHA-256 and byte length. The digest is the import ID. Each import is bound to the administrator's user ID.
- `GET /api/admin/migrations/{id}`: persisted state and byte offset.
- `PUT /api/admin/migrations/{id}/archive?offset=N`: append a chunk of at most 8 MiB. Require exact offsets; roll back failed chunks. Re-query the offset after uncertain responses.
- `POST /api/admin/migrations/{id}/finalize`: asynchronously verify the entire archive, extract only regular allowed database/image paths, then run the existing converter in an isolated directory. Poll status until ready. Restart-interrupted preparation can be retried from the retained archive.

Only regular files are accepted; no absolute paths, traversal, links, duplicate entries, or arbitrary output locations. Completed images are hard-linked into the prepared dataset when supported, with a copy fallback. Source data and live database are never overwritten. Archive/source scratch space is reclaimed after a durable ready receipt. Existing prepared data is protected from finalize retries.

The prepared dataset lives at `<current data>/imports/<digest>/ready`. The user activates it by setting `TANOIMG_DATA` to that path and redeploying. This leaves the original bootstrap instance available for rollback, avoids importing over a live SQLite database, and requires no hot swap or destructive account merge. Original usernames/password hashes, API keys, image IDs/URLs, deleted state, and settings are retained; existing TanoImg sessions/TOTP/Passkeys are not merged into the imported EasyImg accounts.

Validation: unauthorized/API-key-only denial; owner binding; chunk replay/conflict; restart resume; truncated/oversized chunks; hash mismatch; malicious TAR paths/links; asynchronous finalize retry; old URLs and metadata preservation; live data untouched. Client preflight tests cover NeDB updates/tombstones and missing files. Full local end-to-end validation uses a disposable service and the actual supplied backup; it must not activate settings that could contact external notification/moderation services.
