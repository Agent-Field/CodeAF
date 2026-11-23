package filesdoor

import (
	"context"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/chacha20poly1305"

	"github.com/Agent-Field/codeaf/internal/identity"
)

// THE SECRET FILES OF A CHAT TRAVEL BESIDE ITS OBJECTS, NOT AMONG THEM.
//
// The files a chat's seal guard keeps out of its saved history (every dotenv
// file, and any file that looks like it holds a secret) are not objects of any
// store: they are read from the folder of the computer that has them, at the
// moment they are asked for, and written into the folder of the computer that
// asks. So nothing is kept anywhere in between, the live store included, and
// the relay's own storage never hears of them.
//
// THEY LEAVE THE FOLDER SEALED. The page is sealed under a key derived from the
// person's cell key before it is handed to the tunnel, so the tunnel is a second
// layer and not the only one, and a page cannot be passed off as the answer to
// another request: the chat and the cursor are part of what the seal covers.
//
// ONE REQUEST, ANSWERED A BOUNDED PAGE AT A TIME. The asker names the chat and
// the last path it has; the door answers the next page, at most the data its own
// [ServeConfig.SecretsPage] allows, and says whether more remain. Like every
// other request it is asked only after the last answer was read, so the relay is
// never made to hold more than one page.

const (
	// DefaultSecretsPage is the most bytes of file data one page holds. With a
	// page's own headers and sealing it stays well inside [MaxResponseBytes].
	DefaultSecretsPage = 4 << 20

	// MaxSecretFile is the largest file a page may hold. The sender already
	// leaves out what the key vault cannot carry, so this is only the bound the
	// receiver holds a stranger's page to.
	MaxSecretFile = 1 << 20

	// MaxSecretPath and MaxSecretsPerPage bound the headers of a page, which the
	// data budget does not: a page of a thousand empty files with long names is
	// still small.
	MaxSecretPath     = 1024
	MaxSecretsPerPage = 1024

	// maxSecretChat bounds the chat id a request carries. A chat's id is 26
	// characters; the rest is room for what a later version may use.
	maxSecretChat = 255

	secretsSalt = "codeaf files door secrets v1"

	// A secrets answer leads with one of these.
	secretsPage   byte = 0 // a sealed page follows
	secretsAbsent byte = 1 // this computer has no such chat
)

// SecretFile is one of a chat's withheld files as the door sends it.
type SecretFile struct {
	Path    string // slash separated, relative to the chat's folder
	Mode    uint32 // the permission bits
	ModTime time.Time
	Data    []byte
}

// SecretsFunc answers a request for the secret files of one chat: the files
// that sort after the path after, holding at most budget bytes of data (a page
// holds at least one file when any remain), and whether more remain. found is
// false when this computer does not have the chat or is not to give its files.
type SecretsFunc func(ctx context.Context, chat, after string, budget int) (files []SecretFile, more, found bool, err error)

// SecretPage is one answer to a request for a chat's secret files.
type SecretPage struct {
	// Found is false when the computer has no such chat to give files for.
	Found bool
	// More is true when asking again after the last path would give more files.
	More  bool
	Files []SecretFile
}

var errSecretsMalformed = errors.New("filesdoor: a secret files page is not the shape it declares")

// secretsKey is the key a page is sealed under: derived from the person's cell
// key with its own salt, so it is not the key anything else is sealed with.
func secretsKey(id identity.Identity) ([]byte, error) {
	return hkdf.Key(sha256.New, id.CellKey(), []byte(secretsSalt), "", chacha20poly1305.KeySize)
}

// secretsAAD binds a sealed page to the request it answers.
func secretsAAD(chat, after string) []byte {
	aad := append([]byte(secretsSalt), 0)
	aad = append(aad, chat...)
	aad = append(aad, 0)
	return append(aad, after...)
}

// sealSecrets writes a page out and seals it. Nothing about its files is in
// the clear in what it returns.
func sealSecrets(id identity.Identity, chat, after string, p SecretPage) ([]byte, error) {
	if len(p.Files) > MaxSecretsPerPage {
		return nil, fmt.Errorf("filesdoor: a page of %d secret files is over the limit of %d", len(p.Files), MaxSecretsPerPage)
	}
	flags := byte(0)
	if p.More {
		flags = 1
	}
	plain := binary.BigEndian.AppendUint32([]byte{flags}, uint32(len(p.Files)))
	for _, f := range p.Files {
		if f.Path == "" || len(f.Path) > MaxSecretPath || len(f.Data) > MaxSecretFile {
			return nil, fmt.Errorf("filesdoor: a secret file is outside the bounds of a page (%d byte path, %d bytes)", len(f.Path), len(f.Data))
		}
		plain = binary.BigEndian.AppendUint16(plain, uint16(len(f.Path)))
		plain = append(plain, f.Path...)
		plain = binary.BigEndian.AppendUint32(plain, f.Mode)
		plain = binary.BigEndian.AppendUint64(plain, uint64(f.ModTime.UnixNano()))
		plain = binary.BigEndian.AppendUint32(plain, uint32(len(f.Data)))
		plain = append(plain, f.Data...)
	}
	key, err := secretsKey(id)
	if err != nil {
		return nil, err
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, plain, secretsAAD(chat, after)), nil
}

