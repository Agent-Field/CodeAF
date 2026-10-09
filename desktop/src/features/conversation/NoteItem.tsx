export function NoteItem({ text }: { text: string }) {
  if (!text) return null;
  return (
    <div className="note-item" role="note">
      <span className="note-item-text">{text}</span>
    </div>
  );
}
