import { useContext, useEffect, useRef, useState } from 'react';
import { Button, Icon, useToasts } from '../../components/ui';
import { newTab } from '../tabs/helpers';
import { NewTabHostContext } from '../tabs/kinds/newtab/api';
import { createWorkspaceClient, isWorkspaceKey, type WorkspaceRecord, type WorkspaceTransferRequest } from '../workspace-sync/client';
import { matchingTabs, planTransfer, planTransferUndo, transferredLocal, type SavedChatLocation, type TransferReceipt } from '../workspace-sync/transfer-plan';
import { emptyLocal, parseShared } from '../workspace-sync/shared';
import { readPersisted, safeLocal, windowWriter } from '../workspace-sync/windowStore';
import { chatIdFromSessionFile, type HomeDigest } from './client';
import { usePlacesShell } from './shell/PlacesShell';

const client = createWorkspaceClient();
type Pending = { request: WorkspaceTransferRequest; receipt: TransferReceipt };
function readPending(text: string | undefined | null, place: string): Pending | undefined {
  if (!text) return undefined;
  try {
    const value = JSON.parse(text) as Pending;
    const ask = value?.request, receipt = value?.receipt;
    if (!ask || ask.destination !== place || typeof ask.intent !== 'string' || typeof ask.writer !== 'string' || !Number.isSafeInteger(ask.sourceRevision) || ask.sourceRevision < 0 || !Number.isSafeInteger(ask.destinationRevision) || ask.destinationRevision < 0 || !parseShared(ask.sourceWorkspace) || !parseShared(ask.destinationWorkspace)) return undefined;
    if (!receipt || !Array.isArray(receipt.tabIds) || !receipt.tabIds.length || !receipt.tabIds.every(id => typeof id === 'string') || !Array.isArray(receipt.tabs) || !Array.isArray(receipt.groups) || !Array.isArray(receipt.sourceOrder) || !receipt.sourceOrder.every(id => typeof id === 'string') || receipt.tabs.length !== receipt.tabIds.length || !parseShared({ schema: 1, tabs: receipt.tabs, groups: receipt.groups, closed: [], nextNumber: 1 })) return undefined;
    return value; // Preserve immutable original payload bytes/shape for the engine's idempotent replay.
  } catch { return undefined; }
}
const storageKey = (destination: string) => `codeaf.desktop.first-place-transfer.v1:${destination}:${windowWriter()}`;
/** 6d: a single explicit offer, using the existing quiet suggestion control. It moves tab sets, not sessions. */
export function FirstPlaceTabOffer({ digest, active }: { digest: HomeDigest; active: boolean }) {
  const shell = usePlacesShell(), host = useContext(NewTabHostContext), toast = useToasts();
  const placeId = digest.place?.id;
  const eligible = !!placeId && isWorkspaceKey(placeId) && shell?.places.graph?.totals.places === 1;
  const [source, setSource] = useState<WorkspaceRecord>();
  const [ids, setIds] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [emptySource] = useState(() => newTab());
  const held = useRef(false);
  const pending = useRef<Pending | undefined>(undefined);
  const undoPending = useRef<Pending | undefined>(undefined);
  const scope = useRef(placeId); scope.current = placeId;
  useEffect(() => { scope.current = placeId; return () => { scope.current = undefined; }; }, [placeId]);
  useEffect(() => {
    // A newer home digest must not blank the offer. The world feed refreshes the
    // digest while the button is on screen; clearing here unmounts it, and the
    // click lands on nothing so the move never starts.
    if (!eligible || !active || !placeId || !shell) {
      setIds([]); setSource(undefined); pending.current = undefined;
      return;
    }
    let live = true;
    const abort = new AbortController();
    try { pending.current = readPending(safeLocal()?.getItem(storageKey(placeId)), placeId); } catch { /* A request cannot be trusted until the bridge validates it. */ }
    async function read() {
      const record = await client.get('now', abort.signal);
      if (!record.workspace || !live) return record;
      const files = [...new Set(record.workspace.tabs.flatMap(t => (t.split?.panes ?? [t]).flatMap(p => p.sessionFile ? [p.sessionFile] : [])))];
      const rows = new Map(digest.chats.map(chat => [chat.id, chat]));
      const locations = new Map<string, SavedChatLocation>();
      // Only identities already open in Now are queried; a truncated Home never invents membership.
      for (let offset = 0; offset < files.length && live; offset += 8) {
        await Promise.all(files.slice(offset, offset + 8).map(async file => {
          const chatId = chatIdFromSessionFile(file);
          if (!chatId) return;
          const membership = await shell!.client.chatPlaces(chatId);
          if (!live) return;
          if (membership.known && (!membership.sessionFile || membership.sessionFile === file)) locations.set(file, { chatId, workspace: membership.workspace ?? rows.get(chatId)?.workspace ?? '', placeIds: membership.places.filter(p => !p.archived).map(p => p.id) });
        }));
      }
      if (!live) return record;
      setSource(record);
      setIds(matchingTabs(record.workspace, { placeId: placeId!, sources: digest.place?.sources ?? [], resolve: file => locations.get(file) }));
      return record;
    }
    void (async () => {
      let record = await read();
      while (live && record) {
        const next = await client.wait('now', record.revision, abort.signal);
        if (!live) return;
        record = next.revision === record.revision ? next : await read();
      }
    })().catch(failure => { if (live) shell.warn(failure); });
    return () => { live = false; abort.abort(); };
  }, [eligible, active, placeId, digest.revision, digest.readAt]);
  if (!eligible || !source?.workspace || (!ids.length && !pending.current)) return null;

  async function publish(undo?: TransferReceipt) {
    if (!placeId || !isWorkspaceKey(placeId) || !host || !shell || held.current) return;
    held.current = true; setBusy(true);
    const here = placeId;
    try {
      let receipt: TransferReceipt | undefined;
      let acceptedSource: WorkspaceRecord | undefined;
      let acceptedDestination: WorkspaceRecord | undefined;
      for (let attempt = 0; attempt < 5; attempt++) {
        let ask = undo ? undoPending.current : pending.current;
        if (!ask) {
          const [now, place] = await Promise.all([client.get('now'), client.get(here)]);
          if (!now.workspace || !place.workspace) throw new Error('Wait for both tab sets to finish saving.');
          const plan = undo ? planTransferUndo(now.workspace, place.workspace, undo) : planTransfer(now.workspace, place.workspace, ids, emptySource);
          ask = { receipt: undo ?? plan.receipt, request: {
            intent: globalThis.crypto.randomUUID(), writer: windowWriter(), destination: undo ? 'now' : here,
            sourceRevision: undo ? place.revision : now.revision, destinationRevision: undo ? now.revision : place.revision,
            sourceWorkspace: plan.source, destinationWorkspace: plan.destination,
          } };
          if (undo) undoPending.current = ask;
          if (!undo) { pending.current = ask; safeLocal()?.setItem(storageKey(here), JSON.stringify(ask)); }
        }
        const answer = await client.transfer(undo ? here : 'now', ask.request);
        if (answer.kind === 'conflict') {
          if (undo) undoPending.current = undefined;
          if (!undo) { pending.current = undefined; safeLocal()?.removeItem(storageKey(here)); }
          continue;
        }
        if (undo) undoPending.current = undefined;
        receipt = ask.receipt; acceptedSource = answer.source; acceptedDestination = answer.destination;
        if (!undo) { pending.current = undefined; safeLocal()?.removeItem(storageKey(here)); }
        break;
      }
      if (!receipt || !acceptedSource || !acceptedDestination) throw new Error('These tabs are changing in another window. Try the move again.');
      if (scope.current === here) {
        const nowLocal = readPersisted(safeLocal(), 'now', windowWriter())?.local ?? emptyLocal();
        const local = transferredLocal(emptyLocal(), nowLocal, receipt);
        host.receiveTransfer?.(undo ? acceptedSource : acceptedDestination, undo ? undefined : local);
        setIds([]); setSource(undo ? acceptedDestination : acceptedSource);
      }
      if (!undo) {
        const kept = receipt;
        toast.show({ text: `Moved ${kept.tabIds.length} matching ${kept.tabIds.length === 1 ? 'tab' : 'tabs'} into ${digest.title}`, undo: () => publish(kept) });
      } else toast.show({ text: 'Matching tabs returned to Now.' });
    } catch (failure) { shell.warn(failure); }
    finally { held.current = false; if (scope.current === here) setBusy(false); }
  }
  const count = ids.length || pending.current?.receipt.tabIds.length || 0;
  return <div className="home-suggestion" aria-label="Matching tabs suggestion">
    <Icon name="sparkles" size="micro"/><span>{count} matching {count === 1 ? 'tab is' : 'tabs are'} open in Now</span><span aria-hidden="true">·</span>
    <Button variant="ghost" className="home-suggestion-action" disabled={busy} onClick={() => void publish()}>Move matching tabs</Button>
  </div>;
}
