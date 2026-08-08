package exec

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/cas"
)

// An attachment used to be a reference to somebody's own filesystem. The job
// read the bytes when it ran, which meant a PDF moved into a different folder
// on Tuesday broke a retry of Monday's work, and a follow-up the next morning
// carried nothing at all — the durable record named a file, and the file was
// the person's, not ours.
//
// So an attachment is copied when it is mentioned. The bytes go into the
// content-addressed store the moment the message is sent, the message carries
// a reference to them, and every later reader — a retry, a follow-up in a new
// session, a continuation a week later — reads our copy. The person's file is
// opened once, read-only, and never touched again.
const (
	// MaxAttachmentBytes is the ceiling on one attached file.
	MaxAttachmentBytes = 25 << 20
	attachmentDir      = "attachments"
)

// KeepAttachment copies one file the person explicitly attached into the store
// at root and returns the durable reference to it. Only the named file is
// read: nothing here walks a directory, follows a listing, or reaches for a
// sibling.
func KeepAttachment(root, path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", fmt.Errorf("keep attachment: path is required")
	}
	absolute, err := filepath.Abs(trimmed)
	if err != nil {
		return "", fmt.Errorf("keep attachment %s: %w", filepath.Base(trimmed), err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", fmt.Errorf("keep attachment %s: file is unavailable", filepath.Base(absolute))
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("keep attachment %s: not a file", filepath.Base(absolute))
	}
	if info.Size() > MaxAttachmentBytes {
		return "", fmt.Errorf("keep attachment %s: over the 25 MB limit", filepath.Base(absolute))
	}
	blobs, err := cas.New(root)
	if err != nil {
		return "", fmt.Errorf("keep attachment %s: %w", filepath.Base(absolute), err)
	}
	// Identical bytes attached twice cost one copy: the digest is the name, so
	// the second Put finds the object already there.
	ref, err := blobs.PutFile(absolute)
	if err != nil {
		return "", fmt.Errorf("keep attachment %s: %w", filepath.Base(absolute), err)
	}
	return cas.Reference(ref, absolute), nil
}

// StagedAttachments is what one job's workspace holds after staging.
type StagedAttachments struct {
	// Documents and Images are workspace-relative, which is the spelling the
	// leaf's own tools take: read_document and view_image both address the
	// workspace.
	Documents []string
	Images    []string
	// ImageFiles are the same images as absolute paths, for the turn content a
	// model with eyes receives directly.
	ImageFiles []string
}

// StageAttachments materializes a node's attachments as immutable workspace
// inputs. References resolve out of the store; a path recorded before copies
// existed still resolves from disk, so old work keeps running.
func StageAttachments(space *Workspace, root string, attachments []string) (StagedAttachments, error) {
	var staged StagedAttachments
	if space == nil {
		return staged, fmt.Errorf("stage attachments: nil workspace")
	}
	seen := make(map[string]bool)
	for _, attachment := range attachments {
		extension := strings.ToLower(filepath.Ext(cas.SourcePath(attachment)))
		document := extension == ".pdf" || extension == ".docx" || extension == ".pptx"
		image := isStageableImage(extension)
		if !document && !image {
			continue
		}
		data, name, err := readAttachment(root, attachment)
		if err != nil {
			return StagedAttachments{}, err
		}
		relative := filepath.Join(attachmentDir, stagedName(name, data))
		if seen[relative] {
			continue
		}
		seen[relative] = true
		target, err := space.Resolve(relative)
		if err != nil {
			return StagedAttachments{}, fmt.Errorf("stage attachment %s: %w", name, err)
		}
		if err := writeStagedFile(target, data); err != nil {
			return StagedAttachments{}, fmt.Errorf("stage attachment %s: %w", name, err)
		}
		slashed := filepath.ToSlash(relative)
		if document {
			staged.Documents = append(staged.Documents, slashed)
			continue
		}
		staged.Images = append(staged.Images, slashed)
		staged.ImageFiles = append(staged.ImageFiles, target)
	}
	return staged, nil
}

// attachedImageNote is what a leaf that cannot see is told about the images it
// was given. Either a vision model can be asked to describe them, or nothing
// here can look at all — and in that case the leaf must say so in its answer,
// because a person who attached a screenshot and gets a confident reply that
// never mentions it has been quietly lied to.
func attachedImageNote(names []string, proxy bool) string {
	if len(names) == 0 {
		return ""
	}
	list := strings.Join(names, ", ")
	if proxy {
		return "Attached image(s), already in your workspace: " + list +
			". The model working this job cannot see images; view_image will have a vision model" +
			" look at one and describe it, so use it before reasoning about what they contain."
	}
	return "Attached image(s), already in your workspace: " + list +
		". Nothing available to this job can see an image — the working model has no vision and no" +
		" vision model is configured. Do what you can without them and say plainly in your answer" +
		" that the attached image could not be read."
}

func (l *Linear) visionProxy() bool {
	return l.media != nil && strings.TrimSpace(l.media.VisionModel) != "" && l.media.VisionClient != nil
}

// workspaceNames renders staged paths the way the leaf's own tools take them.
func (l *Linear) workspaceNames(paths []string) []string {
	names := make([]string, 0, len(paths))
	for _, path := range paths {
		if l.workspace != nil {
			if relative, err := filepath.Rel(l.workspace.Root(), path); err == nil &&
				!strings.HasPrefix(relative, "..") {
				names = append(names, filepath.ToSlash(relative))
				continue
			}
		}
		names = append(names, filepath.ToSlash(path))
	}
	return names
}

func isStageableImage(extension string) bool {
	switch extension {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif":
		return true
	default:
		return false
	}
}

// readAttachment prefers our own copy and falls back to the person's file,
// which is what a record journaled before copy-at-mention still points at.
func readAttachment(root, attachment string) ([]byte, string, error) {
	ref, source, isReference := cas.ParseReference(attachment)
	name := filepath.Base(source)
	if name == "." || name == string(filepath.Separator) {
		name = "attachment"
	}
	if isReference && strings.TrimSpace(root) != "" {
		if blobs, err := cas.New(root); err == nil {
			if data, err := readBlob(blobs, ref); err == nil {
				return data, name, nil
			}
		}
	}
	if source == "" || isReference && source == attachment {
		return nil, name, fmt.Errorf("stage attachment %s: the stored copy is missing", name)
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return nil, name, fmt.Errorf("stage attachment %s: file is unavailable", name)
	}
	if len(data) > MaxAttachmentBytes {
		return nil, name, fmt.Errorf("stage attachment %s: over the 25 MB limit", name)
	}
	return data, name, nil
}

func readBlob(blobs *cas.Store, ref cas.Ref) ([]byte, error) {
	reader, err := blobs.Get(ref)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	path, err := blobs.Path(ref)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

// stagedName keeps the person's own filename and appends a short content
// suffix, so two files called report.pdf stay distinct and one file staged by
// several leaves of the same job lands on one already-complete path.
func stagedName(name string, data []byte) string {
	extension := filepath.Ext(name)
	stem := strings.TrimSuffix(name, extension)
	if stem == "" {
		stem = "attachment"
	}
	digest := cas.DigestBytes(data).String()
	return stem + "-" + digest[:8] + extension
}

func writeStagedFile(target string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return nil
		}
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
