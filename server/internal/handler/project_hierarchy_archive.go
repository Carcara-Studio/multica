package handler

import (
	"errors"
	"net/http"

	"github.com/multica-ai/multica/server/internal/projecthierarchy"
)

func writeHierarchyArchiveConflict(w http.ResponseWriter, err error) bool {
	var conflict *projecthierarchy.ArchiveConflict
	if !errors.As(err, &conflict) {
		return false
	}
	writeJSON(w, http.StatusConflict, map[string]any{
		"error": conflict.Error(), "code": "hierarchy_archive_blocked", "blockers": conflict.Blockers,
	})
	return true
}
