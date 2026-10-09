import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { engineFavicon, readEngineFile, statEnginePaths, type EngineFile, type EnginePathFact } from '../../chat/engine-client';
import { openPath, openUrl, revealPath } from '../../../design/native';
import { createStatCache } from './stat-cache';
import './assets.css';

/** Everything an asset chip needs from the outside world, so chips work anywhere without prop drilling. */
export type AssetContextValue = {
  /** False when no engine is attached: chips draw as plain text. */
  available: boolean;
  sessionId?: string;
  workspace: string;
  readFile: (path: string) => Promise<EngineFile>;
  stat: (paths: string[]) => Promise<EnginePathFact[]>;
  peek: (path: string) => EnginePathFact | undefined;
  favicon: (domain: string) => Promise<string | null>;
  openPath: (path: string) => Promise<void>;
  revealPath: (path: string) => Promise<void>;
  openUrl: (url: string) => Promise<void>;
};

const noEngine = (): Promise<never> => Promise.reject(new Error('No engine attached'));

export const noEngineAssets: AssetContextValue = {
  available: false,
  workspace: '',
  readFile: noEngine,
  stat: () => Promise.resolve([]),
  peek: () => undefined,
  favicon: () => Promise.resolve(null),
  openPath: noEngine,
  revealPath: noEngine,
  openUrl,
};

const AssetContext = createContext<AssetContextValue>(noEngineAssets);

export function useAssets(): AssetContextValue {
  return useContext(AssetContext);
}

/** Supplies a ready value: the engine provider below, or a fake in specimens and tests. */
export function AssetProvider({ value, children }: { value: AssetContextValue; children: ReactNode }) {
  return <AssetContext.Provider value={value}>{children}</AssetContext.Provider>;
}

const fileCacheLimit = 24;

function cachedReader(read: (path: string) => Promise<EngineFile>) {
  const cache = new Map<string, Promise<EngineFile>>();
  return (path: string) => {
    const hit = cache.get(path);
    if (hit) return hit;
    const next = read(path);
    cache.set(path, next);
    next.catch(() => cache.delete(path));
    if (cache.size > fileCacheLimit) cache.delete(cache.keys().next().value as string);
    return next;
  };
}

function cachedFavicons(session: string) {
  const cache = new Map<string, Promise<string | null>>();
  return (domain: string) => {
    const hit = cache.get(domain);
    if (hit) return hit;
    const next = engineFavicon(session, domain);
    cache.set(domain, next);
    return next;
  };
}

export function createEngineAssets(sessionId: string, workspace: string): AssetContextValue {
  const stats = createStatCache(paths => statEnginePaths(sessionId, paths));
  return {
    available: true,
    sessionId,
    workspace,
    readFile: cachedReader(path => readEngineFile(sessionId, path)),
    stat: stats.stat,
    peek: stats.peek,
    favicon: cachedFavicons(sessionId),
    // The native door only takes absolute paths inside this workspace.
    openPath: path => openPath(absoluteIn(workspace, path), workspace),
    revealPath: path => revealPath(absoluteIn(workspace, path), workspace),
    openUrl,
  };
}

function absoluteIn(workspace: string, path: string): string {
  return path.startsWith('/') ? path : `${workspace.replace(/\/+$/, '')}/${path}`;
}

/** Binds the assets to the attached engine session. Without a session the default plain-text value applies. */
export function EngineAssetProvider({ sessionId, workspace, children }: { sessionId?: string; workspace: string; children: ReactNode }) {
  const value = useMemo(() => (sessionId ? createEngineAssets(sessionId, workspace) : noEngineAssets), [sessionId, workspace]);
  return <AssetProvider value={value}>{children}</AssetProvider>;
}

export type PathState = { fact?: EnginePathFact; failed: boolean };

/** Looks a path up once per provider; `skip` lets a caller that already holds the fact avoid the call. */
export function usePathFact(path: string, skip = false): PathState {
  const assets = useAssets();
  const [state, setState] = useState<PathState>(() => ({ fact: assets.peek(path), failed: false }));
  useEffect(() => {
    if (skip || !assets.available) return;
    let live = true;
    assets.stat([path]).then(
      ([fact]) => live && setState({ fact, failed: false }),
      () => live && setState({ failed: true }),
    );
    return () => {
      live = false;
    };
  }, [assets, path, skip]);
  return state;
}

export type FileState = { status: 'loading' | 'ready' | 'error'; file?: EngineFile; url?: string };

export function dataUrl(file: EngineFile): string {
  return `data:${file.mime};base64,${file.dataBase64}`;
}

/** Fetches a workspace file through the bridge and exposes it as a data URL the CSP allows. */
export function useFile(path: string, enabled = true): FileState {
  const assets = useAssets();
  const [state, setState] = useState<FileState>({ status: 'loading' });
  useEffect(() => {
    if (!enabled) return;
    let live = true;
    setState({ status: 'loading' });
    assets.readFile(path).then(
      file => live && setState({ status: 'ready', file, url: dataUrl(file) }),
      () => live && setState({ status: 'error' }),
    );
    return () => {
      live = false;
    };
  }, [assets, path, enabled]);
  return state;
}

/** A real favicon for a domain the engine contacted; null means draw the monogram. */
export function useFavicon(domain: string): string | null {
  const assets = useAssets();
  const [url, setUrl] = useState<string | null>(null);
  useEffect(() => {
    if (!assets.available) return;
    let live = true;
    assets.favicon(domain).then(next => live && setUrl(next), () => undefined);
    return () => {
      live = false;
    };
  }, [assets, domain]);
  return url;
}
