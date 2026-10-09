import { UserMessage } from '../UserMessage';

const long = `We load configs from three places: the repo root, the user's home dir, and an env override that points anywhere on disk. All three go through the same parser, but the env path is read first and cached, so a bad file there poisons every later load. Last week a trailing comma in the shared fixture broke CI, and the error pointed at line 1 of the wrong file. Cover all three paths and keep errors pointing at the right place. Also add a regression test for each loader, and make the error text name the file that was being read when the parse failed.`;

/** Sent, long (clamped with Show more) and with an attachment slot. */
export function UserMessageSpecimen() {
  return (
    <div className="user-message-specimen">
      <UserMessage text="Can you make the config parser accept trailing commas? The fixtures in testdata keep tripping on it." />
      <UserMessage text={long} />
      <UserMessage text="These two screens show the error. Log attached." attachments={<span>error.log</span>} />
    </div>
  );
}
