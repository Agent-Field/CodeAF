// WASI exists only to initialize Go's runtime. Consensus clocks and election
// entropy enter through raftcore inputs; these imports never supply either.
// There is no filesystem, socket, scheduler loop, environment or payload log.
export function wasiPreview1() {
  let memory;
  const calls = Object.create(null);
  const view = () => new DataView(memory.buffer);
  const bytes = (p, n) => {
    if (!memory || p < 0 || n < 0 || p + n > memory.buffer.byteLength) throw new Error('WASI bounds');
    return new Uint8Array(memory.buffer, p, n);
  };
  const u32 = (p, n) => { bytes(p, 4); view().setUint32(p, n, true); };
  const env = new TextEncoder().encode('GOMEMLIMIT=48MiB\0GOGC=50\0GOMAXPROCS=1\0');
  const offsets = [0];
  for (let i = 0; i < env.length - 1; i++) if (env[i] === 0) offsets.push(i + 1);
  const implemented = {
    args_sizes_get: (count, size) => { u32(count, 0); u32(size, 0); return 0; },
    args_get: () => 0,
    environ_sizes_get: (count, size) => { u32(count, offsets.length); u32(size, env.length); return 0; },
    environ_get: (pointers, data) => {
      bytes(data, env.length).set(env);
      offsets.forEach((off, i) => u32(pointers + 4 * i, data + off));
      return 0;
    },
    clock_time_get: (id, precision, out) => {
      if (id !== 0 && id !== 1) return 28; // WASI EINVAL.
      bytes(out, 8);
      const millis = id === 0 ? Date.now() : performance.now();
      view().setBigUint64(out, BigInt(Math.floor(millis * 1e6)), true);
      return 0;
    },
    random_get: (p, n) => {
      for (let off = 0; off < n; off += 65536) crypto.getRandomValues(bytes(p + off, Math.min(65536, n - off)));
      return 0;
    },
    // Runtime failure output is discarded rather than risking a command or
    // private row in logs. Only byte counters leave this boundary.
    fd_write: (fd, iovs, count, out) => {
      if (fd !== 1 && fd !== 2) return 8; // WASI EBADF.
      if (count > 1024) return 28;
      let total = 0;
      for (let i = 0; i < count; i++) { bytes(iovs + i * 8, 8); total += view().getUint32(iovs + i * 8 + 4, true); }
      u32(out, total); return 0;
    },
    proc_exit: () => { throw new Error('Go reactor exited'); },
    // A synchronous reactor must return; silently faking a poll completion
    // would conceal an incompatible Go scheduler or blocking operation.
    poll_oneoff: () => { throw new Error('Go reactor attempted blocking poll'); },
    sched_yield: () => { throw new Error('Go reactor attempted scheduler yield'); },
  };
  const badFD = ['fd_close', 'fd_read', 'fd_pread', 'fd_pwrite', 'fd_seek', 'fd_fdstat_get', 'fd_fdstat_set_flags',
    'fd_filestat_get', 'fd_filestat_set_size', 'fd_sync', 'fd_datasync', 'fd_prestat_get', 'fd_prestat_dir_name', 'fd_readdir',
    'path_filestat_get', 'path_open', 'sock_accept', 'sock_shutdown'];
  for (const name of badFD) implemented[name] = () => 8;
  const imports = {};
  for (const [name, fn] of Object.entries(implemented)) imports[name] = (...args) => {
    calls[name] = (calls[name] ?? 0) + 1;
    return fn(...args);
  };
  return { imports, calls, attach: (m) => { memory = m; } };
}
