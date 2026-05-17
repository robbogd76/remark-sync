package remarkable

const (
	// DocumentType is a regular file (notebook or uploaded PDF/EPUB).
	DocumentType = "DocumentType"
	// CollectionType is a folder.
	CollectionType = "CollectionType"
)

// Document is a Remarkable cloud document or folder, populated from the
// Sync 1.5 API metadata blob.
type Document struct {
	ID             string // document UUID
	Hash           string // hash of this document's file index on the sync API
	VissibleName   string // display name ("VissibleName" matches the historic field name)
	Type           string // DocumentType | CollectionType
	Parent         string // parent folder UUID; empty string = root
	ModifiedClient string // last-modified formatted as "2006-01-02T15:04:05"
	// FolderPath is the slash-separated chain of ancestor folder names from
	// the root down to (but not including) this document, e.g. "Work/2024".
	// Empty for root-level documents.
	FolderPath string
}

// IsFolder reports whether the document is a collection (folder).
func (d Document) IsFolder() bool {
	return d.Type == CollectionType
}
