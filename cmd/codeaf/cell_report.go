package main

import (
	"fmt"
	"io"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstats"
	"github.com/Agent-Field/codeaf/internal/home"
)

// cellReport prints what syncing a cell cost, one row per flush, a total, and the vault's own row.
// The numbers are counts kept on this device (internal/cellstats). Nothing is
// written anywhere unless the person names a file with --export.
func cellReport(c cell.Cell, args []string, out io.Writer) error {
	lines, err := cellstats.Read(home.Dir(), c.ID)
	if err != nil {
		return err
	}
	vault, err := cellstats.Read(home.Dir(), cellstats.VaultScope)
	if err != nil {
		return err
	}
	cellstats.RenderScopes(out, lines, vault)
	if len(args) == 0 || len(lines) == 0 {
		return nil
	}
	if err := cellstats.Export(args[0], lines); err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "wrote %s (%s)\n", args[0], cellstats.ExportNotice)
	return err
}
