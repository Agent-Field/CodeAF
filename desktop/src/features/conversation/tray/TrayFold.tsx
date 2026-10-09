import type { ReactNode } from 'react';

/** 1f Tray compaction: one of the two tray shapes, shrunk to nothing (or grown back) over dur-base. A hidden shape is inert. */
export function TrayFold({ shown, children }: { shown: boolean; children: ReactNode }) {
  return (
    <div className="tray-fold" data-shown={shown || undefined} inert={!shown}>
      <div className="tray-fold-room">{children}</div>
    </div>
  );
}
