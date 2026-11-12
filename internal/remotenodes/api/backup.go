// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/backup"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/store"
)

// BackupOps is what the API asks of the code that seals the state into a backup
// and takes one back. *manager.Manager is one. Its errors are the backup
// package's and the store's own, which backupError gives their codes.
type BackupOps interface {
	// ExportBackup returns the file and the name to give it.
	ExportBackup(ctx context.Context, passphrase string) (name string, data []byte, err error)
	// ImportBackup opens a file on an install that has no nodes and no
	// subscriptions, and says how many nodes came back.
	ImportBackup(ctx context.Context, passphrase string, data []byte) (nodes int, err error)
}

const (
	// maxBackupBody is the most an import may send: the file as base64 inside a
	// JSON body. A real backup is a few KB, and the page itself stops at 2 MiB.
	maxBackupBody = 4 << 20
	// maxBackupBytes is the most the file may be once it is decoded.
	maxBackupBytes = 4 << 20
)

// backupRoutes adds the two calls that make and take a backup, when there is
// something to carry them out.
func (a *API) backupRoutes(route func(pattern string, h http.HandlerFunc)) {
	if a.cfg.Backup == nil {
		return
	}
	route("POST /api/backup/export", a.exportBackup)
	route("POST /api/backup/import", a.importBackup)
}

// backupError gives what the backup calls can refuse the codes and sentences the
// page has words for. What is not one of them is passed on as it is.
func backupError(err error) error {
	switch {
	case errors.Is(err, backup.ErrWeakPassphrase):
		return &Error{CodeBadRequest, fmt.Sprintf("The passphrase must have at least %d characters.", backup.MinPassphraseRunes)}
	case errors.Is(err, backup.ErrBadPassphrase):
		return &Error{CodeBackupBadPassphrase, "That passphrase does not open the backup, or the file was changed."}
	case errors.Is(err, backup.ErrBadFile):
		msg := "That is not a backup this version can open"
		if why := strings.TrimPrefix(strings.TrimPrefix(err.Error(), backup.ErrBadFile.Error()), ": "); why != "" {
			msg += ": " + why
		}
		return &Error{CodeBackupBadFile, msg + "."}
	case errors.Is(err, store.ErrNotEmpty):
		return &Error{CodeBadRequest, "This install already has nodes or subscriptions, and a backup can only be imported into an empty one."}
	case errors.Is(err, model.ErrNewer):
		return &Error{CodeBackupBadFile, "That backup was made by a newer version of this plugin."}
	case errors.Is(err, store.ErrBadSnapshot):
		return &Error{CodeBackupBadFile, "That backup holds a state this version can't use."}
	}
	return err
}

func (a *API) exportBackup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Passphrase string `json:"passphrase"`
	}
	if err := strictJSON(w, r, maxSmallBody, &req); err != nil {
		a.fail(w, err)
		return
	}
	if req.Passphrase == "" {
		a.fail(w, &Error{CodeBadRequest, "The passphrase is required."})
		return
	}
	name, data, err := a.cfg.Backup.ExportBackup(r.Context(), req.Passphrase)
	if err != nil {
		a.failOp(w, r, backupError(err))
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Content-Disposition", disposition(name))
	h.Set("Content-Length", strconv.Itoa(len(data)))
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// disposition is the header that makes a download of the file, named as the
// manager says. A name with anything but plain ASCII in it goes in the
// encoded form, which the page reads back.
func disposition(name string) string {
	return mime.FormatMediaType("attachment", map[string]string{"filename": name})
}

func (a *API) importBackup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Passphrase string `json:"passphrase"`
		Data       string `json:"data"`
	}
	if err := strictJSON(w, r, maxBackupBody, &req); err != nil {
		if errors.Is(err, errBodyTooLarge) {
			err = &Error{CodeBackupBadFile, "That file is too large to be a backup."}
		}
		a.fail(w, err)
		return
	}
	if req.Passphrase == "" {
		a.fail(w, &Error{CodeBadRequest, "The passphrase is required."})
		return
	}
	data, err := backupFile(req.Data)
	if err != nil {
		a.fail(w, err)
		return
	}
	nodes, err := a.cfg.Backup.ImportBackup(r.Context(), req.Passphrase, data)
	if err != nil {
		a.failOp(w, r, backupError(err))
		return
	}
	writeJSON(w, http.StatusOK, struct {
		OK    bool `json:"ok"`
		Nodes int  `json:"nodes"`
	}{true, nodes})
}

// backupFile is the file in the base64 the page sends it in. The size is judged
// before anything is decoded.
func backupFile(s string) ([]byte, error) {
	if base64.StdEncoding.DecodedLen(len(s)) > maxBackupBytes {
		return nil, &Error{CodeBackupBadFile, "That file is too large to be a backup."}
	}
	data, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(data) == 0 {
		return nil, &Error{CodeBackupBadFile, "The file did not come through as base64."}
	}
	return data, nil
}