// openSecrets opens a page sealed for the request (chat, after) and reads it
// back, refusing anything that is not exactly what sealSecrets writes.
func openSecrets(id identity.Identity, chat, after string, sealed []byte) (SecretPage, error) {
	key, err := secretsKey(id)
	if err != nil {
		return SecretPage{}, err
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return SecretPage{}, err
	}
	if len(sealed) < aead.NonceSize()+aead.Overhead() {
		return SecretPage{}, errSecretsMalformed
	}
	plain, err := aead.Open(nil, sealed[:aead.NonceSize()], sealed[aead.NonceSize():], secretsAAD(chat, after))
	if err != nil {
		return SecretPage{}, errors.New("filesdoor: a secret files page did not open for the request it answers")
	}
	if len(plain) < 5 || plain[0] > 1 {
		return SecretPage{}, errSecretsMalformed
	}
	page := SecretPage{Found: true, More: plain[0] == 1}
	count := int(binary.BigEndian.Uint32(plain[1:5]))
	if count > MaxSecretsPerPage {
		return SecretPage{}, errSecretsMalformed
	}
	rest := plain[5:]
	for range count {
		if len(rest) < 2 {
			return SecretPage{}, errSecretsMalformed
		}
		n := int(binary.BigEndian.Uint16(rest))
		if n == 0 || n > MaxSecretPath || len(rest) < 2+n+4+8+4 {
			return SecretPage{}, errSecretsMalformed
		}
		f := SecretFile{Path: string(rest[2 : 2+n])}
		rest = rest[2+n:]
		f.Mode = binary.BigEndian.Uint32(rest)
		f.ModTime = time.Unix(0, int64(binary.BigEndian.Uint64(rest[4:]))).UTC()
		size := int(binary.BigEndian.Uint32(rest[12:]))
		rest = rest[16:]
		if size > MaxSecretFile || size > len(rest) {
			return SecretPage{}, errSecretsMalformed
		}
		f.Data = append([]byte(nil), rest[:size]...)
		rest = rest[size:]
		page.Files = append(page.Files, f)
	}
	if len(rest) != 0 {
		return SecretPage{}, errSecretsMalformed
	}
	return page, nil
}

// secretsRequest is the body of a request: the chat's id and the path to start
// after, each with its length.
func secretsRequest(chat, after string) ([]byte, error) {
	if chat == "" || len(chat) > maxSecretChat || len(after) > MaxSecretPath {
		return nil, fmt.Errorf("filesdoor: a secret files request for a chat of %d characters after a path of %d is out of bounds", len(chat), len(after))
	}
	body := append([]byte{byte(len(chat))}, chat...)
	body = binary.BigEndian.AppendUint16(body, uint16(len(after)))
	return append(body, after...), nil
}

func parseSecretsRequest(body []byte) (chat, after string, err error) {
	if len(body) < 1 || body[0] == 0 || len(body) < 1+int(body[0])+2 {
		return "", "", errMalformed
	}
	n := int(body[0])
	chat = string(body[1 : 1+n])
	rest := body[1+n:]
	m := int(binary.BigEndian.Uint16(rest))
	if m > MaxSecretPath || len(rest) != 2+m {
		return "", "", errMalformed
	}
	return chat, string(rest[2:]), nil
}

// secrets answers a request for a chat's secret files. A door with no one to
// ask, or asked about a chat it has not got, says so and goes on; it never
// invents a page.
func (d *door) secrets(ctx context.Context, body []byte) (reply, bool) {
	chat, after, err := parseSecretsRequest(body)
	if err != nil {
		return errorReply(codeBadRequest), true
	}
	absent := reply{typ: typeSecrets | answerBit, parts: [][]byte{{secretsAbsent}}}
	if d.cfg.Secrets == nil {
		return absent, false
	}
	budget := d.cfg.SecretsPage
	if budget <= 0 {
		budget = DefaultSecretsPage
	}
	files, more, found, err := d.cfg.Secrets(ctx, chat, after, budget)
	switch {
	case err != nil:
		return errorReply(codeInternal), false
	case !found:
		return absent, false
	}
	// A provider that hands over more than a page is a fault here, and what it
	// handed over is not sent.
	size := 0
	for _, f := range files {
		size += len(f.Data)
	}
	if len(files) > MaxSecretsPerPage || (len(files) > 1 && size > budget) {
		return errorReply(codeInternal), false
	}
	sealed, err := sealSecrets(d.cfg.Identity, chat, after, SecretPage{Found: true, More: more, Files: files})
	if err != nil {
		return errorReply(codeInternal), false
	}
	return reply{typ: typeSecrets | answerBit, parts: [][]byte{{secretsPage}, sealed}}, false
}

// Secrets asks the computer for the next page of a chat's secret files, after
// the path after ("" for the first page). The files are sealed under the
// person's key by the door and opened here; a page that does not open for this
// request breaks the tunnel, as any answer that breaks the wire rules does.
func (s *Source) Secrets(ctx context.Context, chat, after string) (SecretPage, error) {
	body, err := secretsRequest(chat, after)
	if err != nil {
		return SecretPage{}, err
	}
	var page SecretPage
	err = s.exchange(ctx, typeSecrets, body, func(b []byte) error {
		if len(b) < 1 {
			return protocolf("a secret files answer without a status")
		}
		switch b[0] {
		case secretsAbsent:
			if len(b) != 1 {
				return protocolf("a secret files answer that holds a page and says it has none")
			}
			return nil
		case secretsPage:
			got, err := openSecrets(s.cfg.Identity, chat, after, b[1:])
			if err != nil {
				return protocolf("%v", err)
			}
			page = got
			return nil
		}
		return protocolf("a secret files status of %d", b[0])
	})
	if err != nil {
		return SecretPage{}, err
	}
	return page, nil
}
