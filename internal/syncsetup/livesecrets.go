package syncsetup

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/filesdoor"
	"github.com/Agent-Field/codeaf/internal/syncwords"
	"github.com/Agent-Field/codeaf/internal/vaultsync"
)

// THE SECRET FILES OF A CHAT MOVE BY BLIND PASS-THROUGH TOO, AND ONLY THEY.
//
// The files the seal guard keeps out of a chat's saved history (every dotenv
// file, and any file that looks like it holds a secret) travelled only through
// the key vault, which waits for sync storage, so a chat that moved without
// storage arrived without them and its git folder showed them deleted. They now
// ride the files door beside the chat's objects (internal/filesdoor, secrets.go):
// the computer that has the chat reads them from the chat's own folder when it is
// asked, the door seals the page under the person's key, and the computer taking
// the chat writes them into its own folder. Nothing is stored on the way: not on
// the relay, not in either computer's live store.
//
// IT IS NOT THE VAULT, ON PURPOSE. The vault is one sealed object that also
// holds saved provider keys, connections, preferences, shared memories and teams,
// and those must keep waiting for storage approval. This road reads and writes
// the chat's withheld files directly (vaultsync Files.Offer and Files.Receive),
// so it cannot carry anything else.

var (
	// liveChatFinder answers where chat id lives on this computer: its cell and
	// the folder its files are in. A test replaces it, because a rig keeps its
	// chats outside the layout a real home has.
	liveChatFinder = findChatHere
	// liveSecretsPage is how many bytes of file one page of secret files holds;
	// zero is the door's own. A test shortens it to put a removal between pages.
	liveSecretsPage int
	// liveSecretsHook, when set, is told of every request this computer's door
	// answers for a chat's secret files. A test counts them with it.
	liveSecretsHook func(home, chat string)
)

// findChatHere looks in the places a home keeps a chat, the same places the
// take looks when it asks where a chat lives (cmd/codeaf openCellByIDAt).
func findChatHere(home, id string) (cell.Cell, string, bool) {
	// The id becomes part of a glob below, so only an id of the shape every
	// chat's has is looked for.
	if !cell.ValidID(id) {
		return cell.Cell{}, "", false
	}
	buckets, _ := filepath.Glob(filepath.Join(home, "v3", "projects", "*", id))
	moving, _ := filepath.Glob(filepath.Join(home, "v3", "projects", "*", ".moving", id))
	for _, root := range append(append(buckets, moving...), filepath.Join(cell.Dir(home), id)) {
		if c, err := cell.OpenAt(root, id); err == nil {
			return c, workspaceOf(c.Root), true
		}
	}
	return cell.Cell{}, "", false
}

// serveSecrets is this computer's answer to a request for a chat's secret files.
// It answers only while blind pass-through is carrying chats here: with the
// switch off, or with storage approved (the vault carries them then), the door
// says it has nothing. A chat this computer cannot find is answered the same way.
func (s *Sync) serveSecrets(ctx context.Context, chat, after string, budget int) ([]filesdoor.SecretFile, bool, bool, error) {
	if !s.LiveTransfer() {
		return nil, false, false, nil
	}
	c, workspace, ok := liveChatFinder(s.Home, chat)
	if !ok {
		return nil, false, false, nil
	}
	if liveSecretsHook != nil {
		liveSecretsHook(s.Home, chat)
	}
	files, err := s.files(cellstore.EngineFor(workspace), c)
	if err != nil {
		return nil, false, false, err
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, false, false, err
		}
		page, more, err := files.Offer(after, min(budget, vaultsync.PassThroughPage))
		if err != nil {
			return nil, false, false, err
		}
		var out []filesdoor.SecretFile
		for _, f := range page {
			if len(f.Path) > filesdoor.MaxSecretPath {
				continue
			}
			if len(out) == filesdoor.MaxSecretsPerPage {
				more = true
				break
			}
			out = append(out, filesdoor.SecretFile{Path: f.Path, Mode: uint32(f.Mode), ModTime: f.ModTime, Data: f.Data})
		}
		if len(out) > 0 || !more || len(page) == 0 {
			return out, more, true, nil
		}
		// Every file of the page had a name too long to send; look past them.
		after = page[len(page)-1].Path
	}
}

// takeSecretFiles brings the secret files of the chat from the computer that has
// it into the folder the chat's files landed in. It runs after the files are
// down, as the vault's restore does, and it is silent when no computer has any.
// A file this computer already has, saved later than the sender's, is kept: the
// newest whole file wins, the vault's own rule.
//
// Nothing is registered with this computer's seal guard here, on purpose: the
// guard's list of what it keeps out of the history is part of the chat's own
// state and arrived with it, so the files that came are already on it, and this
// computer can pass them on at once. The pass-on test in live_secrets_test.go
// holds that.
func (c *Continuer) takeSecretFiles(ctx context.Context, sc *Scope, files vaultsync.Files) error {
	if sc == nil || sc.live == nil || !c.sync.LiveTransfer() {
		return nil
	}
	err := sc.live.secrets(ctx, files.Chat, func(page filesdoor.SecretPage) error {
		in := make([]vaultsync.File, 0, len(page.Files))
		for _, f := range page.Files {
			in = append(in, vaultsync.File{Path: f.Path, Mode: fs.FileMode(f.Mode), ModTime: f.ModTime, Data: f.Data})
		}
		_, err := files.Receive(in)
		return err
	})
	var off *syncwords.HolderOfflineError
	if errors.As(err, &off) {
		return errors.New(syncwords.SecretFilesLate(off.Name))
	}
	return err
}
