package main

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

var (
	errDoctorPublicationPresent     = errors.New("this chat already reached your other computers")
	errDoctorPublicationUnavailable = errors.New("this chat is no longer available to send")
	errDoctorPublicationArchive     = errors.New("could not put away the chat on your other computers")
)

// doctorUnlistedChats compares only chats with this paired writer's durable
// intent against a successful listing. Absence of a receipt proves nothing.
func doctorUnlistedChats(s *syncsetup.Sync, inv localChatInventory, listing directory.Listing, fix bool) (issues, fixes []string) {
	for _, id := range slices.Sorted(maps.Keys(inv.foldersByID)) {
		if _, listed := listing.Cells[id]; listed || inv.inUseByID[id] || len(inv.foldersByID[id]) != 1 {
			continue
		}
		dir := inv.foldersByID[id][0]
		c, err := cell.OpenAt(dir, id)
		if err != nil || !s.PublicationExpected(c) {
			continue
		}
		meta, err := session.LoadMeta(dir)
		if err != nil || !meta.Owned || meta.Archived {
			continue
		}
		head, err := cellstore.Head(c)
		if err != nil || head == nil {
			continue
		}
		if spoken, _ := session.ConversationIn(session.Place{Dir: dir}.Transcript(), head.Receipt.Transcript.End); !spoken {
			continue
		}
		if fix {
			err := repairDoctorPublication(s, c, meta)
			switch {
			case err == nil:
				fixes = append(fixes, fmt.Sprintf("fixed: sent chat %s to your other computers", id))
				continue
			case errors.Is(err, errDoctorPublicationPresent), errors.Is(err, errDoctorPublicationUnavailable):
				continue
			case errors.Is(err, errDoctorPublicationArchive):
				issues = append(issues, fmt.Sprintf("could not put away chat on your other computers · chat %s", id))
				continue
			}
		}
		issues = append(issues, fmt.Sprintf("chat has not reached your other computers · chat %s", id))
	}
	return issues, fixes
}

func repairDoctorPublication(s *syncsetup.Sync, c cell.Cell, meta session.Meta) error {
	// Hold the same transcript lock as an open conversation so repair cannot
	// race a window starting a new turn on this computer.
	f, err := os.OpenFile(session.Place{Dir: c.Root}.Transcript(), os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := filelock.Lock(f, true, true); err != nil {
		return err
	}
	defer filelock.Unlock(f)
	current, err := session.LoadMeta(c.Root)
	if err != nil {
		return err
	}
	if current.Archived || !current.Owned {
		return errDoctorPublicationUnavailable
	}
	meta = current
	ctx, cancel := context.WithTimeout(context.Background(), driveCloseWithin)
	defer cancel()
	// A record created since the listing needs no missing-chat report.
	// Repair never acquires an existing record or replaces unfamiliar history.
	if _, err := s.Dir.Cell(ctx, c.ID); !errors.Is(err, directory.ErrNotFound) {
		if err == nil {
			return errDoctorPublicationPresent
		}
		return err
	}
	e := cellstore.EngineFor(meta.Workspace)
	e.Workspace = filepath.Join(c.Root, "work")
	d, err := s.Drive(ctx, e, c, syncsetup.DriveOptions{CreateOnly: true, DeviceName: deviceName(), Title: func() string { return meta.Title }})
	if errors.Is(err, directory.ErrExists) {
		return errDoctorPublicationPresent
	}
	if err != nil {
		return err
	}
	closeErr := errors.Join(d.Gate(), d.Close(ctx))
	alreadyPresent := errors.Is(closeErr, directory.ErrExists)
	// Remote creation is too long for the metadata lock. Recheck after the
	// writer stops, then put away only the record this repair just created.
	current, err = session.LoadMeta(c.Root)
	if err != nil {
		return errors.Join(closeErr, err)
	}
	if current.Archived {
		archiveCtx, archiveCancel := context.WithTimeout(context.Background(), driveCloseWithin)
		defer archiveCancel()
		if closeErr != nil {
			// A lost create reply may return as ErrExists on retry. Only a record
			// still held by this writer at this exact head can be that completion;
			// an unrelated record retains its own history and archive state.
			view, err := s.Dir.Cell(archiveCtx, c.ID)
			if errors.Is(err, directory.ErrNotFound) {
				return errDoctorPublicationUnavailable
			}
			if err != nil {
				return errors.Join(errDoctorPublicationArchive, err)
			}
			head, err := cellstore.Head(c)
			if err != nil || head == nil {
				return errors.Join(errDoctorPublicationArchive, err)
			}
			if view.Cell.Lease.Device != s.Device.ID() || view.Cell.Head != head.Turn.ID {
				return errDoctorPublicationUnavailable
			}
		}
		if err := s.Dir.Archive(archiveCtx, c.ID); err != nil && !errors.Is(err, directory.ErrNotFound) {
			return errors.Join(errDoctorPublicationArchive, err)
		}
		return errDoctorPublicationUnavailable
	}
	if alreadyPresent {
		return errDoctorPublicationPresent
	}
	return closeErr
}
