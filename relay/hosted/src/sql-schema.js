// Platform bookkeeping is neither application authority nor owner data to
// erase. Miniflare's local explorer adds exactly this name table before the
// first RPC; unknown application tables still make a fresh store refuse.
export const APPLICATION_TABLES = "type = 'table' AND name NOT LIKE 'sqlite\\_%' ESCAPE '\\' AND name NOT LIKE '\\_cf\\_%' ESCAPE '\\' AND name <> '__miniflare_do_name'";
